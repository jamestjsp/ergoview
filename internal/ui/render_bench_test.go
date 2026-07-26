package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

const (
	benchWidth  = 140
	benchHeight = 40
)

// benchmarkRoot writes a synthetic Ergo repository with containers, children,
// dependencies and Markdown bodies, and returns its root directory.
func benchmarkRoot(b *testing.B, taskCount int) string {
	b.Helper()
	root := b.TempDir()
	ergoDir := filepath.Join(root, ".ergo")
	if err := os.MkdirAll(ergoDir, 0o755); err != nil {
		b.Fatal(err)
	}
	file, err := os.Create(filepath.Join(ergoDir, "plans.jsonl"))
	if err != nil {
		b.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	timestamp := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clock := 0
	next := func() time.Time {
		clock++
		return timestamp.Add(time.Duration(clock) * time.Second)
	}
	emit := func(kind string, data map[string]any) {
		if err := encoder.Encode(map[string]any{"type": kind, "ts": next(), "data": data}); err != nil {
			b.Fatal(err)
		}
	}

	const childrenPerContainer = 8
	body := "## Goal\n\nDeliver the slice end to end.\n\n- [ ] implement\n- [ ] test\n\n" +
		"See `internal/ui/render.go` for the layout rules.\n\n" +
		"| field | meaning |\n| --- | --- |\n| id | short code |\n"
	states := []ergo.State{ergo.StateTodo, ergo.StateDoing, ergo.StateDone, ergo.StateBlocked, ergo.StateCanceled}
	for index := range taskCount {
		id := fmt.Sprintf("T%05d", index)
		parent := ""
		kind := "new_task"
		if index%(childrenPerContainer+1) == 0 {
			kind = "new_epic"
		} else {
			parent = fmt.Sprintf("T%05d", index-index%(childrenPerContainer+1))
		}
		emit(kind, map[string]any{
			"id":         id,
			"uuid":       fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1),
			"epic_id":    parent,
			"state":      ergo.StateTodo,
			"title":      "Synthetic task " + id + " with a reasonably long title",
			"body":       "",
			"created_at": next(),
		})
		emit("body", map[string]any{"id": id, "body": body, "ts": next()})
		if parent != "" {
			state := states[index%len(states)]
			if state != ergo.StateTodo {
				emit("state", map[string]any{"id": id, "state": state, "ts": next()})
			}
			if state == ergo.StateDoing {
				emit("claim", map[string]any{"id": id, "agent_id": "model@host"})
			}
			if index > 1 {
				emit("link", map[string]any{
					"from_id": id,
					"to_id":   fmt.Sprintf("T%05d", index-1),
					"type":    "depends",
				})
			}
			emit("message", map[string]any{
				"task_id": id, "kind": "claim", "text": "Picked up " + id, "ts": next(),
			})
		}
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	return root
}

func benchmarkSnapshot(b *testing.B, taskCount int) ergo.Snapshot {
	b.Helper()
	repository, err := ergo.Open(benchmarkRoot(b, taskCount))
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		b.Fatal(err)
	}
	return snapshot
}

func benchmarkModel(b *testing.B, taskCount int, view viewMode) Model {
	b.Helper()
	model := New(benchmarkSnapshot(b, taskCount), testOptions(Options{}))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: benchWidth, Height: benchHeight})
	model = updated.(Model)
	model.setView(view)
	return model
}

// BenchmarkStartup measures everything ev does before the first frame:
// discover .ergo, load the event log, and build the model (which renders the
// first detail pane through Glamour).
func BenchmarkStartup(b *testing.B) {
	for _, taskCount := range []int{100, 1000} {
		b.Run(fmt.Sprintf("tasks=%d", taskCount), func(b *testing.B) {
			root := benchmarkRoot(b, taskCount)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				repository, err := ergo.Open(root)
				if err != nil {
					b.Fatal(err)
				}
				snapshot, err := repository.Load()
				if err != nil {
					b.Fatal(err)
				}
				New(snapshot, testOptions(Options{}))
			}
		})
	}
}

// BenchmarkView measures one frame of rendering per view mode and reports the
// frame size, which is what has to be written to the terminal.
func BenchmarkView(b *testing.B) {
	modes := map[string]viewMode{
		"overview":     viewOverview,
		"board":        viewBoard,
		"dependencies": viewDependencies,
	}
	for name, mode := range modes {
		b.Run(name, func(b *testing.B) {
			model := benchmarkModel(b, 1000, mode)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = model.View()
			}
			b.StopTimer()
			b.ReportMetric(float64(len(model.View().Content)), "frame_bytes")
		})
	}
}

// BenchmarkKeyPress measures the full interactive round trip for a cursor move:
// update the model, re-render the detail pane, and produce the next frame. This
// is the latency a user feels per keystroke.
func BenchmarkKeyPress(b *testing.B) {
	model := benchmarkModel(b, 1000, viewOverview)
	press := tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		updated, _ := model.Update(press)
		next := updated.(Model)
		_ = next.View()
	}
}

// BenchmarkSyncDetail measures the detail pane rebuild that runs on every
// selection change, resize and reload.
func BenchmarkSyncDetail(b *testing.B) {
	model := benchmarkModel(b, 1000, viewOverview)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		model.syncDetail()
	}
}

// BenchmarkRenderMarkdown measures one Markdown body render, including the
// Glamour renderer construction that renderMarkdown repeats per call.
func BenchmarkRenderMarkdown(b *testing.B) {
	model := benchmarkModel(b, 100, viewOverview)
	body := "## Goal\n\nDeliver the slice end to end.\n\n- [ ] implement\n- [ ] test\n\n" +
		"See `internal/ui/render.go` for the layout rules.\n\n" +
		"| field | meaning |\n| --- | --- |\n| id | short code |\n"
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		model.renderMarkdown(body, 60)
	}
}

// BenchmarkReload measures the once-per-second background reload plus the model
// update it feeds, i.e. the work ev does even when the user is idle.
func BenchmarkReload(b *testing.B) {
	repository, err := ergo.Open(benchmarkRoot(b, 1000))
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := repository.Load()
	if err != nil {
		b.Fatal(err)
	}
	model := New(snapshot, testOptions(Options{Source: repository}))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: benchWidth, Height: benchHeight})
	model = updated.(Model)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		loaded, changed, err := repository.LoadIfChanged()
		if err != nil {
			b.Fatal(err)
		}
		next, _ := model.Update(snapshotLoadedMsg{snapshot: loaded, changed: changed})
		_ = next.(Model).View()
	}
}
