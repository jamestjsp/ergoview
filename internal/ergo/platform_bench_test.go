package ergo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// The benchmarks in this file measure operating system primitives rather than
// Ergo View itself. Their purpose is to make cross-platform CI results
// interpretable: BenchmarkCPUControl establishes how much slower a runner is at
// pure computation, so the remaining results can be read as "expensive on this
// OS" rather than "this runner has a slower CPU".

// BenchmarkCPUControl performs a fixed amount of pure computation with no
// syscalls. Use it to normalise every other benchmark across runners.
func BenchmarkCPUControl(b *testing.B) {
	block := bytes.Repeat([]byte("ergo view performance control block"), 512)
	b.SetBytes(int64(len(block)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		digest := sha256.Sum256(block)
		if digest[0] == 0xff && digest[1] == 0xff && digest[2] == 0xff {
			b.Fatal("unreachable")
		}
	}
}

// BenchmarkStat measures the bare metadata syscall the reload does on every
// tick. Windows pays far more here than Unix, especially with a real-time virus
// scanner attached to the file.
func BenchmarkStat(b *testing.B) {
	path := benchmarkFile(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := os.Stat(path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpenClose measures a bare open/close pair, the other syscall the
// reload repeats every tick.
func BenchmarkOpenClose(b *testing.B) {
	path := benchmarkFile(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		file, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		file.Close()
	}
}

// BenchmarkWriteFrame measures writing a terminal-frame-sized buffer to a
// handle. It cannot reproduce a real console, but it does capture the cost of
// the write path itself, which on Windows is markedly more expensive.
func BenchmarkWriteFrame(b *testing.B) {
	const frameBytes = 32 * 1024
	frame := bytes.Repeat([]byte("x"), frameBytes)
	targets := map[string]string{
		"devnull": os.DevNull,
		"file":    filepath.Join(b.TempDir(), "frames.out"),
	}
	for name, target := range targets {
		b.Run(name, func(b *testing.B) {
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				b.Skip(err)
			}
			defer file.Close()
			b.SetBytes(frameBytes)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := file.Write(frame); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkProcessSpawn measures raw process creation, which every task action
// pays. Windows CreateProcess is far more expensive than a Unix fork/exec, and
// each spawn is also a virus scanner entry point.
func BenchmarkProcessSpawn(b *testing.B) {
	name, args := trivialCommand()
	if _, err := exec.LookPath(name); err != nil {
		b.Skipf("%s unavailable: %v", name, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		command := exec.CommandContext(context.Background(), name, args...)
		if err := command.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSpawnGoBinary spawns this test binary, a stand-in for spawning the
// real ergo executable: a large statically linked Go program, which is the
// worst case for Windows image loading and virus scanning.
func BenchmarkSpawnGoBinary(b *testing.B) {
	self, err := os.Executable()
	if err != nil {
		b.Skip(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		command := exec.CommandContext(context.Background(), self, "-test.run=^$", "-test.bench=^$")
		if err := command.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLookPath measures PATH resolution, which exec.Command performs on
// every call. On Windows this multiplies every PATH entry by every PATHEXT
// suffix and stats each candidate.
func BenchmarkLookPath(b *testing.B) {
	name := "git"
	if _, err := exec.LookPath(name); err != nil {
		b.Skipf("%s unavailable: %v", name, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := exec.LookPath(name); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCachedRunnerBinaryPath(b *testing.B) {
	runner := NewRunner("ergo", "", "")
	runner.binary.lookPath = func(string) (string, error) {
		return "/usr/local/bin/ergo", nil
	}
	if _, err := runner.binary.resolve(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := runner.binary.resolve(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFile(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "plans.jsonl")
	var content bytes.Buffer
	for index := range 200 {
		content.WriteString(`{"type":"state","ts":"2026-07-23T12:00:00Z","data":{"id":"T`)
		content.WriteString(strconv.Itoa(index))
		content.WriteString(`","state":"done"}}` + "\n")
	}
	if err := os.WriteFile(path, content.Bytes(), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

// trivialCommand returns a command that starts and exits immediately, so the
// benchmark measures process creation rather than the child's work.
func trivialCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit"}
	}
	return "/bin/sh", []string{"-c", "exit"}
}
