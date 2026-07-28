package ergo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryLoadsCurrentContract(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(filepath.Join(root, "nested", "directory"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Root != root {
		t.Fatalf("root = %q, want %q", snapshot.Root, root)
	}
	if len(snapshot.AllTasks()) != 6 {
		t.Fatalf("task count = %d, want 6", len(snapshot.AllTasks()))
	}
	if snapshot.Summary != (Summary{
		Containers: 1,
		Ready:      1,
		Waiting:    1,
		Doing:      1,
		Blocked:    1,
		Done:       1,
		Total:      5,
	}) {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}

	container, ok := snapshot.Task("EPIC01")
	if !ok || !container.Container || container.Complete {
		t.Fatalf("container = %#v, found = %v", container, ok)
	}
	if strings.Join(container.Children, ",") != "SCHEMA,TOKENS" {
		t.Fatalf("children = %v", container.Children)
	}
	tokenTask, _ := snapshot.Task("TOKENS")
	if !tokenTask.Ready || tokenTask.Waiting {
		t.Fatalf("token task readiness = ready:%v waiting:%v", tokenTask.Ready, tokenTask.Waiting)
	}
	docsTask, _ := snapshot.Task("DOCS01")
	if docsTask.Ready || !docsTask.Waiting {
		t.Fatalf("docs readiness = ready:%v waiting:%v", docsTask.Ready, docsTask.Waiting)
	}
	schemaTask, _ := snapshot.Task("SCHEMA")
	if len(schemaTask.Messages) != 1 || schemaTask.Messages[0].Text != "Migrated and verified." {
		t.Fatalf("messages = %#v", schemaTask.Messages)
	}
	if len(schemaTask.Results) != 1 || schemaTask.Results[0].Path != "db/migration.sql" {
		t.Fatalf("results = %#v", schemaTask.Results)
	}
	if _, exists := snapshot.Task("REMOVE"); exists {
		t.Fatal("tombstoned task remains visible")
	}
}

func TestRepositoryUsesBacklogFilename(t *testing.T) {
	root := fixtureRepository(t, "backlog.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(repository.EventPath()) != "backlog.jsonl" {
		t.Fatalf("event path = %q", repository.EventPath())
	}

	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.AllTasks()) != 6 {
		t.Fatalf("task count = %d, want 6", len(snapshot.AllTasks()))
	}
}

func TestSnapshotReturnsIndependentValues(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}

	task, _ := snapshot.Task("TOKENS")
	task.Dependencies[0] = "CHANGED"
	all := snapshot.AllTasks()
	all[0].Title = "changed"

	again, _ := snapshot.Task("TOKENS")
	if again.Dependencies[0] != "SCHEMA" {
		t.Fatalf("snapshot dependency mutated: %v", again.Dependencies)
	}
	if current, _ := snapshot.Task("EPIC01"); current.Title != "Authentication" {
		t.Fatalf("snapshot title mutated: %q", current.Title)
	}
}

func TestRepositoryUsesLegacyFilenameAndEvents(t *testing.T) {
	root := fixtureRepository(t, "events.jsonl", "legacy.jsonl")
	repository, err := Open(filepath.Join(root, ".ergo"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(repository.EventPath()) != "events.jsonl" {
		t.Fatalf("event path = %q", repository.EventPath())
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := snapshot.Task("LEGACY")
	if !legacy.Container {
		t.Fatalf("legacy epic is not a container: %#v", legacy)
	}
	task, _ := snapshot.Task("OLDERR")
	if task.State != StateError || task.ClaimedBy != "legacy@host" {
		t.Fatalf("legacy error task = %#v", task)
	}
	if task.Title != "Legacy decoder" || task.Body != "Keep compatibility." {
		t.Fatalf("legacy title migration = title:%q body:%q", task.Title, task.Body)
	}
	if len(task.Dependencies) != 0 {
		t.Fatalf("unlink was not replayed: %v", task.Dependencies)
	}
}

func TestRepositoryToleratesTruncatedFinalRecord(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	path := filepath.Join(root, ".ergo", "plans.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"new_task","data":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.AllTasks()) != 6 {
		t.Fatalf("task count = %d, want 6", len(snapshot.AllTasks()))
	}
}

func TestRepositoryReportsMalformedCompleteRecord(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	path := filepath.Join(root, ".ergo", "plans.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{broken}\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Load()
	if err == nil || !strings.Contains(err.Error(), "plans.jsonl:15: invalid JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadBoundedLineStopsAtLimit(t *testing.T) {
	const (
		limit      = 128
		bufferSize = 32
	)
	source := &countingFillReader{}
	reader := bufio.NewReaderSize(source, bufferSize)
	if _, err := readBoundedLine(reader, limit); !errors.Is(err, errEventLineTooLong) {
		t.Fatalf("error = %v, want event-line limit", err)
	}
	if source.bytesRead > limit+bufferSize {
		t.Fatalf("reader consumed %d bytes past a %d-byte limit", source.bytesRead, limit)
	}

	terminated := append(bytes.Repeat([]byte("x"), limit), '\n')
	line, err := readBoundedLine(bufio.NewReaderSize(bytes.NewReader(terminated), bufferSize), limit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(line, terminated) {
		t.Fatalf("line length = %d, want %d", len(line), len(terminated))
	}
}

func TestRepositoryLoadIfChangedSkipsUnchangedLog(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}

	_, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unchanged event log reported a reload")
	}
	if repository.state.offset == 0 || repository.state.source.version() != initial.Version {
		t.Fatalf("repository state was not retained: %#v", repository.state)
	}
}

func TestRepositoryLoadIfChangedReplaysAppendedEvents(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}

	appendEvent(t, repository.EventPath(), "claim", map[string]any{
		"id":       "TOKENS",
		"agent_id": "codex@host",
	})
	appendEvent(t, repository.EventPath(), "state", map[string]any{
		"id":    "TOKENS",
		"state": StateDoing,
		"ts":    "2026-07-25T12:00:00Z",
	})
	appendEvent(t, repository.EventPath(), "tombstone", map[string]any{
		"id": "SCHEMA",
	})

	updated, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("appended events did not trigger a reload")
	}
	if updated.Version == initial.Version {
		t.Fatal("snapshot version did not advance")
	}
	task, ok := updated.Task("TOKENS")
	if !ok {
		t.Fatal("updated task is missing")
	}
	if task.State != StateDoing || task.ClaimedBy != "codex@host" {
		t.Fatalf("task = %#v", task)
	}
	if task.Ready || task.Waiting {
		t.Fatalf("derived readiness was not rebuilt: %#v", task)
	}
	if len(task.Dependencies) != 0 {
		t.Fatalf("tombstoned dependency remains: %v", task.Dependencies)
	}
	if _, exists := updated.Task("SCHEMA"); exists {
		t.Fatal("incremental tombstone left the task visible")
	}
	container, _ := updated.Task("EPIC01")
	if strings.Join(container.Children, ",") != "TOKENS" {
		t.Fatalf("container children = %v, want TOKENS", container.Children)
	}
}

func TestRepositoryLoadIfChangedRetriesTornFinalRecord(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(); err != nil {
		t.Fatal(err)
	}
	offset := repository.state.offset

	appendText(t, repository.EventPath(), `{"type":"title","data":{"id":"TOKENS","title":"Rotating`)
	partial, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("partial append did not advance the observed source")
	}
	if repository.state.offset != offset {
		t.Fatalf("partial record advanced offset from %d to %d", offset, repository.state.offset)
	}
	task, _ := partial.Task("TOKENS")
	if task.Title == "Rotating tokens" {
		t.Fatal("partial event was applied")
	}

	appendText(t, repository.EventPath(), ` tokens","ts":"2026-07-25T12:00:00Z"}}`+"\n")
	updated, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("completed record did not trigger a reload")
	}
	task, _ = updated.Task("TOKENS")
	if task.Title != "Rotating tokens" {
		t.Fatalf("title = %q, want Rotating tokens", task.Title)
	}
}

func TestRepositoryLoadIfChangedFallsBackAfterUnterminatedValidRecord(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	path := filepath.Join(root, ".ergo", "plans.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.TrimSuffix(data, []byte("\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !repository.state.openLine || len(initial.AllTasks()) != 6 {
		t.Fatalf("unterminated valid record was not retained: %#v", repository.state)
	}

	appendText(t, path, "\n")
	appendEvent(t, path, "title", map[string]any{
		"id":    "TOKENS",
		"title": "Completed line",
		"ts":    "2026-07-25T12:00:00Z",
	})
	updated, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("appended newline and event did not trigger a reload")
	}
	task, _ := updated.Task("TOKENS")
	if task.Title != "Completed line" {
		t.Fatalf("title = %q, want Completed line", task.Title)
	}
}

func TestRepositoryLoadIfChangedFallsBackAfterRewrite(t *testing.T) {
	root := fixtureRepository(t, "plans.jsonl", "current.jsonl")
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(repository.EventPath())
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"title":"Issue tokens"`), []byte(`"title":"Fresh tokens"`), 1)
	if err := os.WriteFile(repository.EventPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(repository.EventPath(), future, future); err != nil {
		t.Fatal(err)
	}

	updated, changed, err := repository.LoadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("rewritten log did not trigger a reload")
	}
	if task, exists := updated.Task("TOKENS"); !exists || task.Title != "Fresh tokens" {
		t.Fatalf("rewritten task = %#v, found = %v", task, exists)
	}
}

func appendEvent(t *testing.T, path, kind string, data map[string]any) {
	t.Helper()
	line, err := json.Marshal(event{Type: kind, TS: "2026-07-25T12:00:00Z", Data: mustJSON(t, data)})
	if err != nil {
		t.Fatal(err)
	}
	appendText(t, path, string(line)+"\n")
}

func appendText(t *testing.T, path, value string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(value); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type countingFillReader struct {
	bytesRead int
}

func (reader *countingFillReader) Read(data []byte) (int, error) {
	for index := range data {
		data[index] = 'x'
	}
	reader.bytesRead += len(data)
	return len(data), nil
}

func fixtureRepository(t *testing.T, destination, fixture string) string {
	t.Helper()
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(filepath.Join(root, "nested", "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ergoDir, destination), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
