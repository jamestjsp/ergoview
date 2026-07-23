package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type commandCall struct {
	args  []string
	input string
}

type fakeCommander struct {
	calls  []commandCall
	output string
	err    error
	plan   string
	path   string
}

func (f *fakeCommander) Run(_ context.Context, input io.Reader, args ...string) (string, error) {
	body := ""
	if input != nil {
		data, _ := io.ReadAll(input)
		body = string(data)
	}
	f.calls = append(f.calls, commandCall{args: append([]string(nil), args...), input: body})
	if len(args) >= 3 && args[0] == "plan" && args[1] == "--file" {
		f.path = args[2]
		data, _ := os.ReadFile(args[2])
		f.plan = string(data)
	}
	return f.output, f.err
}

func TestExecuteNewTaskUsesOfficialCommandAndStdin(t *testing.T) {
	runner := &fakeCommander{output: "NEW123 - Add login"}
	output, err := executeAction(context.Background(), runner, actionRequest{
		kind: actionNewTask,
		values: map[string]string{
			"title":  "Add login",
			"parent": "EPIC01",
			"body":   "## Goal\nShip login.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output != "NEW123 - Add login" || len(runner.calls) != 1 {
		t.Fatalf("output=%q calls=%#v", output, runner.calls)
	}
	call := runner.calls[0]
	if !reflect.DeepEqual(call.args[:2], []string{"new", "task"}) {
		t.Fatalf("args = %v", call.args)
	}
	if !strings.Contains(call.args[2], `"epic":"EPIC01"`) || !strings.Contains(call.args[2], `"title":"Add login"`) {
		t.Fatalf("payload = %s", call.args[2])
	}
	if call.input != "## Goal\nShip login." {
		t.Fatalf("stdin = %q", call.input)
	}
}

func TestExecuteLifecycleAndGraphCommands(t *testing.T) {
	tests := []struct {
		name    string
		request actionRequest
		want    []string
	}{
		{
			name: "done",
			request: actionRequest{
				kind:     actionDone,
				targetID: "TASK01",
				values: map[string]string{
					"message": "Verified",
					"result":  "docs/result.md",
				},
			},
			want: []string{"done", "TASK01", "-m", "Verified", "--result", "docs/result.md"},
		},
		{
			name: "sequence",
			request: actionRequest{
				kind:     actionSequence,
				targetID: "TASK02",
				values:   map[string]string{"dependency": "TASK01"},
			},
			want: []string{"sequence", "TASK01", "TASK02"},
		},
		{
			name: "move root",
			request: actionRequest{
				kind:     actionMove,
				targetID: "TASK02",
				values:   map[string]string{"parent": ""},
			},
			want: []string{"move", "TASK02", "--root"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeCommander{}
			if _, err := executeAction(context.Background(), runner, test.request); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runner.calls[0].args, test.want) {
				t.Fatalf("args = %v, want %v", runner.calls[0].args, test.want)
			}
		})
	}
}

func TestExecutePlanUsesTemporaryMarkdownFile(t *testing.T) {
	runner := &fakeCommander{}
	_, err := executeAction(context.Background(), runner, actionRequest{
		kind: actionNewPlan,
		values: map[string]string{
			"title": "Authentication",
			"plan":  "# Schema\nCreate tables.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.plan != "# Schema\nCreate tables." {
		t.Fatalf("plan = %q", runner.plan)
	}
	if _, err := os.Stat(runner.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary plan remains at %q: %v", runner.path, err)
	}
}

func TestDialogValidationAndCancellation(t *testing.T) {
	model := testModel(t)
	model.openDialog(actionNewTask)
	if command := model.advanceDialog(); command != nil {
		t.Fatal("blank required field started an action")
	}
	if model.dialog.err == "" {
		t.Fatal("required-field error is missing")
	}
	updated, _ := model.updateDialog(key("esc"))
	model = updated
	if model.dialog != nil {
		t.Fatal("escape did not close the dialog")
	}
}

func TestCancelRequiresConfirmation(t *testing.T) {
	model := testModel(t)
	model.rebuildRows("BLOCKD")
	model.openDialog(actionCancel)
	model.dialog.area.SetValue("No longer wanted")
	model.advanceDialog()
	model.dialog.input.SetValue("")
	model.advanceDialog()
	if !model.dialog.confirm {
		t.Fatal("cancel did not enter confirmation state")
	}
	updated, _ := model.updateDialog(key("n"))
	model = updated
	if model.dialog != nil {
		t.Fatal("declining confirmation did not close the dialog")
	}
}

func TestActionSuccessClosesDialogAndFailurePreservesInput(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		runner := &fakeCommander{output: "BLOCKD done"}
		model := testModel(t)
		model.runner = runner
		model.rebuildRows("BLOCKD")
		model.openDialog(actionDone)
		command := model.beginAction()
		message := command()
		updated, _ := model.Update(message)
		model = updated.(Model)
		if model.dialog != nil || model.status != "BLOCKD done" {
			t.Fatalf("dialog=%#v status=%q", model.dialog, model.status)
		}
	})

	t.Run("failure", func(t *testing.T) {
		runner := &fakeCommander{err: errors.New("claim conflict")}
		model := testModel(t)
		model.runner = runner
		model.rebuildRows("BLOCKD")
		model.openDialog(actionDone)
		command := model.beginAction()
		message := command()
		updated, _ := model.Update(message)
		model = updated.(Model)
		if model.dialog == nil || !strings.Contains(model.dialog.err, "claim conflict") {
			t.Fatalf("dialog=%#v", model.dialog)
		}
	})
}

func TestConfiguredAgentIsUsedForClaim(t *testing.T) {
	runner := &fakeCommander{}
	model := New(testSnapshot(t), Options{Agent: "codex@host", Runner: runner})
	model.rebuildRows("BLOCKD")
	model.openDialog(actionClaim)
	if len(model.dialog.fields) != 0 || !model.dialog.confirm {
		t.Fatalf("claim dialog = %#v", model.dialog)
	}
	command := model.beginAction()
	command()
	if !reflect.DeepEqual(runner.calls[0].args, []string{"claim", "BLOCKD", "--agent", "codex@host"}) {
		t.Fatalf("args = %v", runner.calls[0].args)
	}
}

func TestActionDialogFitsNarrowAndWideLayouts(t *testing.T) {
	for _, width := range []int{72, 140} {
		model := resize(t, testModel(t), width, 32)
		model.openDialog(actionNewPlan)
		assertFits(t, model.View().Content, width, 32)
	}
}

func TestContainerActionMenuExcludesLifecycle(t *testing.T) {
	model := resize(t, testModel(t), 120, 28)
	model.rebuildRows("EPIC01")
	model.actionMenu = true
	plain := ansi.Strip(model.View().Content)
	if strings.Contains(plain, "mark done") || !strings.Contains(plain, "edit body") {
		t.Fatalf("container action menu:\n%s", plain)
	}
}

var _ CommandRunner = (*ergo.Runner)(nil)
