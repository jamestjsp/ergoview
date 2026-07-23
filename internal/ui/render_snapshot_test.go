package ui

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func TestRenderSnapshots(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		view     viewMode
		selected string
		light    bool
		noColor  bool
	}{
		{name: "overview-narrow-dark", width: 72, height: 24, view: viewOverview, selected: "POLISH"},
		{name: "overview-wide-light", width: 140, height: 32, view: viewOverview, selected: "POLISH", light: true},
		{name: "board-standard-dark", width: 110, height: 30, view: viewBoard, selected: "WINDOWS"},
		{name: "dependencies-wide-no-color", width: 140, height: 32, view: viewDependencies, selected: "POLISH", noColor: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, test.noColor)
			model.rebuildRows(test.selected)
			model.setView(test.view)
			if test.light {
				updated, _ := model.Update(tea.BackgroundColorMsg{Color: color.White})
				model = updated.(Model)
			}
			model = resize(t, model, test.width, test.height)
			model.syncDetail()
			content := model.View().Content
			assertFits(t, content, test.width, test.height)
			assertGolden(t, filepath.Join("testdata", test.name+".golden"), plainSnapshot(content))
		})
	}
}

func TestDocumentationScreenshots(t *testing.T) {
	tests := []struct {
		name     string
		view     viewMode
		selected string
	}{
		{name: "overview", view: viewOverview, selected: "POLISH"},
		{name: "board", view: viewBoard, selected: "WINDOWS"},
		{name: "dependencies", view: viewDependencies, selected: "POLISH"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, false)
			model.rebuildRows(test.selected)
			model.setView(test.view)
			model = resize(t, model, 140, 32)
			model.syncDetail()
			svg := terminalSVG(ansi.Strip(model.View().Content), "Ergo View — "+test.name)
			assertGolden(t, filepath.Join("..", "..", "docs", "img", test.name+".svg"), svg)
		})
	}
}

func renderFixtureModel(t *testing.T, noColor bool) Model {
	t.Helper()
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "")
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "render.jsonl"))
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
	snapshot.Root = "/workspace/ergoview-demo"
	model := New(snapshot, Options{NoColor: noColor})
	t.Setenv("NO_COLOR", "1")
	return model
}

func plainSnapshot(content string) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(content), "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func terminalSVG(content, title string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	const (
		charWidth  = 8
		lineHeight = 17
		padding    = 24
		titleBar   = 42
	)
	maxWidth := 0
	for _, line := range lines {
		maxWidth = max(maxWidth, len([]rune(line)))
	}
	width := maxWidth*charWidth + padding*2
	height := len(lines)*lineHeight + padding*2 + titleBar
	var body strings.Builder
	for index, line := range lines {
		y := titleBar + padding + (index+1)*lineHeight
		fill := "#e2e8f0"
		if index < 2 {
			fill = "#c4b5fd"
		}
		fmt.Fprintf(
			&body,
			`  <text x="%d" y="%d" fill="%s">%s</text>`+"\n",
			padding,
			y,
			fill,
			html.EscapeString(line),
		)
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="100%%" height="100%%" rx="14" fill="#0b1020"/>
  <path d="M14 0h%dq14 0 14 14v28H0V14Q0 0 14 0" fill="#171d30"/>
  <circle cx="22" cy="21" r="5" fill="#fb7185"/>
  <circle cx="40" cy="21" r="5" fill="#fbbf24"/>
  <circle cx="58" cy="21" r="5" fill="#34d399"/>
  <text x="50%%" y="26" text-anchor="middle" fill="#94a3b8" font-family="ui-monospace, SFMono-Regular, Consolas, monospace" font-size="13">%s</text>
  <g font-family="ui-monospace, SFMono-Regular, Consolas, monospace" font-size="13" xml:space="preserve">
%s  </g>
</svg>
`, width, height, width, height, width-14, html.EscapeString(title), body.String())
}

func assertGolden(t *testing.T, path, actual string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if actual != string(expected) {
		t.Fatalf("render differs from %s; run UPDATE_GOLDEN=1 go test ./internal/ui", path)
	}
}
