package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func TestGraphNavigationMovesSpatiallyAndTargetsActions(t *testing.T) {
	model := graphInteractionModel(t)
	if model.selectedID() != "B" || model.graphFocusID != "B" {
		t.Fatalf("initial selection=%q focus=%q", model.selectedID(), model.graphFocusID)
	}

	updated, _ := model.Update(key("l"))
	model = updated.(Model)
	if model.selectedID() != "D" {
		t.Fatalf("right selection = %q, want D", model.selectedID())
	}
	if model.graphFocusID != "B" {
		t.Fatalf("navigation changed focus to %q", model.graphFocusID)
	}
	actionTarget, ok := model.selectedTask()
	if !ok || actionTarget.ID != "D" {
		t.Fatalf("action target = %#v, found=%v", actionTarget, ok)
	}
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "focus B") || !strings.Contains(plain, "◎") {
		t.Fatalf("focus and selection context missing:\n%s", plain)
	}

	updated, _ = model.Update(key("h"))
	model = updated.(Model)
	if model.selectedID() != "B" {
		t.Fatalf("left selection = %q, want B", model.selectedID())
	}
}

func TestGraphFocusHistoryAndDepthControls(t *testing.T) {
	model := graphInteractionModel(t)
	updated, _ := model.Update(key("l"))
	model = updated.(Model)
	updated, _ = model.Update(key("enter"))
	model = updated.(Model)
	if model.graphFocusID != "D" || len(model.graphFocusHistory) != 1 || model.graphFocusHistory[0] != "B" {
		t.Fatalf("focus=%q history=%v", model.graphFocusID, model.graphFocusHistory)
	}

	updated, _ = model.Update(key("esc"))
	model = updated.(Model)
	if model.graphFocusID != "B" || model.selectedID() != "B" || len(model.graphFocusHistory) != 0 {
		t.Fatalf("restored focus=%q selection=%q history=%v", model.graphFocusID, model.selectedID(), model.graphFocusHistory)
	}

	if model.graphScope != graphScopeAdaptive {
		t.Fatalf("initial scope = %v", model.graphScope)
	}
	updated, _ = model.Update(key("d"))
	model = updated.(Model)
	if model.graphScope != graphScopeLineage {
		t.Fatalf("first depth cycle = %v, want lineage", model.graphScope)
	}
	updated, _ = model.Update(key("d"))
	model = updated.(Model)
	if model.graphScope != graphScopeDirect {
		t.Fatalf("second depth cycle = %v, want direct", model.graphScope)
	}
	updated, _ = model.Update(key("d"))
	model = updated.(Model)
	if model.graphScope != graphScopeAdaptive {
		t.Fatalf("third depth cycle = %v, want adaptive", model.graphScope)
	}
}

func TestGraphMouseSelectsNodeAndBlankCanvasIsStable(t *testing.T) {
	model := graphInteractionModel(t)
	layout := model.graphLayoutForView()
	node, ok := layout.node("D")
	if !ok {
		t.Fatal("D missing from graph")
	}
	style := model.styles.focusPane
	click := tea.MouseClickMsg{
		X:      node.Rect.X + node.Rect.Width/2 + style.GetHorizontalFrameSize()/2,
		Y:      node.Rect.Y + node.Rect.Height/2 + 2 + style.GetVerticalFrameSize()/2 + 1,
		Button: tea.MouseLeft,
	}
	updated, _ := model.Update(click)
	model = updated.(Model)
	if model.selectedID() != "D" {
		t.Fatalf("mouse selection = %q, want D", model.selectedID())
	}

	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: model.height - 2, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.selectedID() != "D" {
		t.Fatalf("blank click changed selection to %q", model.selectedID())
	}
}

func TestGraphOverflowExpandsDepth(t *testing.T) {
	var tasks []graphTaskSpec
	var dependencies []graphDependencySpec
	for index := range 9 {
		id := fmt.Sprintf("T%d", index)
		tasks = append(tasks, graphTaskSpec{ID: id, Title: "Task " + id})
		if index > 0 {
			dependencies = append(dependencies, graphDependencySpec{
				From: fmt.Sprintf("T%d", index-1),
				To:   id,
			})
		}
	}
	snapshot := graphTestSnapshot(t, tasks, dependencies)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("T5")
	model.setView(viewDependencies)
	model = resize(t, model, 140, 26)
	model.graphScope = graphScopeDirect
	if !model.selectTaskID("T6") {
		t.Fatal("could not select direct downstream node")
	}
	updated, _ := model.Update(key("l"))
	model = updated.(Model)
	if model.graphScope != graphScopeAdaptive {
		t.Fatalf("overflow navigation scope = %v, want adaptive", model.graphScope)
	}
}

func TestGraphFocusSurvivesResizeReloadAndMatchingFilter(t *testing.T) {
	model := graphInteractionModel(t)
	model = resize(t, model, 72, 24)
	if model.selectedID() != "B" || model.graphFocusID != "B" {
		t.Fatalf("resize selection=%q focus=%q", model.selectedID(), model.graphFocusID)
	}

	model.filter = filterWaiting
	model.rebuildRows("B")
	if model.selectedID() != "B" || model.graphFocusID != "B" {
		t.Fatalf("filter selection=%q focus=%q", model.selectedID(), model.graphFocusID)
	}

	reloaded := model.snapshot
	reloaded.Version = "graph-reload"
	updated, _ := model.Update(snapshotLoadedMsg{snapshot: reloaded, changed: true})
	model = updated.(Model)
	if model.selectedID() != "B" || model.graphFocusID != "B" {
		t.Fatalf("reload selection=%q focus=%q", model.selectedID(), model.graphFocusID)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "B") {
		t.Fatal("selected graph node is not visible after reload")
	}
}

func TestGraphFocusResetsWhenFilterRemovesIt(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "A", Title: "Foundation"},
			{ID: "B", Title: "Old focus"},
			{ID: "X", Title: "Active foundation", State: ergo.StateDoing},
			{ID: "Y", Title: "Active release", State: ergo.StateDoing},
		},
		[]graphDependencySpec{
			{From: "A", To: "B"},
			{From: "X", To: "Y"},
		},
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("B")
	model.setView(viewDependencies)
	model = resize(t, model, 120, 26)
	model.graphFocusHistory = []string{"A"}

	model.filter = filterDoing
	model.rebuildRows("B")

	if model.selectedID() != "X" || model.graphFocusID != "X" {
		t.Fatalf("filtered selection=%q focus=%q, want X", model.selectedID(), model.graphFocusID)
	}
	if len(model.graphFocusHistory) != 0 {
		t.Fatalf("stale focus history = %v", model.graphFocusHistory)
	}
	layout := model.graphLayoutForView()
	if _, ok := layout.node("X"); !ok {
		t.Fatal("filtered graph does not show the replacement focus")
	}
	if _, ok := layout.node("B"); ok {
		t.Fatal("filtered graph still shows the removed focus component")
	}

	updated, _ := model.Update(key("l"))
	model = updated.(Model)
	if model.selectedID() != "Y" {
		t.Fatalf("spatial navigation selection = %q, want Y", model.selectedID())
	}
}

func TestGraphHelpAndFooterAreContextual(t *testing.T) {
	model := graphInteractionModel(t)
	plain := ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "h/j/k/l node") || !strings.Contains(plain, "d depth") {
		t.Fatalf("graph footer missing:\n%s", plain)
	}
	updated, _ := model.Update(key("?"))
	model = updated.(Model)
	updated, _ = model.Update(key("pgdown"))
	model = updated.(Model)
	plain = ansi.Strip(model.View().Content)
	if !strings.Contains(plain, "Dependency graph") || !strings.Contains(plain, "move spatially between nodes") {
		t.Fatalf("graph help missing:\n%s", plain)
	}
}

func graphInteractionModel(t *testing.T) Model {
	t.Helper()
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "A", Title: "Foundation"},
			{ID: "B", Title: "Left branch"},
			{ID: "C", Title: "Right branch"},
			{ID: "D", Title: "Release"},
		},
		[]graphDependencySpec{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "B", To: "D"},
			{From: "C", To: "D"},
		},
	)
	model := New(snapshot, Options{NoColor: true})
	model.rebuildRows("B")
	model.setView(viewDependencies)
	return resize(t, model, 120, 26)
}
