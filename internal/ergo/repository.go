package ergo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxEventLineBytes = 10 * 1024 * 1024

type Repository struct {
	root      string
	ergoDir   string
	eventPath string
}

func Open(start string) (*Repository, error) {
	if start == "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		start = workingDir
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	ergoDir, err := discover(absolute)
	if err != nil {
		return nil, err
	}
	eventPath := filepath.Join(ergoDir, "plans.jsonl")
	if _, err := os.Stat(eventPath); errors.Is(err, os.ErrNotExist) {
		legacyPath := filepath.Join(ergoDir, "events.jsonl")
		if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
			eventPath = legacyPath
		}
	}
	return &Repository{
		root:      filepath.Dir(ergoDir),
		ergoDir:   ergoDir,
		eventPath: eventPath,
	}, nil
}

func discover(start string) (string, error) {
	info, err := os.Stat(start)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		start = filepath.Dir(start)
	}
	if filepath.Base(start) == ".ergo" {
		return start, nil
	}
	current := start
	for {
		candidate := filepath.Join(current, ".ergo")
		candidateInfo, statErr := os.Stat(candidate)
		if statErr == nil {
			if !candidateInfo.IsDir() {
				return "", fmt.Errorf("%s exists but is not a directory", candidate)
			}
			return candidate, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("no .ergo directory found from %s", start)
}

func (r *Repository) Root() string {
	return r.root
}

func (r *Repository) EventPath() string {
	return r.eventPath
}

func (r *Repository) Load() (Snapshot, error) {
	events, err := readEvents(r.eventPath)
	if err != nil {
		return Snapshot{}, err
	}
	tasks, deps, err := replay(events)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", r.eventPath, err)
	}
	version := ""
	if info, statErr := os.Stat(r.eventPath); statErr == nil {
		version = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
	}
	return buildSnapshot(r.root, r.ergoDir, r.eventPath, version, tasks, deps), nil
}

type event struct {
	Type string          `json:"type"`
	TS   string          `json:"ts"`
	Data json.RawMessage `json:"data"`
}

type taskRecord struct {
	Task
	legacyContainer bool
	tombstoned      bool
}

func readEvents(path string) ([]event, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	endsWithNewline := false
	if info, statErr := file.Stat(); statErr == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, readErr := file.ReadAt(last, info.Size()-1); readErr == nil {
			endsWithNewline = last[0] == '\n'
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)
	var (
		events      []event
		pending     []byte
		pendingLine int
		line        int
	)
	decode := func(lineNumber int, data []byte) error {
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			return nil
		}
		var parsed event
		if err := json.Unmarshal(trimmed, &parsed); err != nil {
			snippet := string(trimmed)
			if len(snippet) > 160 {
				snippet = snippet[:160] + "…"
			}
			return fmt.Errorf("%s:%d: invalid JSON in events log: %s (%w)", path, lineNumber, snippet, err)
		}
		events = append(events, parsed)
		return nil
	}
	for scanner.Scan() {
		line++
		if pending != nil {
			if err := decode(pendingLine, pending); err != nil {
				return nil, err
			}
		}
		pending = append(pending[:0], scanner.Bytes()...)
		pendingLine = line
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%s: event line exceeds %d bytes", path, maxEventLineBytes)
		}
		return nil, err
	}
	if pending != nil {
		if err := decode(pendingLine, pending); err != nil {
			if !endsWithNewline {
				return events, nil
			}
			return nil, err
		}
	}
	return events, nil
}

func replay(events []event) (map[string]*taskRecord, map[string]map[string]struct{}, error) {
	tasks := map[string]*taskRecord{}
	deps := map[string]map[string]struct{}{}
	tombstones := map[string]struct{}{}

	for _, current := range events {
		switch current.Type {
		case "new_task", "new_epic":
			var data struct {
				ID        string `json:"id"`
				UUID      string `json:"uuid"`
				EpicID    string `json:"epic_id"`
				State     State  `json:"state"`
				Title     string `json:"title"`
				Body      string `json:"body"`
				CreatedAt string `json:"created_at"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if _, removed := tombstones[data.ID]; removed {
				continue
			}
			if _, exists := tasks[data.ID]; exists {
				return nil, nil, fmt.Errorf("duplicate task id %s", data.ID)
			}
			createdAt, err := parseTime(data.CreatedAt)
			if err != nil {
				return nil, nil, err
			}
			title, body := migrateLegacyTitle(data.Title, data.Body)
			tasks[data.ID] = &taskRecord{
				Task: Task{
					ID:        data.ID,
					UUID:      data.UUID,
					ParentID:  data.EpicID,
					Title:     title,
					Body:      body,
					State:     data.State,
					CreatedAt: createdAt,
					UpdatedAt: createdAt,
				},
				legacyContainer: current.Type == "new_epic",
			}
		case "state":
			var data struct {
				ID    string `json:"id"`
				State State  `json:"state"`
				TS    string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.State = data.State
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
				if data.State == StateTodo || data.State == StateDone || data.State == StateCanceled {
					task.ClaimedBy = ""
				}
			}
		case "claim":
			var data struct {
				ID      string `json:"id"`
				AgentID string `json:"agent_id"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				task.ClaimedBy = data.AgentID
			}
		case "unclaim":
			var data struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				task.ClaimedBy = ""
			}
		case "link", "unlink":
			var data struct {
				FromID string `json:"from_id"`
				ToID   string `json:"to_id"`
				Type   string `json:"type"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if data.Type != "depends" {
				continue
			}
			if _, removed := tombstones[data.FromID]; removed {
				continue
			}
			if _, removed := tombstones[data.ToID]; removed {
				continue
			}
			if current.Type == "link" {
				if deps[data.FromID] == nil {
					deps[data.FromID] = map[string]struct{}{}
				}
				deps[data.FromID][data.ToID] = struct{}{}
			} else if deps[data.FromID] != nil {
				delete(deps[data.FromID], data.ToID)
			}
		case "title":
			var data struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				TS    string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.Title = data.Title
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
			}
		case "body":
			var data struct {
				ID   string `json:"id"`
				Body string `json:"body"`
				TS   string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.Body = data.Body
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
			}
		case "epic":
			var data struct {
				ID     string `json:"id"`
				EpicID string `json:"epic_id"`
				TS     string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.ParentID = data.EpicID
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
			}
		case "result":
			var data struct {
				TaskID            string `json:"task_id"`
				Summary           string `json:"summary"`
				Path              string `json:"path"`
				SHA256AtAttach    string `json:"sha256_at_attach"`
				MtimeAtAttach     string `json:"mtime_at_attach"`
				GitCommitAtAttach string `json:"git_commit_at_attach"`
				TS                string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.TaskID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.Results = append([]Result{{
					Summary:           data.Summary,
					Path:              data.Path,
					SHA256AtAttach:    data.SHA256AtAttach,
					MtimeAtAttach:     data.MtimeAtAttach,
					GitCommitAtAttach: data.GitCommitAtAttach,
					CreatedAt:         timestamp,
				}}, task.Results...)
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
			}
		case "message":
			var data struct {
				TaskID string `json:"task_id"`
				Kind   string `json:"kind"`
				Text   string `json:"text"`
				TS     string `json:"ts"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			if task := tasks[data.TaskID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return nil, nil, err
				}
				task.Messages = append([]Message{{
					Kind:      data.Kind,
					Text:      data.Text,
					CreatedAt: timestamp,
				}}, task.Messages...)
				task.UpdatedAt = later(task.UpdatedAt, timestamp)
			}
		case "tombstone":
			var data struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return nil, nil, err
			}
			tombstones[data.ID] = struct{}{}
			delete(tasks, data.ID)
			delete(deps, data.ID)
			for fromID := range deps {
				delete(deps[fromID], data.ID)
			}
		}
	}
	return tasks, deps, nil
}

func buildSnapshot(root, ergoDir, eventPath, version string, records map[string]*taskRecord, deps map[string]map[string]struct{}) Snapshot {
	children := map[string][]string{}
	reverseDeps := map[string][]string{}
	for id, task := range records {
		if task.ParentID != "" {
			children[task.ParentID] = append(children[task.ParentID], id)
		}
	}
	for fromID, targets := range deps {
		for targetID := range targets {
			reverseDeps[targetID] = append(reverseDeps[targetID], fromID)
		}
	}
	for id, task := range records {
		task.Container = task.legacyContainer || len(children[id]) > 0
		task.Children = sortedIDs(children[id])
		task.Dependencies = sortedSet(deps[id])
		task.Dependents = sortedIDs(reverseDeps[id])
	}
	for _, task := range records {
		if !task.Container {
			continue
		}
		task.Complete = len(task.Children) > 0
		for _, childID := range task.Children {
			child := records[childID]
			if child == nil || (child.State != StateDone && child.State != StateCanceled) {
				task.Complete = false
				break
			}
		}
	}
	dependencyComplete := func(id string) bool {
		task := records[id]
		if task == nil {
			return false
		}
		if task.Container {
			return task.Complete
		}
		return task.State == StateDone || task.State == StateCanceled
	}
	for _, task := range records {
		if task.Container || task.State != StateTodo {
			continue
		}
		task.Ready = true
		allDeps := append([]string(nil), task.Dependencies...)
		if parent := records[task.ParentID]; parent != nil {
			allDeps = append(allDeps, parent.Dependencies...)
		}
		for _, dependencyID := range allDeps {
			if !dependencyComplete(dependencyID) {
				task.Ready = false
				break
			}
		}
		task.Waiting = !task.Ready
	}

	ordered := make([]*taskRecord, 0, len(records))
	for _, task := range records {
		ordered = append(ordered, task)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})

	snapshot := Snapshot{
		Root:      root,
		ErgoDir:   ergoDir,
		EventPath: eventPath,
		Version:   version,
		tasks:     make([]Task, 0, len(ordered)),
		byID:      make(map[string]int, len(ordered)),
	}
	for _, record := range ordered {
		task := cloneTask(record.Task)
		snapshot.byID[task.ID] = len(snapshot.tasks)
		snapshot.tasks = append(snapshot.tasks, task)
		if task.ParentID == "" {
			snapshot.Roots = append(snapshot.Roots, task.ID)
		}
		if task.Container {
			snapshot.Summary.Containers++
			continue
		}
		snapshot.Summary.Total++
		switch task.State {
		case StateTodo:
			if task.Ready {
				snapshot.Summary.Ready++
			} else {
				snapshot.Summary.Waiting++
			}
		case StateDoing:
			snapshot.Summary.Doing++
		case StateBlocked:
			snapshot.Summary.Blocked++
		case StateError:
			snapshot.Summary.Error++
		case StateDone:
			snapshot.Summary.Done++
		case StateCanceled:
			snapshot.Summary.Canceled++
		}
	}
	sort.Strings(snapshot.Roots)
	return snapshot
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: %w", value, err)
	}
	return timestamp, nil
}

func later(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedIDs(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func migrateLegacyTitle(title, body string) (string, string) {
	if strings.TrimSpace(title) != "" {
		return title, body
	}
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isMarkdownHeading(trimmed) {
			continue
		}
		if index+1 == len(lines) {
			return trimmed, ""
		}
		return trimmed, strings.Join(lines[index+1:], "\n")
	}
	if strings.TrimSpace(body) == "" {
		return "(untitled)", ""
	}
	return "(untitled)", body
}

func isMarkdownHeading(value string) bool {
	trimmed := strings.TrimLeft(value, "#")
	return trimmed != value && strings.TrimSpace(trimmed) != ""
}
