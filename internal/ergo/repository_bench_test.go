package ergo

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// benchmarkRepository writes a synthetic plans.jsonl shaped like a real Ergo log
// (containers, children, dependencies, state churn, messages, results) and
// returns a repository rooted at it.
func benchmarkRepository(b *testing.B, taskCount int) *Repository {
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
		"See `internal/ui/render.go` for the layout rules.\n"
	for index := range taskCount {
		id := fmt.Sprintf("T%05d", index)
		parent := ""
		kind := "new_task"
		if index%(childrenPerContainer+1) == 0 {
			kind = "new_epic"
		} else {
			parent = fmt.Sprintf("T%05d", index-index%(childrenPerContainer+1))
		}
		created := next()
		emit(kind, map[string]any{
			"id":         id,
			"uuid":       fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1),
			"epic_id":    parent,
			"state":      StateTodo,
			"title":      "Synthetic task " + id,
			"body":       "",
			"created_at": created,
		})
		emit("body", map[string]any{"id": id, "body": body, "ts": next()})
		if index > 0 && parent != "" {
			emit("link", map[string]any{
				"from_id": id,
				"to_id":   fmt.Sprintf("T%05d", index-1),
				"type":    "depends",
			})
		}
		// Roughly two thirds of the log is lifecycle churn on existing tasks.
		switch index % 3 {
		case 0:
			emit("claim", map[string]any{"id": id, "agent_id": "model@host"})
			emit("state", map[string]any{"id": id, "state": StateDoing, "ts": next()})
			emit("message", map[string]any{
				"task_id": id, "kind": "claim", "text": "Picked up " + id, "ts": next(),
			})
		case 1:
			emit("state", map[string]any{"id": id, "state": StateDone, "ts": next()})
			emit("unclaim", map[string]any{"id": id})
			emit("result", map[string]any{
				"task_id": id, "summary": "Verified", "path": "docs/verification.md", "ts": next(),
			})
		}
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	repository, err := Open(root)
	if err != nil {
		b.Fatal(err)
	}
	return repository
}

// BenchmarkRepositoryLoad measures the whole reload path the UI runs once per
// second: stat, open, read, JSON decode, replay, snapshot build.
func BenchmarkRepositoryLoad(b *testing.B) {
	for _, taskCount := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tasks=%d", taskCount), func(b *testing.B) {
			repository := benchmarkRepository(b, taskCount)
			info, err := os.Stat(repository.EventPath())
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(info.Size())
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := repository.Load(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRepositoryReadOnly isolates the file I/O half of a reload so it can
// be compared against BenchmarkRepositoryLoad to separate syscall cost from
// parse cost.
func BenchmarkRepositoryReadOnly(b *testing.B) {
	repository := benchmarkRepository(b, 1000)
	path := repository.EventPath()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		file, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, file); err != nil {
			b.Fatal(err)
		}
		file.Close()
	}
}

// BenchmarkRepositoryParseOnly isolates the CPU half of a reload: the JSON
// decode, event replay and snapshot build, with no file system access.
func BenchmarkRepositoryParseOnly(b *testing.B) {
	repository := benchmarkRepository(b, 1000)
	events, err := readEvents(repository.EventPath())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tasks, deps, err := replay(events)
		if err != nil {
			b.Fatal(err)
		}
		buildSnapshot("root", ".ergo", "plans.jsonl", "v", tasks, deps)
	}
}

// BenchmarkOpenDiscover measures the parent walk ev runs at startup to find the
// .ergo directory. Each level costs a stat.
func BenchmarkOpenDiscover(b *testing.B) {
	repository := benchmarkRepository(b, 100)
	nested := filepath.Join(repository.Root(), "a", "b", "c", "d", "e")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Open(nested); err != nil {
			b.Fatal(err)
		}
	}
}
