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
