package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type graphTaskSpec struct {
	ID       string
	Title    string
	ParentID string
	State    ergo.State
}

type graphDependencySpec struct {
	From string
	To   string
}

func TestAdaptiveGraphLayoutRanksAndOrientsDiamond(t *testing.T) {
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

	wide := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "B",
		Scope:    graphScopeLineage,
		Width:    140,
		Height:   20,
	})
	if wide.Orientation != graphHorizontal {
		t.Fatalf("wide orientation = %v, want horizontal", wide.Orientation)
	}
	assertGraphRanks(t, wide, map[string]int{"A": 0, "B": 1, "C": 1, "D": 2})
	if len(wide.Edges) != 4 {
		t.Fatalf("edge count = %d, want 4", len(wide.Edges))
	}

	tall := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "B",
		Scope:    graphScopeLineage,
		Width:    48,
		Height:   40,
	})
	if tall.Orientation != graphVertical {
		t.Fatalf("tall orientation = %v, want vertical", tall.Orientation)
	}
	assertGraphRanks(t, tall, map[string]int{"A": 0, "B": 1, "C": 1, "D": 2})
}

func TestAdaptiveGraphLayoutUsesStableCrossingReduction(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "A", Title: "First source"},
			{ID: "B", Title: "Second source"},
			{ID: "X", Title: "Second target"},
			{ID: "Y", Title: "First target"},
			{ID: "Z", Title: "Join"},
		},
		[]graphDependencySpec{
			{From: "A", To: "Y"},
			{From: "B", To: "X"},
			{From: "X", To: "Z"},
			{From: "Y", To: "Z"},
		},
	)
	request := dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "Z",
		Scope:    graphScopeLineage,
		Width:    140,
		Height:   24,
	}
	first := buildDependencyGraphLayout(request)
	second := buildDependencyGraphLayout(request)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical requests produced different layouts")
	}
	x, _ := first.node("X")
	y, _ := first.node("Y")
	if y.Rect.Y >= x.Rect.Y {
		t.Fatalf("crossing reduction ordered Y at %d and X at %d", y.Rect.Y, x.Rect.Y)
	}
}

func TestAdaptiveGraphLayoutSummarizesHiddenLineage(t *testing.T) {
	var tasks []graphTaskSpec
	var dependencies []graphDependencySpec
	for index := range 12 {
		id := fmt.Sprintf("T%02d", index)
		tasks = append(tasks, graphTaskSpec{ID: id, Title: "Chain " + id})
		if index > 0 {
			dependencies = append(dependencies, graphDependencySpec{
				From: fmt.Sprintf("T%02d", index-1),
				To:   id,
			})
		}
	}
	snapshot := graphTestSnapshot(t, tasks, dependencies)
	layout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "T05",
		Scope:    graphScopeAdaptive,
		Width:    44,
		Height:   16,
	})
	upstream, upstreamOK := layout.node(upstreamOverflowID)
	downstream, downstreamOK := layout.node(downstreamOverflowID)
	if !upstreamOK || !downstreamOK {
		t.Fatalf("overflow nodes missing: upstream=%v downstream=%v", upstreamOK, downstreamOK)
	}
	if len(upstream.HiddenIDs) == 0 || len(downstream.HiddenIDs) == 0 {
		t.Fatalf("hidden lineage not recorded: upstream=%v downstream=%v", upstream.HiddenIDs, downstream.HiddenIDs)
	}
	if _, ok := layout.node("T05"); !ok {
		t.Fatal("focused task was omitted")
	}
}

func TestGraphScopesKeepHierarchySeparateFromDependencies(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{
			{ID: "EPIC", Title: "Delivery epic"},
			{ID: "CHILD", Title: "Independent child", ParentID: "EPIC"},
			{ID: "OTHER", Title: "Depends on epic"},
			{ID: "LOOSE", Title: "Disconnected"},
		},
		[]graphDependencySpec{{From: "EPIC", To: "OTHER"}},
	)

	childLayout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "CHILD",
		Scope:    graphScopeLineage,
		Width:    100,
		Height:   24,
	})
	if len(childLayout.Nodes) != 1 || childLayout.Nodes[0].ID != "CHILD" {
		t.Fatalf("containment leaked into dependency graph: %#v", childLayout.Nodes)
	}

	otherLayout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "OTHER",
		Scope:    graphScopeLineage,
		Width:    100,
		Height:   24,
	})
	epic, ok := otherLayout.node("EPIC")
	if !ok || !epic.Task.Container {
		t.Fatalf("explicit epic dependency missing: %#v", epic)
	}
	if _, ok := otherLayout.node("LOOSE"); ok {
		t.Fatal("disconnected task appeared in focused lineage")
	}
}

func TestGraphLayoutHandlesUnicodeTinyAndMissingReferences(t *testing.T) {
	snapshot := graphTestSnapshot(t,
		[]graphTaskSpec{{ID: "UNICOD", Title: "Crème brûlée 你好"}},
		[]graphDependencySpec{{From: "MISSING", To: "UNICOD"}},
	)
	layout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "UNICOD",
		Scope:    graphScopeAdaptive,
		Width:    8,
		Height:   4,
	})
	node, ok := layout.node("UNICOD")
	if !ok || node.Task.Title != "Crème brûlée 你好" {
		t.Fatalf("unicode node = %#v, found=%v", node, ok)
	}
	if node.Rect.Width < 1 || node.Rect.Height < 1 || len(layout.Edges) != 0 {
		t.Fatalf("tiny layout = %#v", layout)
	}
}

func BenchmarkAdaptiveGraphLayout(b *testing.B) {
	const taskCount = 1000
	tasks := make([]graphTaskSpec, 0, taskCount)
	dependencies := make([]graphDependencySpec, 0, taskCount*2)
	for index := range taskCount {
		id := fmt.Sprintf("T%04d", index)
		tasks = append(tasks, graphTaskSpec{ID: id, Title: "Synthetic task " + id})
		if index > 0 {
			dependencies = append(dependencies, graphDependencySpec{
				From: fmt.Sprintf("T%04d", index-1),
				To:   id,
			})
		}
		if index >= 7 {
			dependencies = append(dependencies, graphDependencySpec{
				From: fmt.Sprintf("T%04d", index-7),
				To:   id,
			})
		}
	}
	snapshot := graphTestSnapshot(b, tasks, dependencies)
	request := dependencyGraphRequest{
		Snapshot: snapshot,
		FocusID:  "T0500",
		Scope:    graphScopeAdaptive,
		Width:    140,
		Height:   36,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buildDependencyGraphLayout(request)
	}
}

func assertGraphRanks(t *testing.T, layout dependencyGraphLayout, expected map[string]int) {
	t.Helper()
	for id, expectedRank := range expected {
		node, ok := layout.node(id)
		if !ok {
			t.Fatalf("node %s missing", id)
		}
		if node.Rank != expectedRank {
			t.Fatalf("node %s rank = %d, want %d", id, node.Rank, expectedRank)
		}
	}
}

func graphTestSnapshot(t testing.TB, tasks []graphTaskSpec, dependencies []graphDependencySpec) ergo.Snapshot {
	t.Helper()
	root := t.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ergoDir, "plans.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	timestamp := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	for index, task := range tasks {
		state := task.State
		if state == "" {
			state = ergo.StateTodo
		}
		event := map[string]any{
			"type": "new_task",
			"ts":   timestamp.Add(time.Duration(index) * time.Second),
			"data": map[string]any{
				"id":         task.ID,
				"uuid":       fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1),
				"epic_id":    task.ParentID,
				"state":      state,
				"title":      task.Title,
				"body":       "",
				"created_at": timestamp.Add(time.Duration(index) * time.Second),
			},
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	for index, dependency := range dependencies {
		event := map[string]any{
			"type": "link",
			"ts":   timestamp.Add(time.Duration(len(tasks)+index) * time.Second),
			"data": map[string]any{
				"from_id": dependency.To,
				"to_id":   dependency.From,
				"type":    "depends",
			},
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
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
