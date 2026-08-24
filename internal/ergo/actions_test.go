package ergo

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunnerDetectsAndCachesReleasedCapabilities(t *testing.T) {
	t.Setenv("ERGO_VIEW_HELPER_VERSION", "6.0.0")
	runner := NewRunner(os.Args[0], "/project", "")
	builds := 0
	runner.build = func(ctx context.Context, binary string, args ...string) *exec.Cmd {
		builds++
		return helperCommand(ctx, binary, args...)
	}

	first, err := runner.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Major != 6 || !first.ModernCreation || !first.Journal || !first.Draft || !first.Fail || !first.Open {
		t.Fatalf("capabilities = %#v / %#v", first, second)
	}
	if builds != 1 {
		t.Fatalf("capability probe builds = %d, want 1", builds)
	}
}

func TestRunnerDetectsDevelopmentCapabilitiesFromHelp(t *testing.T) {
	t.Setenv("ERGO_VIEW_HELPER_VERSION", "dev")
	t.Setenv("ERGO_VIEW_HELPER_HELP", `new epic "<title>" --file <path> --draft
fail <id>
open <id>
result <id> "<text>"`)
	runner := NewRunner(os.Args[0], "/project", "")
	runner.build = helperCommand

	capabilities, err := runner.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Major != 6 || !capabilities.ModernCreation || !capabilities.Draft || !capabilities.Open || !capabilities.Journal || !capabilities.Fail {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}

func TestRunnerMapsTaskCreationAcrossErgoGenerations(t *testing.T) {
	tests := []struct {
		name  string
		major int
		draft bool
		want  string
	}{
		{name: "legacy JSON", major: 3, want: `--dir|/project|new|task|{"epic":"EPIC01","title":"Add login"}|stdin=body`},
		{name: "modern flags", major: 4, want: `--dir|/project|new|task|Add login|--epic|EPIC01|stdin=body`},
		{name: "draft", major: 6, draft: true, want: `--dir|/project|new|task|Add login|--epic|EPIC01|--draft|stdin=body`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := actionTestRunner(test.major)
			output, err := runner.Execute(context.Background(), CreateTask{
				Title: "Add login", ParentID: "EPIC01", Body: "body", Draft: test.draft,
			})
			if err != nil {
				t.Fatal(err)
			}
			if output != test.want {
				t.Fatalf("output = %q, want %q", output, test.want)
			}
		})
	}
}

func TestRunnerMapsEpicCreationAcrossErgoGenerations(t *testing.T) {
	tests := []struct {
		name  string
		major int
		draft bool
		want  []string
	}{
		{name: "legacy plan", major: 3, want: []string{"--dir|/project|plan|--file|", `|{"title":"Authentication"}|stdin=`}},
		{name: "modern epic", major: 4, want: []string{"--dir|/project|new|epic|Authentication|--file|", "|stdin=context"}},
		{name: "draft epic", major: 6, draft: true, want: []string{"--dir|/project|new|epic|Authentication|--file|", "|--draft|stdin=context"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := actionTestRunner(test.major)
			body := "context"
			if test.major < 4 {
				body = ""
			}
			output, err := runner.Execute(context.Background(), CreateEpic{
				Title: "Authentication", Body: body, Plan: "# Schema\nCreate tables.", Draft: test.draft,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(output, want) {
					t.Fatalf("output = %q, want substring %q", output, want)
				}
			}
		})
	}
}

func TestRunnerMapsLifecycleAndResultsAcrossErgoGenerations(t *testing.T) {
	t.Run("Ergo 4 lifecycle result", func(t *testing.T) {
		runner := actionTestRunner(4)
		output, err := runner.Execute(context.Background(), LifecycleTask{
			TaskID: "TASK01", Kind: LifecycleDone, Message: "Verified", ResultPath: "docs/proof.md",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "--dir|/project|done|TASK01|-m|Verified|--result|docs/proof.md|stdin="
		if output != want {
			t.Fatalf("output = %q, want %q", output, want)
		}
	})

	t.Run("Ergo 5 journal result", func(t *testing.T) {
		t.Setenv("ERGO_VIEW_HELPER_ALLOW_FAIL", "1")
		runner := actionTestRunner(5)
		var calls [][]string
		runner.build = recordingHelper(&calls)
		_, err := runner.Execute(context.Background(), LifecycleTask{
			TaskID: "TASK01", Kind: LifecycleFail, Message: "Tests failed",
			ResultSummary: "Failure report", ResultPath: "docs/failure.md",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := joinedCalls(calls); got != "--dir /project fail TASK01 -m Tests failed\n--dir /project result TASK01 Failure report --file docs/failure.md" {
			t.Fatalf("calls = %q", got)
		}
	})

	t.Run("Ergo 6 return", func(t *testing.T) {
		runner := actionTestRunner(6)
		output, err := runner.Execute(context.Background(), LifecycleTask{TaskID: "TASK01", Kind: LifecycleReturn, Message: "Retry"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output, "|open|TASK01|-m|Retry|") {
			t.Fatalf("output = %q", output)
		}
	})

	t.Run("older fail rejected", func(t *testing.T) {
		runner := actionTestRunner(4)
		_, err := runner.Execute(context.Background(), LifecycleTask{TaskID: "TASK01", Kind: LifecycleFail})
		if err == nil || !strings.Contains(err.Error(), "Ergo 4 does not support failed lifecycle outcomes") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestRunnerMapsVersionNeutralOperations(t *testing.T) {
	runner := actionTestRunner(6)
	tests := []struct {
		operation Operation
		want      string
	}{
		{operation: ClaimTask{TaskID: "TASK01", Agent: "codex@host"}, want: "|claim|TASK01|--agent|codex@host|"},
		{operation: RenameTask{TaskID: "TASK01", Title: "New title"}, want: "|title|TASK01|New title|"},
		{operation: ReplaceBody{TaskID: "TASK01", Body: "new body"}, want: "|body|TASK01|stdin=new body"},
		{operation: MoveTask{TaskID: "TASK01", ParentID: "EPIC01"}, want: "|move|TASK01|EPIC01|"},
		{operation: SetDependency{TaskID: "TASK01", DependencyID: "BASE01"}, want: "|sequence|BASE01|TASK01|"},
		{operation: SetDependency{TaskID: "TASK01", DependencyID: "BASE01", Remove: true}, want: "|unsequence|BASE01|TASK01|"},
		{operation: RecordResult{TaskID: "TASK01", Summary: "Verified"}, want: "|result|TASK01|Verified|"},
	}
	for _, test := range tests {
		output, err := runner.Execute(context.Background(), test.operation)
		if err != nil {
			t.Fatalf("%T: %v", test.operation, err)
		}
		if !strings.Contains(output, test.want) {
			t.Fatalf("%T output = %q, want %q", test.operation, output, test.want)
		}
	}
}

func actionTestRunner(major int) *Runner {
	runner := NewRunner(os.Args[0], "/project", "")
	runner.build = helperCommand
	runner.dialect = &dialectCache{known: true, value: capabilitiesForMajor(major)}
	return runner
}

func recordingHelper(calls *[][]string) commandBuilder {
	return func(ctx context.Context, binary string, args ...string) *exec.Cmd {
		*calls = append(*calls, append([]string(nil), args...))
		return helperCommand(ctx, binary, args...)
	}
}

func joinedCalls(calls [][]string) string {
	lines := make([]string, len(calls))
	for index, call := range calls {
		lines[index] = strings.Join(call, " ")
	}
	return strings.Join(lines, "\n")
}
