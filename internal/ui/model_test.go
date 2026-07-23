package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func TestWideLayoutShowsOutlineAndMarkdownDetail(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model = resize(t, model, 140, 32)
	model.syncDetail()

	content := model.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "Issue tokens") {
		t.Fatalf("outline missing selected task:\n%s", plain)
	}
	if !strings.Contains(plain, "Use rotating refresh tokens.") {
		t.Fatalf("detail missing Markdown body:\n%s", plain)
	}
	assertFits(t, content, 140, 32)
}

func TestNarrowLayoutSwitchesBetweenOutlineAndDetail(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model = resize(t, model, 72, 24)
	model.syncDetail()

	outline := ansi.Strip(model.View().Content)
	if !strings.Contains(outline, "WORK") || strings.Contains(outline, "Use rotating refresh tokens.") {
		t.Fatalf("unexpected narrow outline:\n%s", outline)
	}
	updated, _ := model.Update(key("enter"))
	model = updated.(Model)
	detail := ansi.Strip(model.View().Content)
	if !strings.Contains(detail, "DETAIL") || !strings.Contains(detail, "Use rotating refresh tokens.") {
		t.Fatalf("unexpected narrow detail:\n%s", detail)
	}
	assertFits(t, model.View().Content, 72, 24)
}

func TestKeyboardNavigationAndHelp(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	initial := model.selected
	updated, _ := model.Update(key("j"))
	model = updated.(Model)
	if model.selected != initial+1 {
		t.Fatalf("selection = %d, want %d", model.selected, initial+1)
	}
	updated, _ = model.Update(key("?"))
	model = updated.(Model)
	if !strings.Contains(ansi.Strip(model.View().Content), "Ergo View keys") {
		t.Fatal("help overlay did not open")
	}
	updated, _ = model.Update(key("esc"))
	model = updated.(Model)
	if model.help {
		t.Fatal("help overlay did not close")
	}
}

func TestBoardKeepsWaitingSeparateFromBlocked(t *testing.T) {
	model := resize(t, testModel(t), 140, 32)
	model.setView(viewBoard)
	groups := model.boardGroups()
	if len(groups[1].tasks) != 1 || groups[1].tasks[0].ID != "DOCS01" {
		t.Fatalf("waiting group = %#v", groups[1].tasks)
	}
	if len(groups[3].tasks) != 1 || groups[3].tasks[0].ID != "BLOCKD" {
		t.Fatalf("blocked group = %#v", groups[3].tasks)
	}
	content := model.View().Content
	plain := ansi.Strip(content)
	for _, heading := range []string{"READY", "WAITING", "DOING", "BLOCKED", "DONE", "CANCELED"} {
		if !strings.Contains(plain, heading) {
			t.Fatalf("board missing %s:\n%s", heading, plain)
		}
	}
	assertFits(t, content, 140, 32)
}

func TestSearchMatchesBodyAndPreservesSelection(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	updated, _ := model.Update(key("/"))
	model = updated.(Model)
	for _, character := range "rotating" {
		updated, _ = model.Update(key(string(character)))
		model = updated.(Model)
	}
	if selected := model.selectedID(); selected != "TOKENS" {
		t.Fatalf("selected = %q, want TOKENS", selected)
	}
	if len(model.rows) != 2 || model.rows[0].id != "EPIC01" || model.rows[1].id != "TOKENS" {
		t.Fatalf("filtered rows = %#v", model.rows)
	}
}

func TestStateAndContainerFiltersCompose(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	model.filter = filterReady
	model.containerFilter = "EPIC01"
	model.rebuildRows("TOKENS")
	if len(model.rows) != 2 || model.rows[0].id != "EPIC01" || model.rows[1].id != "TOKENS" {
		t.Fatalf("filtered rows = %#v", model.rows)
	}
	model.clearFilters()
	if model.filter != filterAll || model.containerFilter != "" || len(model.rows) != 6 {
		t.Fatalf("clear filters left filter=%v container=%q rows=%d", model.filter, model.containerFilter, len(model.rows))
	}
}

func TestViewsShareSelectionAndFitResponsiveLayouts(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 72, height: 24},
		{width: 140, height: 32},
	} {
		model := resize(t, testModel(t), size.width, size.height)
		model.rebuildRows("TOKENS")
		for _, view := range []viewMode{viewBoard, viewDependencies, viewOverview} {
			model.setView(view)
			if selected := model.selectedID(); selected != "TOKENS" {
				t.Fatalf("%dx%d view %d selected = %q", size.width, size.height, view, selected)
			}
			assertFits(t, model.View().Content, size.width, size.height)
		}
	}
}

func TestLiveReloadPreservesExistingSelection(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("TOKENS")
	updatedSnapshot := testSnapshot(t)
	updatedSnapshot.Version = "external-change"
	updated, _ := model.Update(snapshotLoadedMsg{snapshot: updatedSnapshot})
	model = updated.(Model)
	if selected := model.selectedID(); selected != "TOKENS" {
		t.Fatalf("selected = %q, want TOKENS", selected)
	}
	if model.status == "" {
		t.Fatal("reload status was not set")
	}
}

func TestFuzzyMatchSupportsUnicode(t *testing.T) {
	task := ergo.Task{Title: "Crème brûlée"}
	if !matchesQuery(task, "cbr") || !matchesQuery(task, "brû") {
		t.Fatal("unicode fuzzy match failed")
	}
}

func TestNoColorRemovesANSIColorSequences(t *testing.T) {
	snapshot := testSnapshot(t)
	model := New(snapshot, Options{NoColor: true})
	model = resize(t, model, 120, 28)
	content := model.View().Content
	if strings.Contains(content, "\x1b[38;") || strings.Contains(content, "\x1b[48;") {
		t.Fatalf("NO_COLOR view contains ANSI color sequences:\n%q", content)
	}
}

func testModel(t *testing.T) Model {
	t.Helper()
	return New(testSnapshot(t), Options{})
}

func testSnapshot(t *testing.T) ergo.Snapshot {
	t.Helper()
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "ergo", "testdata", "current.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ergoDir, "plans.jsonl"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := ergo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func resize(t *testing.T, model Model, width, height int) Model {
	t.Helper()
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func key(value string) tea.KeyPressMsg {
	switch value {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	default:
		return tea.KeyPressMsg(tea.Key{Code: []rune(value)[0], Text: value})
	}
}

func assertFits(t *testing.T, content string, width, height int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		t.Fatalf("rendered height = %d, want <= %d:\n%s", len(lines), height, ansi.Strip(content))
	}
	for index, line := range lines {
		if lineWidth := lipgloss.Width(line); lineWidth > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", index+1, lineWidth, width, ansi.Strip(line))
		}
	}
}
