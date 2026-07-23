package ergo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
