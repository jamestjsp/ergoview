package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// BenchmarkBinaryStartup measures how long the real ev executable takes to
// start and exit. It isolates process creation, image loading and Go runtime
// initialisation from any work ev does, which is the part of "slow to launch"
// that no in-process benchmark can see.
func BenchmarkBinaryStartup(b *testing.B) {
	binary := filepath.Join(b.TempDir(), "ev")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		b.Skipf("build failed: %v", err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		b.Fatal(err)
	}
	// Warm the file system cache so the first iteration is not an outlier.
	if err := exec.Command(binary, "--version").Run(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := exec.Command(binary, "--version").Run(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(info.Size()), "binary_bytes")
}
