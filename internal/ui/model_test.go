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
