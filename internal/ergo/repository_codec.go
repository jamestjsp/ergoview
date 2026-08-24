package ergo

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

const (
	transactionRecordType        = "transaction"
	snapshotRecordType           = "snapshot"
	snapshotTaskRecordType       = "snapshot_task"
	snapshotResultRecordType     = "snapshot_result"
	snapshotMessageRecordType    = "snapshot_message"
	snapshotDependencyRecordType = "snapshot_dependency"
)

type transactionRecord struct {
	Type    string  `json:"type"`
	Version int     `json:"version"`
	Events  []event `json:"events"`
}

type backlogRead struct {
	events   []event
	snapshot *replayState
	offset   int64
	lines    int
	openLine bool
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
	read, err := readBacklogRange(file, path, 0, info.Size(), 0)
	if err != nil {
		return nil, err
	}
	if read.snapshot != nil {
		return nil, fmt.Errorf("%s: compacted snapshot cannot be represented as an event slice", path)
	}
	return read.events, nil
}

func readBacklogRange(file *os.File, path string, offset, size int64, lines int) (backlogRead, error) {
	result := backlogRead{offset: offset, lines: lines}
	reader := bufio.NewReaderSize(io.NewSectionReader(file, offset, size-offset), 64*1024)
	seenRecords := 0
	var snapshotDecoder *snapshotBlockDecoder

	process := func(lineNumber int, data []byte) error {
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			return nil
		}
		seenRecords++
		if snapshotDecoder != nil && snapshotDecoder.seen < snapshotDecoder.total() {
			if err := snapshotDecoder.consume(lineNumber, trimmed); err != nil {
				return err
			}
			if snapshotDecoder.seen == snapshotDecoder.total() {
				snapshot, err := snapshotDecoder.finish()
				if err != nil {
					return err
				}
				result.snapshot = snapshot
			}
			return nil
		}

		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(trimmed, &header); err != nil {
			return formatLogJSONError(path, lineNumber, trimmed, err)
		}
		if header.Type == snapshotRecordType {
			if offset != 0 || seenRecords != 1 || result.snapshot != nil || snapshotDecoder != nil {
				return fmt.Errorf("%s:%d: snapshot manifest must be the first and only snapshot", path, lineNumber)
			}
			decoder, err := newSnapshotBlockDecoder(path, lineNumber, trimmed)
			if err != nil {
				return err
			}
			snapshotDecoder = decoder
			if decoder.total() == 0 {
				snapshot, err := decoder.finish()
				if err != nil {
					return err
				}
				result.snapshot = snapshot
			}
			return nil
		}
		if isSnapshotRecordKind(header.Type) {
			return fmt.Errorf("%s:%d: snapshot data record outside a snapshot block: %q", path, lineNumber, header.Type)
		}
		events, err := decodeEventLogRecord(path, lineNumber, trimmed)
		if err != nil {
			return err
		}
		result.events = append(result.events, events...)
		return nil
	}

	for {
		lineStart := result.offset
		data, err := readBoundedLine(reader, maxEventLineBytes)
		if errors.Is(err, errEventLineTooLong) {
			return backlogRead{}, fmt.Errorf("%s: event line exceeds %d bytes", path, maxEventLineBytes)
		}
		if len(data) == 0 && errors.Is(err, io.EOF) {
			break
		}
		lineNumber := result.lines + 1
		if processErr := process(lineNumber, data); processErr != nil {
			if errors.Is(err, io.EOF) && !json.Valid(bytes.TrimSpace(data)) {
				result.offset = lineStart
				break
			}
			return backlogRead{}, processErr
		}
		result.offset += int64(len(data))
		result.lines++
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			result.openLine = true
			break
		}
		return backlogRead{}, err
	}
	if snapshotDecoder != nil && snapshotDecoder.seen != snapshotDecoder.total() {
		return backlogRead{}, fmt.Errorf("%s:%d: incomplete snapshot: got %d of %d data records",
			path, snapshotDecoder.line, snapshotDecoder.seen, snapshotDecoder.total())
	}
	return result, nil
}

func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		contentBytes := len(line) + len(fragment)
		if err == nil && len(fragment) > 0 && fragment[len(fragment)-1] == '\n' {
			contentBytes--
		}
		if contentBytes > limit {
			return nil, errEventLineTooLong
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

func decodeEventLogRecord(path string, lineNumber int, data []byte) ([]event, error) {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, formatLogJSONError(path, lineNumber, data, err)
	}
	if header.Type != transactionRecordType {
		var parsed event
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, formatLogJSONError(path, lineNumber, data, err)
		}
		return []event{parsed}, nil
	}
	var record transactionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, formatLogJSONError(path, lineNumber, data, err)
	}
	if record.Version != 1 {
		return nil, fmt.Errorf("%s:%d: unsupported transaction record version %d", path, lineNumber, record.Version)
	}
	if len(record.Events) == 0 {
		return nil, fmt.Errorf("%s:%d: transaction record contains no events", path, lineNumber)
	}
	return record.Events, nil
}

func formatLogJSONError(path string, lineNumber int, data []byte, cause error) error {
	snippet := string(data)
	if len(snippet) > 160 {
		snippet = snippet[:160] + "…"
	}
	return fmt.Errorf("%s:%d: invalid JSON in events log: %s (%w)", path, lineNumber, snippet, cause)
}

type snapshotManifest struct {
	Type         string `json:"type"`
	Version      int    `json:"version"`
	Tasks        int    `json:"tasks"`
	Results      int    `json:"results"`
	Messages     int    `json:"messages"`
	Dependencies int    `json:"dependencies"`
	SHA256       string `json:"sha256"`
}

type snapshotTaskRecord struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	UUID         string `json:"uuid"`
	EpicID       string `json:"epic_id"`
	ExplicitEpic bool   `json:"explicit_epic"`
	State        State  `json:"state"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	ClaimedBy    string `json:"claimed_by"`
	ClaimedAt    string `json:"claimed_at"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type snapshotResultRecord struct {
	Type              string `json:"type"`
	TaskID            string `json:"task_id"`
	Ordinal           int    `json:"ordinal"`
	Summary           string `json:"summary"`
	Path              string `json:"path"`
	SHA256AtAttach    string `json:"sha256_at_attach"`
	MtimeAtAttach     string `json:"mtime_at_attach"`
	GitCommitAtAttach string `json:"git_commit_at_attach"`
	CreatedAt         string `json:"created_at"`
}

type snapshotMessageRecord struct {
	Type      string `json:"type"`
	TaskID    string `json:"task_id"`
	Ordinal   int    `json:"ordinal"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type snapshotDependencyRecord struct {
	Type   string `json:"type"`
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
}

type snapshotBlockDecoder struct {
	path               string
	line               int
	manifest           snapshotManifest
	seen               int
	hash               hash.Hash
	replay             replayState
	lastTask           string
	lastResultTask     string
	lastMessageTask    string
	lastDependencyFrom string
	lastDependencyTo   string
}

func newSnapshotBlockDecoder(path string, line int, data []byte) (*snapshotBlockDecoder, error) {
	var manifest snapshotManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s:%d: invalid snapshot manifest: %w", path, line, err)
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("%s:%d: unsupported snapshot version %d", path, line, manifest.Version)
	}
	if manifest.Tasks < 0 || manifest.Results < 0 || manifest.Messages < 0 || manifest.Dependencies < 0 {
		return nil, fmt.Errorf("%s:%d: snapshot counts cannot be negative", path, line)
	}
	return &snapshotBlockDecoder{
		path: path, line: line, manifest: manifest, hash: sha256.New(), replay: newReplayState(),
	}, nil
}

func (decoder *snapshotBlockDecoder) total() int {
	return decoder.manifest.Tasks + decoder.manifest.Results + decoder.manifest.Messages + decoder.manifest.Dependencies
}

func (decoder *snapshotBlockDecoder) consume(lineNumber int, data []byte) error {
	decoder.hash.Write(data)
	decoder.hash.Write([]byte{'\n'})
	index := decoder.seen
	switch {
	case index < decoder.manifest.Tasks:
		var record snapshotTaskRecord
		if err := decodeSnapshotRecord(decoder.path, lineNumber, data, snapshotTaskRecordType, &record); err != nil {
			return err
		}
		if record.ID == "" || decoder.replay.tasks[record.ID] != nil {
			return fmt.Errorf("%s:%d: invalid or duplicate snapshot task id %q", decoder.path, lineNumber, record.ID)
		}
		if decoder.lastTask != "" && record.ID <= decoder.lastTask {
			return fmt.Errorf("%s:%d: snapshot tasks are not in increasing ID order", decoder.path, lineNumber)
		}
		createdAt, err := parseTime(record.CreatedAt)
		if err != nil {
			return fmt.Errorf("%s:%d: snapshot task %s has invalid created_at: %w", decoder.path, lineNumber, record.ID, err)
		}
		updatedAt, err := parseTime(record.UpdatedAt)
		if err != nil {
			return fmt.Errorf("%s:%d: snapshot task %s has invalid updated_at: %w", decoder.path, lineNumber, record.ID, err)
		}
		if record.ClaimedAt != "" {
			if _, err := parseTime(record.ClaimedAt); err != nil {
				return fmt.Errorf("%s:%d: snapshot task %s has invalid claimed_at: %w", decoder.path, lineNumber, record.ID, err)
			}
		}
		if !isReadableState(record.State) {
			return fmt.Errorf("%s:%d: snapshot task %s has invalid state %q", decoder.path, lineNumber, record.ID, record.State)
		}
		if record.State == StateDoing && record.ClaimedBy == "" {
			return fmt.Errorf("%s:%d: snapshot task %s is doing without a claim", decoder.path, lineNumber, record.ID)
		}
		if record.State != StateDoing && record.State != StateError && record.ClaimedBy != "" {
			return fmt.Errorf("%s:%d: snapshot task %s has claim in state %q", decoder.path, lineNumber, record.ID, record.State)
		}
		if record.ExplicitEpic && record.EpicID != "" {
			return fmt.Errorf("%s:%d: explicit snapshot epic %s cannot have a parent", decoder.path, lineNumber, record.ID)
		}
		decoder.lastTask = record.ID
		decoder.replay.tasks[record.ID] = &taskRecord{
			Task: Task{
				ID: record.ID, UUID: record.UUID, ParentID: record.EpicID, State: record.State,
				Title: record.Title, Body: record.Body, ClaimedBy: record.ClaimedBy,
				CreatedAt: createdAt, UpdatedAt: updatedAt,
			},
			legacyContainer: record.ExplicitEpic,
		}
	case index < decoder.manifest.Tasks+decoder.manifest.Results:
		var record snapshotResultRecord
		if err := decodeSnapshotRecord(decoder.path, lineNumber, data, snapshotResultRecordType, &record); err != nil {
			return err
		}
		task := decoder.replay.tasks[record.TaskID]
		if task == nil || record.Ordinal != len(task.Results) {
			return fmt.Errorf("%s:%d: invalid snapshot result task/ordinal %s/%d", decoder.path, lineNumber, record.TaskID, record.Ordinal)
		}
		if decoder.lastResultTask != "" && record.TaskID < decoder.lastResultTask {
			return fmt.Errorf("%s:%d: snapshot results are not ordered by task ID", decoder.path, lineNumber)
		}
		createdAt, err := parseTime(record.CreatedAt)
		if err != nil {
			return fmt.Errorf("%s:%d: snapshot result for %s has invalid created_at: %w", decoder.path, lineNumber, record.TaskID, err)
		}
		decoder.lastResultTask = record.TaskID
		task.Results = append(task.Results, Result{
			Summary: record.Summary, Path: record.Path, SHA256AtAttach: record.SHA256AtAttach,
			MtimeAtAttach: record.MtimeAtAttach, GitCommitAtAttach: record.GitCommitAtAttach, CreatedAt: createdAt,
		})
	case index < decoder.manifest.Tasks+decoder.manifest.Results+decoder.manifest.Messages:
		var record snapshotMessageRecord
		if err := decodeSnapshotRecord(decoder.path, lineNumber, data, snapshotMessageRecordType, &record); err != nil {
			return err
		}
		task := decoder.replay.tasks[record.TaskID]
		if task == nil || record.Ordinal != len(task.Messages) {
			return fmt.Errorf("%s:%d: invalid snapshot message task/ordinal %s/%d", decoder.path, lineNumber, record.TaskID, record.Ordinal)
		}
		if decoder.lastMessageTask != "" && record.TaskID < decoder.lastMessageTask {
			return fmt.Errorf("%s:%d: snapshot messages are not ordered by task ID", decoder.path, lineNumber)
		}
		createdAt, err := parseTime(record.CreatedAt)
		if err != nil {
			return fmt.Errorf("%s:%d: snapshot message for %s has invalid created_at: %w", decoder.path, lineNumber, record.TaskID, err)
		}
		decoder.lastMessageTask = record.TaskID
		task.Messages = append(task.Messages, Message{Kind: record.Kind, Text: record.Text, CreatedAt: createdAt})
	default:
		var record snapshotDependencyRecord
		if err := decodeSnapshotRecord(decoder.path, lineNumber, data, snapshotDependencyRecordType, &record); err != nil {
			return err
		}
		if decoder.lastDependencyFrom != "" &&
			(record.FromID < decoder.lastDependencyFrom ||
				(record.FromID == decoder.lastDependencyFrom && record.ToID <= decoder.lastDependencyTo)) {
			return fmt.Errorf("%s:%d: snapshot dependencies are not in increasing endpoint order", decoder.path, lineNumber)
		}
		if decoder.replay.tasks[record.FromID] == nil || decoder.replay.tasks[record.ToID] == nil {
			return fmt.Errorf("%s:%d: dangling snapshot dependency %s -> %s", decoder.path, lineNumber, record.FromID, record.ToID)
		}
		if record.FromID == record.ToID || dependencyPathExists(decoder.replay.deps, record.ToID, record.FromID) {
			return fmt.Errorf("%s:%d: cyclic snapshot dependency %s -> %s", decoder.path, lineNumber, record.FromID, record.ToID)
		}
		if decoder.replay.deps[record.FromID] == nil {
			decoder.replay.deps[record.FromID] = map[string]struct{}{}
		}
		decoder.lastDependencyFrom, decoder.lastDependencyTo = record.FromID, record.ToID
		decoder.replay.deps[record.FromID][record.ToID] = struct{}{}
	}
	decoder.seen++
	return nil
}

func dependencyPathExists(dependencies map[string]map[string]struct{}, from, target string) bool {
	seen := map[string]struct{}{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == target {
			return true
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
		for dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		return false
	}
	return visit(from)
}

func (decoder *snapshotBlockDecoder) finish() (*replayState, error) {
	if got := hex.EncodeToString(decoder.hash.Sum(nil)); got != decoder.manifest.SHA256 {
		return nil, fmt.Errorf("%s:%d: snapshot integrity mismatch: got %s, want %s", decoder.path, decoder.line, got, decoder.manifest.SHA256)
	}
	return &decoder.replay, nil
}

func decodeSnapshotRecord(path string, lineNumber int, data []byte, want string, destination any) error {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("%s:%d: invalid snapshot record: %w", path, lineNumber, err)
	}
	if header.Type != want {
		return fmt.Errorf("%s:%d: snapshot record order: got %q, want %q", path, lineNumber, header.Type, want)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("%s:%d: invalid %s record: %w", path, lineNumber, want, err)
	}
	return nil
}

func isSnapshotRecordKind(kind string) bool {
	switch kind {
	case snapshotRecordType, snapshotTaskRecordType, snapshotResultRecordType,
		snapshotMessageRecordType, snapshotDependencyRecordType:
		return true
	default:
		return false
	}
}

func isReadableState(state State) bool {
	switch state {
	case StateDraft, StateTodo, StateDoing, StateBlocked, StateDone, StateFailed, StateCanceled, StateError:
		return true
	default:
		return false
	}
}
