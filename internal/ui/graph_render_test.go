package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestGraphCanvasRendersEpicTasksStateAndDirection(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "EPIC", Title: "Launch experience"},
			{ID: "CHILD", Title: "Polish navigation", ParentID: "EPIC"},
			{ID: "BRIDGE", Title: "Integrate graph"},
		},
		[]graphDependencySpec{
			{From: "EPIC", To: "BRIDGE"},
			{From: "BRIDGE", To: "CHILD"},
		},
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("CHILD")
	model.setView(viewDependencies)
	model = resize(t, model, 120, 24)

	content := model.View().Content
	plain := ansi.Strip(content)
	for _, expected := range []string{
		"DEPENDENCY FLOW",
		"left → right",
		"Launch experience",
		"EPIC",
		"Integrate graph",
		"Polish navigation",
		"◇EPIC",
		"▶",
	} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("graph missing %q:\n%s", expected, plain)
		}
	}
	if strings.Contains(content, "\x1b[38;") || strings.Contains(content, "\x1b[48;") {
		t.Fatalf("NO_COLOR graph contains color escapes:\n%q", content)
	}
	assertFits(t, content, 120, 24)
}

func TestGraphCanvasUsesVerticalFlowWhenItFitsBetter(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "A", Title: "Prepare"},
			{ID: "B", Title: "Build"},
			{ID: "C", Title: "Verify"},
		},
		[]graphDependencySpec{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
		},
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("B")
	model.setView(viewDependencies)
	model = resize(t, model, 52, 30)
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "top ↓ bottom") || !strings.Contains(plain, "▼") {
		t.Fatalf("vertical graph not rendered:\n%s", plain)
	}
	assertFits(t, model.View().Content, 52, 30)
}

func TestGraphCanvasPreservesNodesAroundLongEdges(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "A", Title: "Start"},
			{ID: "B", Title: "Middle one"},
			{ID: "C", Title: "Middle two"},
			{ID: "D", Title: "Finish"},
		},
		[]graphDependencySpec{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
			{From: "C", To: "D"},
			{From: "A", To: "D"},
		},
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("D")
	model.setView(viewDependencies)
	model = resize(t, model, 120, 28)
	plain := ansi.Strip(model.View().Content)
	for _, title := range []string{"Start", "Middle one", "Middle two", "Finish"} {
		if !strings.Contains(plain, title) {
			t.Fatalf("long-edge graph lost %q:\n%s", title, plain)
		}
	}
	assertFits(t, model.View().Content, 120, 28)
}

func TestGraphCanvasHandlesUnicodeAndTooSmallTerminal(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{{ID: "UNICOD", Title: "Crème 你好"}},
		nil,
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("UNICOD")
	model.setView(viewDependencies)
	model = resize(t, model, 48, 14)
	if plain := ansi.Strip(model.View().Content); !strings.Contains(plain, "Crème 你好") {
		t.Fatalf("unicode title missing:\n%s", plain)
	}
	assertFits(t, model.View().Content, 48, 14)

	model = resize(t, model, 72, 8)
	if plain := ansi.Strip(model.View().Content); !strings.Contains(plain, "Terminal too small") {
		t.Fatalf("small-terminal guidance missing:\n%s", plain)
	}
	assertFits(t, model.View().Content, 72, 8)
}

func TestGraphEdgeGlyphsRepresentJunctions(t *testing.T) {
	tests := map[uint8]string{
		graphEdgeLeft | graphEdgeRight:                               "─",
		graphEdgeUp | graphEdgeDown:                                  "│",
		graphEdgeDown | graphEdgeRight:                               "╭",
		graphEdgeUp | graphEdgeLeft:                                  "╯",
		graphEdgeUp | graphEdgeDown | graphEdgeRight:                 "├",
		graphEdgeUp | graphEdgeDown | graphEdgeLeft | graphEdgeRight: "┼",
	}
	for mask, expected := range tests {
		if actual := graphEdgeGlyph(mask); actual != expected {
			t.Fatalf("mask %04b = %q, want %q", mask, actual, expected)
		}
	}
}
