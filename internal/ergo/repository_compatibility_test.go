package ergo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryLoadsReleasedErgoBacklogs(t *testing.T) {
	fixtures := []struct {
		name, file, log, epic, first, second string
		secondState                          State
		secondClaim, message, block          string
	}{
		{name: "v1", file: "v1.0.0-events.jsonl", log: "events.jsonl", epic: "RHMKMK", first: "TSDLBT", second: "S3BKFM", secondState: StateError, secondClaim: "fixture@v1.0.0"},
		{name: "v2", file: "v2.0.0-plans.jsonl", log: "plans.jsonl", epic: "QZ43EH", first: "YDSBYY", second: "HAVLMC", secondState: StateBlocked},
		{name: "v3", file: "v3.0.0-plans.jsonl", log: "plans.jsonl", epic: "27P2GS", first: "RJZVUO", second: "KMLJS2", secondState: StateBlocked, message: "v3.0.0 completed", block: "v3.0.0 waiting"},
		{name: "v4", file: "v4.0.0-backlog.jsonl", log: "backlog.jsonl", epic: "NX5XPB", first: "NNZXBA", second: "64EBIM", secondState: StateBlocked, message: "v4.0.0 completed", block: "v4.0.0 waiting"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := fixtureFromUpstream(t, fixture.log, fixture.file)
			repository, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := repository.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.AllTasks()) != 3 {
				t.Fatalf("task count = %d, want 3", len(snapshot.AllTasks()))
			}
			epic, epicOK := snapshot.Task(fixture.epic)
			first, firstOK := snapshot.Task(fixture.first)
			second, secondOK := snapshot.Task(fixture.second)
			if !epicOK || !firstOK || !secondOK || !epic.Container {
				t.Fatalf("released tasks missing or epic not derived: epic=%#v first=%v second=%v", epic, firstOK, secondOK)
			}
			if first.State != StateDone || len(first.Results) != 1 || first.Results[0].Path != "go.mod" {
				t.Fatalf("first task state/results = %s/%#v", first.State, first.Results)
			}
			if second.State != fixture.secondState || second.ClaimedBy != fixture.secondClaim {
				t.Fatalf("second state/claim = %s/%q, want %s/%q", second.State, second.ClaimedBy, fixture.secondState, fixture.secondClaim)
			}
			if strings.Join(second.Dependencies, ",") != fixture.first {
				t.Fatalf("second dependencies = %v, want %s", second.Dependencies, fixture.first)
			}
			assertOnlyMessageText(t, first.Messages, fixture.message)
			assertOnlyMessageText(t, second.Messages, fixture.block)
		})
	}
}

func TestRepositoryLoadsCompactedSnapshotAndAppendedTransaction(t *testing.T) {
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.Mkdir(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ergoDir, "backlog.jsonl")
	records := []any{
		snapshotTaskRecord{Type: snapshotTaskRecordType, ID: "EPIC01", UUID: "1", ExplicitEpic: true, State: StateTodo, Title: "Release", CreatedAt: "2026-08-21T10:00:00Z", UpdatedAt: "2026-08-21T10:00:00Z"},
		snapshotTaskRecord{Type: snapshotTaskRecordType, ID: "TASK01", UUID: "2", EpicID: "EPIC01", State: StateTodo, Title: "Old title", CreatedAt: "2026-08-21T10:01:00Z", UpdatedAt: "2026-08-21T10:01:00Z"},
		snapshotResultRecord{Type: snapshotResultRecordType, TaskID: "TASK01", Ordinal: 0, Summary: "Legacy result", Path: "go.mod", SHA256AtAttach: "abc", CreatedAt: "2026-08-21T10:02:00Z"},
		snapshotMessageRecord{Type: snapshotMessageRecordType, TaskID: "TASK01", Ordinal: 0, Kind: "block", Text: "Legacy note", CreatedAt: "2026-08-21T10:03:00Z"},
	}
	writeSnapshot(t, path, 2, 1, 1, 0, records)
	appendTransaction(t, path, event{Type: "title", TS: "2026-08-21T10:04:00Z", Data: mustJSON(t, map[string]any{
		"id": "TASK01", "title": "Current title", "ts": "2026-08-21T10:04:00Z",
	})})

	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	task, ok := snapshot.Task("TASK01")
	if !ok || task.Title != "Current title" || len(task.Results) != 1 || len(task.Messages) != 1 {
		t.Fatalf("compacted task = %#v, found = %v", task, ok)
	}
	epic, _ := snapshot.Task("EPIC01")
	if !epic.Container || len(epic.Children) != 1 {
		t.Fatalf("explicit compacted epic = %#v", epic)
	}
}

func TestRepositoryHydratesJournalAndUsesErgo6StateSemantics(t *testing.T) {
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.Mkdir(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ergoDir, "backlog.jsonl")
	appendTransaction(t, path,
		newTaskForTest(t, "EPIC01", "", StateTodo, "Epic", "2026-08-21T10:00:00Z"),
		newTaskForTest(t, "FAILED", "EPIC01", StateFailed, "Failed attempt", "2026-08-21T10:01:00Z"),
		newTaskForTest(t, "CANCEL", "EPIC01", StateCanceled, "Canceled branch", "2026-08-21T10:02:00Z"),
		newTaskForTest(t, "DRAFT1", "", StateDraft, "Staged work", "2026-08-21T10:03:00Z"),
		newTaskForTest(t, "FOLLOW", "", StateTodo, "Follow up", "2026-08-21T10:04:00Z"),
		event{Type: "link", TS: "2026-08-21T10:05:00Z", Data: mustJSON(t, map[string]any{"from_id": "FOLLOW", "to_id": "EPIC01", "type": "depends"})},
	)
	journalPath := filepath.Join(ergoDir, "journal.jsonl")
	writeJournal(t, journalPath,
		journalEntry{Version: 1, TaskID: "FAILED", Kind: "fail", At: "2026-08-21T10:06:00Z", Agent: "codex@host", Text: "Attempt exhausted"},
		journalEntry{Version: 1, TaskID: "FAILED", Kind: "result", At: "2026-08-21T10:07:00Z", Text: "Failure report", File: &journalFile{Path: "docs/failure.md", SHA256: "def"}},
	)

	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	if initial.Summary.Draft != 1 || initial.Summary.Failed != 1 || initial.Summary.Ready != 1 {
		t.Fatalf("summary = %#v", initial.Summary)
	}
	epic, _ := initial.Task("EPIC01")
	if !epic.Container || !epic.Complete || epic.State != StateFailed {
		t.Fatalf("derived failed epic = %#v", epic)
	}
	follow, _ := initial.Task("FOLLOW")
	if !follow.Ready || follow.Waiting {
		t.Fatalf("failed epic did not release dependency: %#v", follow)
	}
	draft, _ := initial.Task("DRAFT1")
	if draft.Ready || draft.Waiting {
		t.Fatalf("draft readiness = %#v", draft)
	}
	failed, _ := initial.Task("FAILED")
	if len(failed.Messages) != 1 || failed.Messages[0].Text != "Attempt exhausted" ||
		len(failed.Results) != 1 || failed.Results[0].Path != "docs/failure.md" {
		t.Fatalf("journal evidence = messages:%#v results:%#v", failed.Messages, failed.Results)
	}

	appendJournal(t, journalPath, journalEntry{Version: 1, TaskID: "FAILED", Kind: "result", At: "2026-08-21T10:08:00Z", Text: "Second result"})
	updated, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updated.Version == initial.Version {
		t.Fatalf("journal-only update = changed:%v versions:%q/%q", changed, initial.Version, updated.Version)
	}
	failed, _ = updated.Task("FAILED")
	if len(failed.Results) != 2 || failed.Results[0].Summary != "Second result" {
		t.Fatalf("updated results = %#v", failed.Results)
	}
}

func TestRepositoryRejectsCorruptSnapshotAndJournal(t *testing.T) {
	t.Run("snapshot checksum", func(t *testing.T) {
		root := t.TempDir()
		ergoDir := filepath.Join(root, ".ergo")
		if err := os.Mkdir(ergoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		record := snapshotTaskRecord{Type: snapshotTaskRecordType, ID: "TASK01", State: StateTodo, Title: "Task", CreatedAt: "2026-08-21T10:00:00Z", UpdatedAt: "2026-08-21T10:00:00Z"}
		writeSnapshot(t, filepath.Join(ergoDir, "backlog.jsonl"), 1, 0, 0, 0, []any{record})
		path := filepath.Join(ergoDir, "backlog.jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), `"sha256":"`, `"sha256":"00`, 1))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		repository, _ := Open(root)
		if _, err := repository.Load(); err == nil || !strings.Contains(err.Error(), "snapshot integrity mismatch") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("complete journal record", func(t *testing.T) {
		root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
		path := filepath.Join(root, ".ergo", "journal.jsonl")
		if err := os.WriteFile(path, []byte("{broken}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		repository, _ := Open(root)
		if _, err := repository.Load(); err == nil || !strings.Contains(err.Error(), "journal.jsonl:1") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("truncated journal tail", func(t *testing.T) {
		root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
		path := filepath.Join(root, ".ergo", "journal.jsonl")
		if err := os.WriteFile(path, []byte(`{"version":1,"task_id":"SCHEMA","kind":"done","at":"2026-08-21T10:00:00Z","text":"kept"}`+"\n"+`{"version":`), 0o644); err != nil {
			t.Fatal(err)
		}
		repository, _ := Open(root)
		snapshot, err := repository.Load()
		if err != nil {
			t.Fatal(err)
		}
		task, _ := snapshot.Task("SCHEMA")
		if len(task.Messages) != 2 || task.Messages[0].Text != "kept" {
			t.Fatalf("messages = %#v", task.Messages)
		}
	})
}

func fixtureFromUpstream(t *testing.T, destination, fixture string) string {
	t.Helper()
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.Mkdir(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "..", "upstream", "ergo", "internal", "ergo", "testdata", "compatibility", fixture)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read pinned upstream fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ergoDir, destination), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertOnlyMessageText(t *testing.T, messages []Message, want string) {
	t.Helper()
	if want == "" {
		if len(messages) != 0 {
			t.Fatalf("messages = %#v, want none", messages)
		}
		return
	}
	if len(messages) != 1 || messages[0].Text != want {
		t.Fatalf("messages = %#v, want %q", messages, want)
	}
}

func newTaskForTest(t *testing.T, id, epic string, state State, title, at string) event {
	t.Helper()
	return event{Type: "new_task", TS: at, Data: mustJSON(t, map[string]any{
		"id": id, "uuid": id + "-uuid", "epic_id": epic, "state": state, "title": title, "body": "", "created_at": at,
	})}
}

func appendTransaction(t *testing.T, path string, events ...event) {
	t.Helper()
	data, err := json.Marshal(transactionRecord{Type: transactionRecordType, Version: 1, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeSnapshot(t *testing.T, path string, tasks, results, messages, dependencies int, records []any) {
	t.Helper()
	hasher := sha256.New()
	lines := make([][]byte, 0, len(records)+1)
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		hasher.Write(line)
		hasher.Write([]byte{'\n'})
		lines = append(lines, line)
	}
	manifest, err := json.Marshal(snapshotManifest{
		Type: snapshotRecordType, Version: 1, Tasks: tasks, Results: results,
		Messages: messages, Dependencies: dependencies, SHA256: hex.EncodeToString(hasher.Sum(nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	data := append(manifest, '\n')
	for _, line := range lines {
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJournal(t *testing.T, path string, entries ...journalEntry) {
	t.Helper()
	for _, entry := range entries {
		appendJournal(t, path, entry)
	}
}

func appendJournal(t *testing.T, path string, entry journalEntry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
