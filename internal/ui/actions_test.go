package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type fakeCommander struct {
	operations    []ergo.Operation
	output        string
	err           error
	capabilities  ergo.Capabilities
	capabilityErr error
}

func (f *fakeCommander) Execute(_ context.Context, operation ergo.Operation) (string, error) {
	f.operations = append(f.operations, operation)
	return f.output, f.err
}

func (f *fakeCommander) Capabilities(context.Context) (ergo.Capabilities, error) {
	return f.capabilities, f.capabilityErr
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
	if output != "NEW123 - Add login" || len(runner.operations) != 1 {
		t.Fatalf("output=%q operations=%#v", output, runner.operations)
	}
	want := ergo.CreateTask{Title: "Add login", ParentID: "EPIC01", Body: "## Goal\nShip login."}
	if !reflect.DeepEqual(runner.operations[0], want) {
		t.Fatalf("operation = %#v, want %#v", runner.operations[0], want)
	}
}

func TestExecuteLifecycleAndGraphCommands(t *testing.T) {
	tests := []struct {
		name    string
		request actionRequest
		want    ergo.Operation
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
			want: ergo.LifecycleTask{TaskID: "TASK01", Kind: ergo.LifecycleDone, Message: "Verified", ResultPath: "docs/result.md"},
		},
		{
			name: "sequence",
			request: actionRequest{
				kind:     actionSequence,
				targetID: "TASK02",
				values:   map[string]string{"dependency": "TASK01"},
			},
			want: ergo.SetDependency{TaskID: "TASK02", DependencyID: "TASK01"},
		},
		{
			name: "move root",
			request: actionRequest{
				kind:     actionMove,
				targetID: "TASK02",
				values:   map[string]string{"parent": ""},
			},
			want: ergo.MoveTask{TaskID: "TASK02"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeCommander{}
			if _, err := executeAction(context.Background(), runner, test.request); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runner.operations[0], test.want) {
				t.Fatalf("operation = %#v, want %#v", runner.operations[0], test.want)
			}
		})
	}
}

func TestExecutePlanPassesMarkdownToErgoAdapter(t *testing.T) {
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
	want := ergo.CreateEpic{Title: "Authentication", Plan: "# Schema\nCreate tables."}
	if !reflect.DeepEqual(runner.operations[0], want) {
		t.Fatalf("operation = %#v, want %#v", runner.operations[0], want)
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
	model := New(testSnapshot(t), testOptions(Options{Agent: "codex@host", Runner: runner}))
	model.rebuildRows("BLOCKD")
	model.openDialog(actionClaim)
	if len(model.dialog.fields) != 0 || !model.dialog.confirm {
		t.Fatalf("claim dialog = %#v", model.dialog)
	}
	command := model.beginAction()
	command()
	want := ergo.ClaimTask{TaskID: "BLOCKD", Agent: "codex@host"}
	if !reflect.DeepEqual(runner.operations[0], want) {
		t.Fatalf("operation = %#v, want %#v", runner.operations[0], want)
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

func TestErgo6CapabilitiesEnableDraftFailureAndResultWorkflows(t *testing.T) {
	capabilities := ergo.Capabilities{Major: 6, ModernCreation: true, Journal: true, Draft: true, Fail: true, Open: true}
	runner := &fakeCommander{capabilities: capabilities}
	model := New(testSnapshot(t), testOptions(Options{Runner: runner, Capabilities: &capabilities}))

	model.openDialog(actionNewTask)
	var keys []string
	for _, field := range model.dialog.fields {
		keys = append(keys, field.key)
	}
	if !reflect.DeepEqual(keys, []string{"title", "parent", "draft", "body"}) {
		t.Fatalf("new task fields = %v", keys)
	}

	_, err := executeAction(context.Background(), runner, actionRequest{
		kind: actionNewTask,
		values: map[string]string{
			"title": "Stage rollout", "draft": "yes", "body": "## Goal\nPrepare safely.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTask := ergo.CreateTask{Title: "Stage rollout", Body: "## Goal\nPrepare safely.", Draft: true}
	if !reflect.DeepEqual(runner.operations[len(runner.operations)-1], wantTask) {
		t.Fatalf("operation = %#v, want %#v", runner.operations[len(runner.operations)-1], wantTask)
	}

	_, err = executeAction(context.Background(), runner, actionRequest{
		kind: actionResult, targetID: "TASK01",
		values: map[string]string{"summary": "Verified compatibility", "result": "docs/proof.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantResult := ergo.RecordResult{TaskID: "TASK01", Summary: "Verified compatibility", Path: "docs/proof.md"}
	if !reflect.DeepEqual(runner.operations[len(runner.operations)-1], wantResult) {
		t.Fatalf("operation = %#v, want %#v", runner.operations[len(runner.operations)-1], wantResult)
	}
}

func TestActionMenusMatchDraftAndFailedSemantics(t *testing.T) {
	capabilities := ergo.Capabilities{Major: 6, ModernCreation: true, Journal: true, Draft: true, Fail: true, Open: true}
	model := renderFixtureModel(t, true)
	model.capabilities = capabilities
	model.capabilitiesReady = true
	model = resize(t, model, 120, 30)

	model.rebuildRows("STAGE1")
	model.actionMenu = true
	draftMenu := ansi.Strip(model.View().Content)
	for _, expected := range []string{"return to todo", "cancel", "record result"} {
		if !strings.Contains(draftMenu, expected) {
			t.Fatalf("draft menu missing %q:\n%s", expected, draftMenu)
		}
	}
	for _, absent := range []string{"claim or resume", "mark done", "mark failed", "mark blocked"} {
		if strings.Contains(draftMenu, absent) {
			t.Fatalf("draft menu contains %q:\n%s", absent, draftMenu)
		}
	}

	model.actionMenu = false
	model.rebuildRows("FAIL01")
	model.actionMenu = true
	failedMenu := ansi.Strip(model.View().Content)
	for _, expected := range []string{"claim or resume", "record result"} {
		if !strings.Contains(failedMenu, expected) {
			t.Fatalf("failed menu missing %q:\n%s", expected, failedMenu)
		}
	}
	for _, absent := range []string{"mark done", "return to todo", "cancel"} {
		if strings.Contains(failedMenu, absent) {
			t.Fatalf("failed menu contains %q:\n%s", absent, failedMenu)
		}
	}
}

func TestCapabilitiesLoadWithoutBlockingReadOnlyStartup(t *testing.T) {
	want := ergo.Capabilities{Major: 6, ModernCreation: true, Journal: true, Draft: true, Fail: true, Open: true}
	runner := &fakeCommander{capabilities: want}
	model := New(testSnapshot(t), testOptions(Options{Runner: runner}))
	if model.capabilitiesReady {
		t.Fatal("capabilities unexpectedly loaded synchronously")
	}
	message := loadCapabilities(runner)()
	updated, _ := model.Update(message)
	model = updated.(Model)
	if !model.capabilitiesReady || model.capabilities != want {
		t.Fatalf("capabilities = %#v ready=%v", model.capabilities, model.capabilitiesReady)
	}
}

var _ CommandRunner = (*ergo.Runner)(nil)
