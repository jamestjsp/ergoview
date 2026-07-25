package ergo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunnerBuildsExplicitErgoInvocation(t *testing.T) {
	runner := NewRunner(os.Args[0], "/project", "codex@host")
	runner.build = helperCommand
	output, err := runner.Run(context.Background(), strings.NewReader("body"), "body", "ABC123")
	if err != nil {
		t.Fatal(err)
	}
	want := "--dir|/project|--agent|codex@host|body|ABC123|stdin=body"
	if output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}
}

func TestRunnerReturnsCommandError(t *testing.T) {
	runner := NewRunner(os.Args[0], "/project", "")
	runner.build = helperCommand
	_, err := runner.Run(context.Background(), nil, "fail")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if !reflect.DeepEqual(commandErr.Args, []string{"fail"}) {
		t.Fatalf("args = %v", commandErr.Args)
	}
	if commandErr.Output != "rejected by ergo" {
		t.Fatalf("output = %q", commandErr.Output)
	}
}

func TestRunnerHonorsCancellation(t *testing.T) {
	runner := NewRunner(os.Args[0], "/project", "")
	runner.build = helperCommand
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := runner.Run(ctx, nil, "sleep")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerCachesBinaryPathAcrossAgentCopies(t *testing.T) {
	runner := NewRunner("ergo-test", "/project", "")
	lookups := 0
	runner.binary.lookPath = func(name string) (string, error) {
		lookups++
		if name != "ergo-test" {
			t.Fatalf("lookup name = %q, want ergo-test", name)
		}
		return os.Args[0], nil
	}
	runner.build = helperCommand

	if _, err := runner.Run(context.Background(), nil, "show", "ABC123"); err != nil {
		t.Fatal(err)
	}
	withAgent := runner.WithAgent("codex@host")
	output, err := withAgent.Run(context.Background(), nil, "claim", "ABC123")
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 1 {
		t.Fatalf("binary lookups = %d, want 1", lookups)
	}
	if !strings.Contains(output, "--agent|codex@host") {
		t.Fatalf("agent invocation = %q", output)
	}
}

func TestRunnerCachesBinaryLookupError(t *testing.T) {
	runner := NewRunner("missing-ergo", "/project", "")
	notFound := errors.New("not found")
	lookups := 0
	runner.binary.lookPath = func(string) (string, error) {
		lookups++
		return "", notFound
	}

	for range 2 {
		_, err := runner.Run(context.Background(), nil, "list")
		if !errors.Is(err, notFound) {
			t.Fatalf("error = %v, want lookup failure", err)
		}
	}
	if lookups != 1 {
		t.Fatalf("binary lookups = %d, want 1", lookups)
	}
}

func TestRunnerExplicitBinaryPathSkipsLookup(t *testing.T) {
	runner := NewRunner("./tools/ergo", "/project", "")
	runner.binary.lookPath = func(string) (string, error) {
		t.Fatal("explicit binary path used LookPath")
		return "", nil
	}
	runner.build = func(ctx context.Context, binary string, args ...string) *exec.Cmd {
		if binary != "./tools/ergo" {
			t.Fatalf("binary = %q, want ./tools/ergo", binary)
		}
		return helperCommand(ctx, binary, args...)
	}
	if _, err := runner.Run(context.Background(), nil, "list"); err != nil {
		t.Fatal(err)
	}
}

func helperCommand(ctx context.Context, _ string, args ...string) *exec.Cmd {
	commandArgs := []string{"-test.run=TestRunnerHelperProcess", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "ERGO_VIEW_HELPER=1")
	return command
}

func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("ERGO_VIEW_HELPER") != "1" {
		return
	}
	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index + 1
			break
		}
	}
	args := os.Args[separator:]
	for _, arg := range args {
		switch arg {
		case "fail":
			fmt.Fprint(os.Stderr, "rejected by ergo")
			os.Exit(2)
		case "sleep":
			time.Sleep(10 * time.Second)
		}
	}
	input, _ := io.ReadAll(os.Stdin)
	fmt.Printf("%s|stdin=%s", strings.Join(args, "|"), input)
	os.Exit(0)
}
