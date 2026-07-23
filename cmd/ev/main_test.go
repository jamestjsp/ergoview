package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestHelpIsComplete(t *testing.T) {
	output := captureStdout(t, printHelp)
	for _, expected := range []string{"ev [--dir path] [--agent identity]", "--dir", "--agent", "--version"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("help missing %q:\n%s", expected, output)
		}
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	err := run([]string{"unexpected"})
	if err == nil || !strings.Contains(err.Error(), "usage: ev") {
		t.Fatalf("error = %v", err)
	}
}

func captureStdout(t *testing.T, action func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	defer func() {
		os.Stdout = original
	}()
	action()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, read); err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
