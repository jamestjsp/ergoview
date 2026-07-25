package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"reflect"
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

func TestCopySelectedIDWithKeyboard(t *testing.T) {
	for _, id := range []string{"TOKENS", "EPIC01"} {
		t.Run(id, func(t *testing.T) {
			model := resize(t, testModel(t), 120, 28)
			model.rebuildRows(id)

			updated, command := model.Update(key("c"))
			model = updated.(Model)

			if got := clipboardCommandText(t, command); got != id {
				t.Fatalf("clipboard content = %q, want %s", got, id)
			}
			wantStatus := "Copied " + id + " to clipboard"
			if model.status != wantStatus {
				t.Fatalf("status = %q, want %q", model.status, wantStatus)
			}
		})
	}
}

func TestCopySelectedIDFooterWorksAcrossViewsAndWidths(t *testing.T) {
	for _, test := range []struct {
		name   string
		view   viewMode
		width  int
		height int
	}{
		{name: "narrow overview", view: viewOverview, width: 72, height: 24},
		{name: "board", view: viewBoard, width: 120, height: 30},
		{name: "dependencies", view: viewDependencies, width: 120, height: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := testModel(t)
			model.rebuildRows("TOKENS")
			model.setView(test.view)
			model = resize(t, model, test.width, test.height)

			updated, command := model.Update(tea.MouseClickMsg{
				X:      copyControlX(t, model),
				Y:      model.height - 1,
				Button: tea.MouseLeft,
			})
			model = updated.(Model)

			if got := clipboardCommandText(t, command); got != "TOKENS" {
				t.Fatalf("clipboard content = %q, want TOKENS", got)
			}
			if model.status != "Copied TOKENS to clipboard" {
				t.Fatalf("status = %q", model.status)
			}
		})
	}
}

func TestCopyControlIsUnavailableWithoutSelection(t *testing.T) {
	model := New(ergo.Snapshot{Root: t.TempDir()}, Options{NoColor: true})
	model = resize(t, model, 72, 24)

	if strings.Contains(ansi.Strip(model.View().Content), "copy ID") {
		t.Fatal("empty snapshot rendered a copy control")
	}
	updated, command := model.Update(key("c"))
	model = updated.(Model)
	if command != nil {
		t.Fatal("empty snapshot produced a clipboard command")
	}
	if model.status != "" {
		t.Fatalf("empty snapshot status = %q", model.status)
	}
}

func TestCopyShortcutDoesNotInterceptSearchInput(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	updated, _ := model.Update(key("/"))
	model = updated.(Model)

	updated, _ = model.Update(key("c"))
	model = updated.(Model)

	if got := model.search.Value(); got != "c" {
		t.Fatalf("search value = %q, want c", got)
	}
	if strings.Contains(model.status, "Copied") {
		t.Fatalf("copy status set while searching: %q", model.status)
	}
}

func TestCopyFooterDoesNotBypassActionMenu(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("TOKENS")
	copyX := copyControlX(t, model)
	model.actionMenu = true

	updated, command := model.Update(tea.MouseClickMsg{
		X:      copyX,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	if command != nil {
		t.Fatal("action menu footer click produced a clipboard command")
	}
	if strings.Contains(model.status, "Copied") {
		t.Fatalf("copy status set behind action menu: %q", model.status)
	}
}

func TestFooterRowClaimsEveryMouseClick(t *testing.T) {
	for _, test := range []struct {
		name   string
		view   viewMode
		width  int
		height int
	}{
		{name: "narrow overview", view: viewOverview, width: 72, height: 12},
		{name: "wide overview", view: viewOverview, width: 120, height: 28},
		{name: "board", view: viewBoard, width: 120, height: 28},
		{name: "dependencies", view: viewDependencies, width: 120, height: 28},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, true)
			model.rebuildRows("DEDIT1")
			model.setView(test.view)
			model = resize(t, model, test.width, test.height)
			model.status = "stale"
			selected, focused := model.selected, model.focus

			updated, command := model.Update(tea.MouseClickMsg{
				X:      model.width - 2,
				Y:      model.height - 1,
				Button: tea.MouseLeft,
			})
			model = updated.(Model)

			if command != nil {
				t.Fatal("footer miss produced a command")
			}
			if model.selected != selected {
				t.Fatalf("footer miss changed selection from %d to %d", selected, model.selected)
			}
			if model.focus != focused {
				t.Fatalf("footer miss changed focus from %d to %d", focused, model.focus)
			}
			if model.status != "" {
				t.Fatalf("footer miss left status %q", model.status)
			}
		})
	}
}

func TestCopyFooterHitBoundsAreHalfOpen(t *testing.T) {
	newModel := func() Model {
		model := resize(t, testModel(t), 120, 28)
		model.rebuildRows("TOKENS")
		return model
	}
	placement := footerPlacementForAction(t, newModel(), footerActionCopyID)

	model := newModel()
	updated, command := model.Update(tea.MouseClickMsg{
		X:      placement.start,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if got := clipboardCommandText(t, command); got != "TOKENS" {
		t.Fatalf("start-bound clipboard content = %q, want TOKENS", got)
	}

	model = newModel()
	model.status = "stale"
	updated, command = model.Update(tea.MouseClickMsg{
		X:      placement.end,
		Y:      model.height - 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command != nil {
		t.Fatal("end-bound click produced a clipboard command")
	}
	if model.status != "" {
		t.Fatalf("end-bound click left status %q", model.status)
	}
}

func TestMouseClickClearsStatusAboveFooter(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.status = "stale"

	updated, _ := model.Update(tea.MouseClickMsg{
		X:      4,
		Y:      4,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	if model.status != "" {
		t.Fatalf("mouse click left status %q", model.status)
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
	updated, _ := model.Update(snapshotLoadedMsg{snapshot: updatedSnapshot, changed: true})
	model = updated.(Model)
	if selected := model.selectedID(); selected != "TOKENS" {
		t.Fatalf("selected = %q, want TOKENS", selected)
	}
	if model.status == "" {
		t.Fatal("reload status was not set")
	}
}

func TestLiveReloadSkipsUnchangedSnapshot(t *testing.T) {
	snapshot := testSnapshot(t)
	source := &stubSnapshotSource{snapshot: snapshot}
	message := loadSnapshot(source)().(snapshotLoadedMsg)
	if source.calls != 1 {
		t.Fatalf("conditional loads = %d, want 1", source.calls)
	}
	if message.changed {
		t.Fatal("unchanged source reported a changed snapshot")
	}

	model := New(snapshot, Options{Source: source})
	model.status = "keep"
	updated, _ := model.Update(message)
	model = updated.(Model)
	if model.status != "keep" {
		t.Fatalf("unchanged reload replaced status with %q", model.status)
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

func TestTerminalBackgroundSelectsThemePalette(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	model := testModel(t)
	updated, _ := model.Update(tea.BackgroundColorMsg{Color: color.White})
	model = updated.(Model)
	if model.dark {
		t.Fatal("white terminal background did not select light theme")
	}
}

type stubSnapshotSource struct {
	snapshot ergo.Snapshot
	changed  bool
	err      error
	calls    int
}

func (source *stubSnapshotSource) LoadIfChanged() (ergo.Snapshot, bool, error) {
	source.calls++
	return source.snapshot, source.changed, source.err
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

func clipboardCommandText(t *testing.T, command tea.Cmd) string {
	t.Helper()
	if command == nil {
		t.Fatal("clipboard command is nil")
	}
	message := command()
	value := reflect.ValueOf(message)
	if value.Kind() != reflect.String {
		t.Fatalf("clipboard command message type = %T", message)
	}
	return value.String()
}

func copyControlX(t *testing.T, model Model) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	footer := lines[len(lines)-1]
	index := strings.Index(footer, "c copy ID")
	if index < 0 {
		t.Fatalf("copy control not visible in footer:\n%s", ansi.Strip(model.View().Content))
	}
	return lipgloss.Width(footer[:index])
}

func footerPlacementForAction(t *testing.T, model Model, action footerAction) footerPlacement {
	t.Helper()
	for _, placement := range model.footerPlacements(model.footerItems()) {
		if placement.item.action == action {
			return placement
		}
	}
	t.Fatalf("footer action %d not visible:\n%s", action, ansi.Strip(model.View().Content))
	return footerPlacement{}
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
