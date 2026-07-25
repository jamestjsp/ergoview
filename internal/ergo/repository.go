package ergo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxEventLineBytes = 10 * 1024 * 1024

type Repository struct {
	root      string
	ergoDir   string
	eventPath string
	mu        sync.Mutex
	state     *repositoryState
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
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	events, _, _, _, err := readEventRange(file, path, 0, info.Size(), 0)
	return events, err
}

func readEventRange(file *os.File, path string, offset, size int64, lines int) ([]event, int64, int, bool, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(file, offset, size-offset), 64*1024)
	events := make([]event, 0)
	committedOffset := offset
	committedLines := lines

	for {
		lineStart := committedOffset
		data, err := reader.ReadBytes('\n')
		if len(data) > maxEventLineBytes+1 {
			return nil, offset, lines, false, fmt.Errorf("%s: event line exceeds %d bytes", path, maxEventLineBytes)
		}
		if len(data) == 0 && errors.Is(err, io.EOF) {
			return events, committedOffset, committedLines, false, nil
		}

		lineNumber := committedLines + 1
		parsed, present, decodeErr := decodeEventLine(path, lineNumber, data)
		if decodeErr != nil {
			if errors.Is(err, io.EOF) {
				return events, lineStart, committedLines, false, nil
			}
			return nil, offset, lines, false, decodeErr
		}
		if present {
			events = append(events, parsed)
		}
		committedOffset += int64(len(data))
		committedLines++

		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return events, committedOffset, committedLines, true, nil
		}
		return nil, offset, lines, false, err
	}
}

func decodeEventLine(path string, lineNumber int, data []byte) (event, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return event{}, false, nil
	}
	var parsed event
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		snippet := string(trimmed)
		if len(snippet) > 160 {
			snippet = snippet[:160] + "…"
		}
		return event{}, false, fmt.Errorf("%s:%d: invalid JSON in events log: %s (%w)", path, lineNumber, snippet, err)
	}
	return parsed, true, nil
}

func replay(events []event) (map[string]*taskRecord, map[string]map[string]struct{}, error) {
	state := newReplayState()
	if err := replayInto(&state, events); err != nil {
		return nil, nil, err
	}
	return state.tasks, state.deps, nil
}

type replayState struct {
	tasks      map[string]*taskRecord
	deps       map[string]map[string]struct{}
	tombstones map[string]struct{}
}

func newReplayState() replayState {
	return replayState{
		tasks:      map[string]*taskRecord{},
		deps:       map[string]map[string]struct{}{},
		tombstones: map[string]struct{}{},
	}
}

func replayInto(state *replayState, events []event) error {
	tasks := state.tasks
	deps := state.deps
	tombstones := state.tombstones

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
				return err
			}
			if _, removed := tombstones[data.ID]; removed {
				continue
			}
			if _, exists := tasks[data.ID]; exists {
				return fmt.Errorf("duplicate task id %s", data.ID)
			}
			createdAt, err := parseTime(data.CreatedAt)
			if err != nil {
				return err
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
				return err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			if task := tasks[data.ID]; task != nil {
				task.ClaimedBy = data.AgentID
			}
		case "unclaim":
			var data struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return err
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
				return err
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
				return err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			if task := tasks[data.ID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			if task := tasks[data.TaskID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			if task := tasks[data.TaskID]; task != nil {
				timestamp, err := parseTime(data.TS)
				if err != nil {
					return err
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
				return err
			}
			tombstones[data.ID] = struct{}{}
			delete(tasks, data.ID)
			delete(deps, data.ID)
			for fromID := range deps {
				delete(deps[fromID], data.ID)
			}
		}
	}
	return nil
}

func buildSnapshot(root, ergoDir, eventPath, version string, records map[string]*taskRecord, deps map[string]map[string]struct{}) Snapshot {
	children := map[string][]string{}
	reverseDeps := map[string][]string{}
	for _, task := range records {
		task.Container = task.legacyContainer
		task.Complete = false
		task.Ready = false
		task.Waiting = false
		task.Dependencies = nil
		task.Dependents = nil
		task.Children = nil
	}
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
