package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFooterEndsOnCompleteItems(t *testing.T) {
	labels := map[string]bool{
		"move": true, "scroll": true, "views": true, "actions": true,
		"new": true, "search": true, "filter": true, "epic": true,
		"clear": true, "help": true, "quit": true, "pane": true,
		"detail": true, "node": true, "focus": true, "back": true,
		"depth": true, "ID": true,
	}
	for _, size := range []struct{ width, height int }{
		{40, 20}, {60, 20}, {72, 24}, {90, 24}, {140, 32},
	} {
		model := renderFixtureModel(t, true)
		model.rebuildRows("DEDIT1")
		model = resize(t, model, size.width, size.height)
		lines := strings.Split(strings.TrimRight(ansi.Strip(model.View().Content), "\n"), "\n")
		footer := strings.TrimSpace(lines[len(lines)-1])
		if footer == "" {
			continue
		}
		items := strings.Split(footer, "  ·  ")
		fields := strings.Fields(items[len(items)-1])
		if len(fields) < 2 || !labels[fields[len(fields)-1]] {
			t.Fatalf("width %d footer ends mid-item: %q", size.width, footer)
		}
	}
}

func TestStatusClearsOnNextKeyPress(t *testing.T) {
	model := renderFixtureModel(t, true)
	model.rebuildRows("DEDIT1")
	model.setView(viewDependencies)
	model = resize(t, model, 100, 30)

	updated, _ := model.Update(key("d"))
	model = updated.(Model)
	if model.status == "" {
		t.Fatal("depth cycle should set a status message")
	}
	updated, _ = model.Update(key("j"))
	model = updated.(Model)
	if model.status != "" {
		t.Fatalf("status should clear on the next key press, got %q", model.status)
	}
}

func TestCopyFooterPlacementPreservesPriorityHints(t *testing.T) {
	for _, test := range []struct {
		name     string
		view     viewMode
		width    int
		height   int
		expected string
	}{
		{name: "narrow overview search", view: viewOverview, width: 72, height: 24, expected: "/ search"},
		{name: "standard dependencies search", view: viewDependencies, width: 100, height: 30, expected: "/ search"},
		{name: "wide overview clear", view: viewOverview, width: 140, height: 32, expected: "x clear"},
		{name: "wide board clear", view: viewBoard, width: 140, height: 32, expected: "x clear"},
		{name: "narrow dependencies actions", view: viewDependencies, width: 72, height: 24, expected: "a actions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, true)
			model.rebuildRows("DEDIT1")
			model.setView(test.view)
			model = resize(t, model, test.width, test.height)
			footer := footerText(model)

			items := model.footerItems()
			actionIndex := -1
			for index, item := range items {
				if strings.Contains(ansi.Strip(item.content), "a actions") {
					actionIndex = index
					break
				}
			}
			if actionIndex < 0 || actionIndex+1 >= len(items) ||
				items[actionIndex+1].action != footerActionCopy {
				t.Fatalf("copy control is not ordered immediately after actions: %#v", items)
			}
			if !strings.Contains(footer, test.expected) {
				t.Fatalf("footer missing %q: %q", test.expected, footer)
			}
		})
	}
}

func TestCompactFooterItemsDoesNotAliasInput(t *testing.T) {
	items := []footerItem{{content: "first"}, {}, {content: "second"}}
	compacted := compactFooterItems(items)

	compacted[0].content = "changed"
	if items[0].content != "first" {
		t.Fatalf("compactFooterItems mutated input: %#v", items)
	}
}

func TestFooterPlacementIncludesStyleFrames(t *testing.T) {
	model := renderFixtureModel(t, true)
	model.width = 100
	model.styles.app = model.styles.app.MarginLeft(2).BorderLeft(true).PaddingLeft(3)
	model.styles.footer = model.styles.footer.MarginLeft(4).BorderLeft(true).PaddingLeft(5)

	placements := model.footerPlacements([]footerItem{{content: "copy"}})
	want := model.styles.app.GetMarginLeft() +
		model.styles.app.GetBorderLeftSize() +
		model.styles.app.GetPaddingLeft() +
		model.styles.footer.GetMarginLeft() +
		model.styles.footer.GetBorderLeftSize() +
		model.styles.footer.GetPaddingLeft()
	if len(placements) != 1 || placements[0].start != want {
		t.Fatalf("footer placement = %#v, want start %d", placements, want)
	}
}

func TestCopyControlIsHiddenDuringModalInput(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*Model)
	}{
		{name: "search", setup: func(model *Model) { model.searching = true }},
		{name: "action menu", setup: func(model *Model) { model.actionMenu = true }},
		{name: "dialog", setup: func(model *Model) { model.openDialog(actionRename) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := renderFixtureModel(t, true)
			model.rebuildRows("DEDIT1")
			model = resize(t, model, 120, 28)
			test.setup(&model)

			if footer := footerText(model); strings.Contains(footer, "copy ID") {
				t.Fatalf("modal footer exposes copy control: %q", footer)
			}
		})
	}
}

func TestEveryViewOverlayFitsViewport(t *testing.T) {
	type overlaySpec struct {
		name  string
		setup func(*Model)
	}
	overlays := []overlaySpec{
		{name: "none"},
		{name: "help", setup: func(model *Model) {
			model.help = true
			model.syncHelpView(true)
		}},
		{name: "action menu", setup: func(model *Model) {
			model.actionMenu = true
		}},
	}
	for _, kind := range []actionKind{
		actionNewTask,
		actionNewPlan,
		actionClaim,
		actionDone,
		actionBlock,
		actionCancel,
		actionRelease,
		actionRename,
		actionBody,
		actionMove,
		actionSequence,
		actionUnsequence,
	} {
		kind := kind
		overlays = append(overlays, overlaySpec{
			name: "dialog " + string(kind),
			setup: func(model *Model) {
				model.openDialog(kind)
			},
		})
	}

	views := []struct {
		name string
		view viewMode
	}{
		{name: "overview", view: viewOverview},
		{name: "board", view: viewBoard},
		{name: "dependencies", view: viewDependencies},
	}
	focuses := []struct {
		name  string
		focus focus
	}{
		{name: "outline", focus: focusOutline},
		{name: "detail", focus: focusDetail},
	}
	sizes := []struct{ width, height int }{
		{width: 40, height: 12},
		{width: 72, height: 20},
		{width: 80, height: 24},
		{width: 100, height: 25},
		{width: 120, height: 29},
		{width: 120, height: 30},
		{width: 180, height: 50},
	}

	for _, view := range views {
		for _, overlay := range overlays {
			for _, focused := range focuses {
				for _, size := range sizes {
					name := fmt.Sprintf(
						"%s/%s/%s/%dx%d",
						view.name,
						overlay.name,
						focused.name,
						size.width,
						size.height,
					)
					t.Run(name, func(t *testing.T) {
						model := renderFixtureModel(t, true)
						model.rebuildRows("DEDIT1")
						model.setView(view.view)
						model.focus = focused.focus
						model = resize(t, model, size.width, size.height)
						if overlay.setup != nil {
							overlay.setup(&model)
						}
						assertRenderedSize(t, model.View().Content, size.width, size.height)
					})
				}
			}
		}
	}
}

func assertRenderedSize(t *testing.T, content string, width, height int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		t.Fatalf("rendered height = %d, allowed = %d", len(lines), height)
	}
	for index, line := range lines {
		if actual := ansi.StringWidth(line); actual > width {
			t.Fatalf("line %d width = %d, allowed = %d: %q", index+1, actual, width, ansi.Strip(line))
		}
	}
}

func footerText(model Model) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(model.View().Content), "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
