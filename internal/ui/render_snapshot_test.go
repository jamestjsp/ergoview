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
		{name: "overview-narrow-dark", width: 72, height: 24, view: viewOverview, selected: "DEDIT1"},
		{name: "overview-wide-light", width: 140, height: 32, view: viewOverview, selected: "DEDIT1", light: true},
		{name: "board-standard-dark", width: 110, height: 30, view: viewBoard, selected: "RTHEME"},
		{name: "dependencies-narrow-dark", width: 72, height: 24, view: viewDependencies, selected: "DEDIT1"},
		{name: "dependencies-standard-light", width: 100, height: 30, view: viewDependencies, selected: "DEDIT1", light: true},
		{name: "dependencies-wide-no-color", width: 140, height: 32, view: viewDependencies, selected: "DEDIT1", noColor: true},
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
		title    string
		view     viewMode
		selected string
	}{
		{name: "overview", title: "Overview", view: viewOverview, selected: "DEDIT1"},
		{name: "board", title: "Board", view: viewBoard, selected: "RTHEME"},
		{name: "dependencies", title: "Dependencies", view: viewDependencies, selected: "DEDIT1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, false)
			model.rebuildRows(test.selected)
			model.setView(test.view)
			model = resize(t, model, 124, 30)
			model.syncDetail()
			svg := terminalSVG(ansi.Strip(model.View().Content), "Ergo View  ·  "+test.title, test.title)
			assertGolden(t, filepath.Join("..", "..", "docs", "img", test.name+".svg"), svg)
		})
	}
}

func TestDependencyGraphUsesAvailableTerminalRealEstate(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 70, height: 24},
		{width: 100, height: 30},
		{width: 140, height: 36},
	} {
		model := renderFixtureModel(t, true)
		model.rebuildRows("DEDIT1")
		model.setView(viewDependencies)
		model = resize(t, model, size.width, size.height)
		content := model.View().Content
		plain := ansi.Strip(content)
		if !strings.Contains(plain, "DEPENDENCY FLOW") || !strings.Contains(plain, "DEDIT1") {
			t.Fatalf("%dx%d graph lost its focus:\n%s", size.width, size.height, plain)
		}
		assertFits(t, content, size.width, size.height)
		if size.width == 140 && size.height == 36 {
			for _, expected := range []string{"CORE01 EPIC 1/1", "RTHEME READY", "RSIGN1 BLOCKED", "LAUNCH WAITING"} {
				if !strings.Contains(plain, expected) {
					t.Fatalf("%dx%d graph missing %q:\n%s", size.width, size.height, expected, plain)
				}
			}
		}
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
	snapshot.Root = "/workspace/product-roadmap"
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

func terminalSVG(content, title, activeView string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	const (
		cellWidth  = 8.2
		lineHeight = 18
		paddingX   = 26
		paddingY   = 20
		titleBar   = 46
	)
	maxColumns := 0
	for _, line := range lines {
		maxColumns = max(maxColumns, len([]rune(line)))
	}
	width := float64(maxColumns)*cellWidth + paddingX*2
	height := float64(len(lines)*lineHeight + paddingY*2 + titleBar)
	var body strings.Builder
	for index, line := range lines {
		y := float64(titleBar+paddingY+(index+1)*lineHeight) - 3
		for _, run := range svgTextRuns(line) {
			fill, weight, opacity := svgRunStyle(run.text, index, activeView)
			fmt.Fprintf(
				&body,
				`    <text x="%.1f" y="%.1f" fill="%s" fill-opacity="%.2f" font-weight="%s" textLength="%.1f" lengthAdjust="spacingAndGlyphs">%s</text>`+"\n",
				float64(paddingX)+float64(run.column)*cellWidth,
				y,
				fill,
				opacity,
				weight,
				float64(len([]rune(run.text)))*cellWidth,
				html.EscapeString(run.text),
			)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">
  <defs>
    <filter id="shadow" x="-10%%" y="-10%%" width="120%%" height="130%%">
      <feDropShadow dx="0" dy="8" stdDeviation="10" flood-color="#020617" flood-opacity=".45"/>
    </filter>
    <clipPath id="window"><rect x="1" y="1" width="%.0f" height="%.0f" rx="15"/></clipPath>
  </defs>
  <rect x="1" y="1" width="%.0f" height="%.0f" rx="15" fill="#0a101c" stroke="#27344d" stroke-width="2" filter="url(#shadow)"/>
  <g clip-path="url(#window)">
    <rect x="1" y="1" width="%.0f" height="%d" fill="#151d30"/>
    <path d="M1 %dH%.0f" stroke="#2a3650"/>
    <circle cx="24" cy="23" r="5.5" fill="#fb7185"/>
    <circle cx="43" cy="23" r="5.5" fill="#fbbf24"/>
    <circle cx="62" cy="23" r="5.5" fill="#34d399"/>
    <text x="50%%" y="28" text-anchor="middle" fill="#9aa8bd" font-family="Menlo, SFMono-Regular, Consolas, monospace" font-size="13">%s</text>
    <g font-family="Menlo, SFMono-Regular, Consolas, monospace" font-size="13.5" xml:space="preserve">
%s    </g>
  </g>
</svg>
`, width, height, width, height, width-2, height-2, width-2, height-2, width-2, titleBar, titleBar, width-1, html.EscapeString(title), body.String())
}

type svgTextRun struct {
	column int
	text   string
}

func svgTextRuns(line string) []svgTextRun {
	runes := []rune(line)
	var runs []svgTextRun
	for column := 0; column < len(runes); {
		if runes[column] == ' ' {
			column++
			continue
		}
		start := column
		for column < len(runes) && runes[column] != ' ' {
			column++
		}
		runs = append(runs, svgTextRun{column: start, text: string(runes[start:column])})
	}
	return runs
}

func svgRunStyle(text string, line int, activeView string) (fill, weight string, opacity float64) {
	token := strings.Trim(text, "│┃")
	fill, weight, opacity = "#dbe4f0", "400", 1
	if isBoxDrawing(text) {
		return "#3b4964", "400", 1
	}
	switch token {
	case "ERGO", "VIEW":
		return "#c4b5fd", "700", 1
	case "WORK", "DETAIL", "DEPENDENCY", "FLOW", "PREREQUISITES", "SELECTED", "UNLOCKS", "Description", "Depends", "Progress":
		return "#a78bfa", "700", 1
	case "READY":
		return "#fbbf24", "700", 1
	case "DOING":
		return "#22d3ee", "700", 1
	case "BLOCKED", "ERROR":
		return "#fb7185", "700", 1
	case "DONE":
		return "#34d399", "600", 1
	case "WAITING":
		return "#94a3b8", "500", 1
	case "CANCELED":
		return "#64748b", "500", .8
	case "○":
		return "#fbbf24", "700", 1
	case "◐":
		return "#22d3ee", "700", 1
	case "!", "⚠":
		return "#fb7185", "700", 1
	case "✓":
		return "#34d399", "700", 1
	case "×":
		return "#64748b", "600", .8
	case "◇", "◆", "•":
		return "#a78bfa", "700", 1
	case "2/4", "0/3", "1/2":
		return "#a78bfa", "600", 1
	}
	if line == 0 {
		switch token {
		case activeView:
			return "#c4b5fd", "700", 1
		case "Overview", "Board", "Dependencies":
			return "#8492a8", "500", .9
		}
	}
	if line == 1 {
		switch token {
		case "ready":
			return "#fbbf24", "600", 1
		case "doing":
			return "#22d3ee", "600", 1
		case "blocked":
			return "#fb7185", "600", 1
		case "waiting":
			return "#94a3b8", "500", 1
		}
	}
	if strings.HasPrefix(token, "@") {
		return "#22d3ee", "600", 1
	}
	if isFixtureID(token) || strings.Contains(token, "product-roadmap") {
		return "#8492a8", "400", .9
	}
	if isFooterKey(token) {
		return "#f1f5f9", "600", 1
	}
	return fill, weight, opacity
}

func isBoxDrawing(text string) bool {
	for _, character := range text {
		if !strings.ContainsRune("╭─╮│╰╯┌┐└┘├┤┬┴┼┃┏┓┗┛━┄┆▶▼", character) {
			return false
		}
	}
	return text != ""
}

func isFixtureID(token string) bool {
	switch token {
	case "DESK01", "DLOAD1", "DVIEW1", "DEDIT1", "DWIN01", "CORE01", "CORECH", "REL001", "RDOCS1", "RTHEME", "RSIGN1", "TEAM01", "TDEMO1", "TREVW1", "LAUNCH":
		return true
	default:
		return false
	}
}

func isFooterKey(token string) bool {
	switch token {
	case "j/k", "h/j/k/l", "tab", "enter", "esc", "d", "a", "n/p", "n", "1/2/3", "/", "f", "e", "x", "?", "q":
		return true
	default:
		return false
	}
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
