package ergo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type journalFile struct {
	Path              string `json:"path"`
	SHA256            string `json:"sha256"`
	Mtime             string `json:"mtime,omitempty"`
	GitCommitAtAttach string `json:"git_commit,omitempty"`
}

type journalEntry struct {
	Version int          `json:"version"`
	TaskID  string       `json:"task_id"`
	Kind    string       `json:"kind"`
	At      string       `json:"at"`
	Agent   string       `json:"agent,omitempty"`
	Text    string       `json:"text,omitempty"`
	File    *journalFile `json:"file,omitempty"`
}

func readJournalFile(path string) ([]journalEntry, fileState, error) {
	file, source, err := openFile(path)
	if err != nil || file == nil {
		return nil, source, err
	}
	defer file.Close()
	endsWithNewline := false
	if source.info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, source.info.Size()-1); err == nil {
			endsWithNewline = last[0] == '\n'
		}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, source, err
	}
	var entries []journalEntry
	lines := bytes.Split(data, []byte{'\n'})
	for index, raw := range lines {
		if index == len(lines)-1 && len(raw) == 0 {
			break
		}
		lineNumber := index + 1
		if len(raw) > maxEventLineBytes {
			return nil, source, fmt.Errorf("%s:%d: journal record exceeds %d bytes", path, lineNumber, maxEventLineBytes)
		}
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			if !endsWithNewline && index == len(lines)-1 {
				break
			}
			return nil, source, fmt.Errorf("%s:%d: invalid JSON in journal: %w", path, lineNumber, err)
		}
		if err := validateJournalEntry(entry); err != nil {
			return nil, source, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}
		entries = append(entries, entry)
	}
	return entries, source, nil
}

func validateJournalEntry(entry journalEntry) error {
	if entry.Version != 1 {
		return fmt.Errorf("unsupported journal version %d", entry.Version)
	}
	if strings.TrimSpace(entry.TaskID) == "" {
		return errors.New("journal task_id is required")
	}
	switch entry.Kind {
	case "created", "claim", "done", "fail", "block", "cancel", "open", "release", "result":
	default:
		return fmt.Errorf("invalid journal kind %q", entry.Kind)
	}
	if _, err := parseTime(entry.At); err != nil {
		return fmt.Errorf("invalid journal timestamp: %w", err)
	}
	if entry.Kind == "result" && strings.TrimSpace(entry.Text) == "" {
		return errors.New("journal result text is required")
	}
	if entry.File != nil {
		if entry.Kind != "result" {
			return errors.New("only result journal entries may attach files")
		}
		if entry.File.Path == "" {
			return errors.New("journal file path is required")
		}
	}
	return nil
}

func hydrateJournalEvidence(records map[string]*taskRecord, entries []journalEntry) {
	for _, entry := range entries {
		task := records[entry.TaskID]
		if task == nil {
			continue
		}
		createdAt, err := parseTime(entry.At)
		if err != nil {
			continue
		}
		if entry.Kind == "result" {
			result := Result{Summary: entry.Text, CreatedAt: createdAt}
			if entry.File != nil {
				result.Path = entry.File.Path
				result.SHA256AtAttach = entry.File.SHA256
				result.MtimeAtAttach = entry.File.Mtime
				result.GitCommitAtAttach = entry.File.GitCommitAtAttach
			}
			if !containsResult(task.Results, result) {
				task.Results = append(task.Results, result)
			}
			continue
		}
		if entry.Text == "" {
			continue
		}
		message := Message{Kind: entry.Kind, Text: entry.Text, CreatedAt: createdAt}
		if !containsMessage(task.Messages, message) {
			task.Messages = append(task.Messages, message)
		}
	}
	for _, task := range records {
		sort.SliceStable(task.Results, func(left, right int) bool {
			return task.Results[left].CreatedAt.After(task.Results[right].CreatedAt)
		})
		sort.SliceStable(task.Messages, func(left, right int) bool {
			return task.Messages[left].CreatedAt.After(task.Messages[right].CreatedAt)
		})
	}
}

func containsResult(results []Result, candidate Result) bool {
	for _, result := range results {
		if result.Summary == candidate.Summary && result.Path == candidate.Path &&
			result.SHA256AtAttach == candidate.SHA256AtAttach && result.CreatedAt.Equal(candidate.CreatedAt) {
			return true
		}
	}
	return false
}

func containsMessage(messages []Message, candidate Message) bool {
	for _, message := range messages {
		if message.Kind == candidate.Kind && message.Text == candidate.Text && message.CreatedAt.Equal(candidate.CreatedAt) {
			return true
		}
	}
	return false
}
