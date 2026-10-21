package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func TestTaskDetailMarkdownMirrorsVisibleSections(t *testing.T) {
	model := renderFixtureModel(t, true)
	task, ok := model.snapshot.Task("DEDIT1")
	if !ok {
		t.Fatal("DEDIT1 not found")
	}
	task.Container = true
	task.Children = []string{"DLOAD1", "DVIEW1", "DWIN01"}
	task.Messages = []ergo.Message{{Kind: "done", Text: "Verified the workflow."}}
	task.Results = []ergo.Result{{Summary: "Validation report", Path: "reports/validation.md"}}
	task.Body += "\n\n" + strings.Repeat("unwrapped ", 30)

	rendered := ansi.Strip(model.renderTaskDetail(task, 48))
	markdown := model.taskDetailMarkdown(task)

	for _, section := range []string{
		"Depends on",
		"Unlocks",
		"Progress",
		"Description",
		"Activity",
		"Results",
	} {
		if !strings.Contains(rendered, section) {
			t.Fatalf("detail fixture does not render %q:\n%s", section, rendered)
		}
		if !strings.Contains(markdown, "## "+section) {
			t.Fatalf("markdown does not mirror %q:\n%s", section, markdown)
		}
	}
	for _, value := range []string{
		"**IN PROGRESS**  `DEDIT1`",
		"# Add task editing workflows",
		"claimed by codex@workstation",
		"container  Desktop experience  DESK01",
		"Load Ergo repositories  DLOAD1",
		"Write install and keyboard guide  RDOCS1",
		"2 of 3 children complete",
		"- **DONE**  Verified the workflow.",
		"- ↗ Validation report  reports/validation.md",
	} {
		if !strings.Contains(markdown, value) {
			t.Fatalf("markdown missing %q:\n%s", value, markdown)
		}
	}
	if ansi.Strip(markdown) != markdown {
		t.Fatalf("markdown contains ANSI escapes: %q", markdown)
	}
	if strings.ContainsAny(markdown, "▰▱") {
		t.Fatalf("markdown contains progress bar glyphs:\n%s", markdown)
	}
	if !strings.Contains(markdown, "## Description\n\n"+task.Body+"\n\n## Activity") {
		t.Fatalf("description body was not embedded verbatim:\n%s", markdown)
	}
}

func TestTaskDetailMarkdownOmitsEmptySections(t *testing.T) {
	model := renderFixtureModel(t, true)
	task := ergo.Task{ID: "EMPTY", Title: "Empty task", State: ergo.StateCanceled}

	markdown := model.taskDetailMarkdown(task)

	for _, absent := range []string{
		"claimed by",
		"container  ",
		"## Depends on",
		"## Unlocks",
		"## Progress",
		"## Description",
		"## Activity",
		"## Results",
	} {
		if strings.Contains(markdown, absent) {
			t.Fatalf("markdown contains omitted section %q:\n%s", absent, markdown)
		}
	}
	for _, present := range []string{"**CANCELED**", "`EMPTY`", "# Empty task"} {
		if !strings.Contains(markdown, present) {
			t.Fatalf("markdown missing %q:\n%s", present, markdown)
		}
	}
}

func TestTaskDetailShowsTimestampsAndLinkedTaskStatus(t *testing.T) {
	model := renderFixtureModel(t, true)
	task, ok := model.snapshot.Task("DEDIT1")
	if !ok {
		t.Fatal("DEDIT1 not found")
	}
	task.UpdatedAt = time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	task.Messages = []ergo.Message{{Kind: "claim", Text: "Picked up.", CreatedAt: time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)}}
	task.Results = []ergo.Result{{Path: "reports/validation.md", CreatedAt: time.Date(2026, 7, 24, 9, 15, 0, 0, time.UTC)}}

	rendered := ansi.Strip(model.renderTaskDetail(task, 80))
	markdown := model.taskDetailMarkdown(task)
	for _, value := range []string{
		"created 2026-07-23 10:03  ·  updated 2026-07-24 09:30",
		"2026-07-23 11:00  Picked up.",
		"reports/validation.md  2026-07-24 09:15",
	} {
		if !strings.Contains(rendered, value) {
			t.Fatalf("detail missing %q:\n%s", value, rendered)
		}
		if !strings.Contains(markdown, value) {
			t.Fatalf("markdown missing %q:\n%s", value, markdown)
		}
	}
	for _, value := range []string{
		"← ✓ DONE  Load Ergo repositories  DLOAD1",
		"→ · WAITING  Write install and keyboard guide  RDOCS1",
	} {
		if !strings.Contains(rendered, value) {
			t.Fatalf("detail missing %q:\n%s", value, rendered)
		}
	}
	for _, value := range []string{
		"- ← DONE  Load Ergo repositories  DLOAD1",
		"- ← DONE  Graph foundations  CORE01",
		"- → WAITING  Write install and keyboard guide  RDOCS1",
	} {
		if !strings.Contains(markdown, value) {
			t.Fatalf("markdown missing %q:\n%s", value, markdown)
		}
	}
}

func TestTaskDetailOmitsZeroTimestamps(t *testing.T) {
	model := renderFixtureModel(t, true)
	task := ergo.Task{
		ID:       "LEGACY",
		Title:    "Legacy task",
		State:    ergo.StateTodo,
		Messages: []ergo.Message{{Kind: "note", Text: "No time."}},
	}
	rendered := ansi.Strip(model.renderTaskDetail(task, 80))
	markdown := model.taskDetailMarkdown(task)
	for _, content := range []string{rendered, markdown} {
		if strings.Contains(content, "created") || strings.Contains(content, "0001-01-01") {
			t.Fatalf("zero timestamp rendered:\n%s", content)
		}
	}
	if !strings.Contains(markdown, "- **NOTE**  No time.") {
		t.Fatalf("message without time misformatted:\n%s", markdown)
	}
}

func TestTaskDetailShowsTimestampsFromLoadedBacklog(t *testing.T) {
	model := testModel(t)
	task, ok := model.snapshot.Task("SCHEMA")
	if !ok {
		t.Fatal("SCHEMA not found")
	}
	markdown := model.taskDetailMarkdown(task)
	for _, value := range []string{
		"updated 2026-07-23 10:06",
		"- **DONE**  2026-07-23 10:06  Migrated and verified.",
		"db/migration.sql  2026-07-23 10:06",
	} {
		if !strings.Contains(markdown, value) {
			t.Fatalf("markdown missing %q:\n%s", value, markdown)
		}
	}
}
