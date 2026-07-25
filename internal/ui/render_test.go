package ui

import (
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
				items[actionIndex+1].action != footerActionCopyID {
				t.Fatalf("copy control is not ordered immediately after actions: %#v", items)
			}
			if !strings.Contains(footer, test.expected) {
				t.Fatalf("footer missing %q: %q", test.expected, footer)
			}
		})
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

func footerText(model Model) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(model.View().Content), "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
