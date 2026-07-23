package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type actionKind string

const (
	actionNewTask    actionKind = "new task"
	actionNewPlan    actionKind = "new container plan"
	actionClaim      actionKind = "claim"
	actionDone       actionKind = "done"
	actionBlock      actionKind = "block"
	actionCancel     actionKind = "cancel"
	actionRelease    actionKind = "release"
	actionRename     actionKind = "rename"
	actionBody       actionKind = "edit body"
	actionMove       actionKind = "move"
	actionSequence   actionKind = "add dependency"
	actionUnsequence actionKind = "remove dependency"
)

type formField struct {
	key         string
	label       string
	value       string
	placeholder string
	required    bool
	multiline   bool
}

type dialog struct {
	kind           actionKind
	targetID       string
	targetTitle    string
	fields         []formField
	index          int
	input          textinput.Model
	area           textarea.Model
	confirm        bool
	requireConfirm bool
	busy           bool
	err            string
}

type actionResultMsg struct {
	kind     actionKind
	targetID string
	output   string
	err      error
}

func (m *Model) openDialog(kind actionKind) {
	task, hasTask := m.selectedTask()
	if kind != actionNewTask && kind != actionNewPlan && !hasTask {
		return
	}
	current := &dialog{kind: kind}
	if hasTask {
		current.targetID = task.ID
		current.targetTitle = task.Title
	}
	switch kind {
	case actionNewTask:
		parent := ""
		if hasTask {
			if task.Container {
				parent = task.ID
			} else {
				parent = task.ParentID
			}
		}
		current.fields = []formField{
			{key: "title", label: "Task title", placeholder: "Add password login", required: true},
			{key: "parent", label: "Container ID (optional)", value: parent, placeholder: "ABC123"},
			{key: "body", label: "Markdown body (optional)", placeholder: "## Goal", multiline: true},
		}
	case actionNewPlan:
		current.fields = []formField{
			{key: "title", label: "Container title", placeholder: "Authentication", required: true},
			{
				key:         "plan",
				label:       "Plan Markdown (Ctrl+S to submit)",
				placeholder: "# Schema\nCreate tables.\n---\n# Endpoints\nAdd login.",
				required:    true,
				multiline:   true,
			},
		}
	case actionClaim:
		if strings.TrimSpace(m.agent) == "" {
			current.fields = []formField{
				{key: "agent", label: "Claim identity", placeholder: "model@hostname", required: true},
			}
		}
		current.requireConfirm = task.State == ergo.StateDone || task.State == ergo.StateCanceled
	case actionDone, actionBlock, actionCancel, actionRelease:
		current.fields = []formField{
			{key: "message", label: "Lifecycle message (optional, Ctrl+S to continue)", multiline: true},
			{key: "result", label: "Result path (optional)", placeholder: "docs/verification.md"},
		}
		current.requireConfirm = kind == actionCancel
	case actionRename:
		current.fields = []formField{
			{key: "title", label: "Task title", value: task.Title, required: true},
		}
	case actionBody:
		current.fields = []formField{
			{key: "body", label: "Markdown body (Ctrl+S to save)", value: task.Body, multiline: true},
		}
	case actionMove:
		current.fields = []formField{
			{key: "parent", label: "Container ID (blank moves to root)", value: task.ParentID, placeholder: "ABC123"},
		}
	case actionSequence, actionUnsequence:
		current.fields = []formField{
			{key: "dependency", label: "Dependency ID", placeholder: "ABC123", required: true},
		}
	}
	current.input = textinput.New()
	current.area = textarea.New()
	current.area.SetHeight(8)
	current.area.CharLimit = 32 * 1024
	current.applyStyles(m.dark)
	current.resize(m.dialogWidth())
	if len(current.fields) == 0 {
		current.confirm = true
	} else {
		current.focusField()
	}
	m.dialog = current
	m.actionMenu = false
}

func (d *dialog) applyStyles(dark bool) {
	if dark {
		d.input.SetStyles(textinput.DefaultDarkStyles())
		d.area.SetStyles(textarea.DefaultDarkStyles())
		return
	}
	d.input.SetStyles(textinput.DefaultLightStyles())
	d.area.SetStyles(textarea.DefaultLightStyles())
}

func (d *dialog) resize(width int) {
	d.input.SetWidth(max(20, width-8))
	d.area.SetWidth(max(20, width-8))
}

func (d *dialog) focusField() {
	if d.index < 0 || d.index >= len(d.fields) {
		return
	}
	field := d.fields[d.index]
	d.input.Blur()
	d.area.Blur()
	if field.multiline {
		d.area.SetValue(field.value)
		d.area.Placeholder = field.placeholder
		d.area.Focus()
		return
	}
	d.input.SetValue(field.value)
	d.input.Placeholder = field.placeholder
	d.input.Focus()
}

func (m *Model) updateActionMenu(message tea.KeyPressMsg) (Model, tea.Cmd) {
	key := message.String()
	if key == "ctrl+c" {
		return *m, tea.Quit
	}
	if key == "esc" || key == "a" || key == "q" {
		m.actionMenu = false
		return *m, nil
	}
	task, ok := m.selectedTask()
	if !ok {
		m.actionMenu = false
		return *m, nil
	}
	var kind actionKind
	switch key {
	case "c":
		kind = actionClaim
	case "d":
		kind = actionDone
	case "b":
		kind = actionBlock
	case "x":
		kind = actionCancel
	case "r":
		kind = actionRelease
	case "t":
		kind = actionRename
	case "e":
		kind = actionBody
	case "m":
		kind = actionMove
	case "s":
		kind = actionSequence
	case "u":
		kind = actionUnsequence
	default:
		return *m, nil
	}
	if task.Container {
		switch kind {
		case actionRename, actionBody, actionSequence, actionUnsequence:
		default:
			m.status = "Containers support title, body, and dependency actions only"
			m.actionMenu = false
			return *m, nil
		}
	}
	m.openDialog(kind)
	return *m, nil
}

func (m *Model) updateDialog(message tea.KeyPressMsg) (Model, tea.Cmd) {
	current := m.dialog
	if current == nil {
		return *m, nil
	}
	key := message.String()
	if key == "ctrl+c" {
		return *m, tea.Quit
	}
	if current.busy {
		return *m, nil
	}
	if current.confirm {
		switch key {
		case "y", "enter":
			return *m, m.beginAction()
		case "n", "esc", "q":
			m.dialog = nil
			return *m, nil
		}
		return *m, nil
	}
	if key == "esc" {
		m.dialog = nil
		return *m, nil
	}
	field := current.fields[current.index]
	if (!field.multiline && (key == "enter" || key == "tab")) ||
		(field.multiline && key == "ctrl+s") {
		return *m, m.advanceDialog()
	}
	var command tea.Cmd
	if field.multiline {
		current.area, command = current.area.Update(message)
	} else {
		current.input, command = current.input.Update(message)
	}
	return *m, command
}

func (m *Model) advanceDialog() tea.Cmd {
	current := m.dialog
	field := &current.fields[current.index]
	if field.multiline {
		field.value = current.area.Value()
	} else {
		field.value = current.input.Value()
	}
	if field.required && strings.TrimSpace(field.value) == "" {
		current.err = field.label + " is required"
		return nil
	}
	if field.key == "plan" && !strings.Contains(field.value, "# ") {
		current.err = "Plan Markdown must contain at least one # Title"
		return nil
	}
	current.err = ""
	if current.index+1 < len(current.fields) {
		current.index++
		current.focusField()
		return nil
	}
	if current.requireConfirm {
		current.confirm = true
		current.input.Blur()
		current.area.Blur()
		return nil
	}
	return m.beginAction()
}

func (m *Model) beginAction() tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	if m.runner == nil {
		m.dialog.err = "Ergo command runner is unavailable"
		return nil
	}
	m.dialog.busy = true
	m.dialog.err = ""
	request := actionRequest{
		kind:     m.dialog.kind,
		targetID: m.dialog.targetID,
		agent:    m.agent,
		values:   map[string]string{},
	}
	for _, field := range m.dialog.fields {
		request.values[field.key] = field.value
	}
	return runAction(m.runner, request)
}

type actionRequest struct {
	kind     actionKind
	targetID string
	agent    string
	values   map[string]string
}

func runAction(runner CommandRunner, request actionRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := executeAction(ctx, runner, request)
		return actionResultMsg{
			kind:     request.kind,
			targetID: request.targetID,
			output:   output,
			err:      err,
		}
	}
}

func executeAction(ctx context.Context, runner CommandRunner, request actionRequest) (string, error) {
	value := func(key string) string {
		return request.values[key]
	}
	switch request.kind {
	case actionNewTask:
		fields := map[string]string{"title": strings.TrimSpace(value("title"))}
		if parent := strings.TrimSpace(value("parent")); parent != "" {
			fields["epic"] = parent
		}
		payload, err := json.Marshal(fields)
		if err != nil {
			return "", err
		}
		return runner.Run(ctx, strings.NewReader(value("body")), "new", "task", string(payload))
	case actionNewPlan:
		return runPlan(ctx, runner, strings.TrimSpace(value("title")), value("plan"))
	case actionClaim:
		agent := strings.TrimSpace(value("agent"))
		if agent == "" {
			agent = strings.TrimSpace(request.agent)
		}
		if agent == "" {
			return "", errors.New("claim identity is required")
		}
		return runner.Run(ctx, nil, "claim", request.targetID, "--agent", agent)
	case actionDone, actionBlock, actionCancel, actionRelease:
		args := []string{string(request.kind), request.targetID}
		if message := strings.TrimSpace(value("message")); message != "" {
			args = append(args, "-m", message)
		}
		if result := strings.TrimSpace(value("result")); result != "" {
			args = append(args, "--result", result)
		}
		return runner.Run(ctx, nil, args...)
	case actionRename:
		return runner.Run(ctx, nil, "title", request.targetID, strings.TrimSpace(value("title")))
	case actionBody:
		return runner.Run(ctx, strings.NewReader(value("body")), "body", request.targetID)
	case actionMove:
		if parent := strings.TrimSpace(value("parent")); parent != "" {
			return runner.Run(ctx, nil, "move", request.targetID, parent)
		}
		return runner.Run(ctx, nil, "move", request.targetID, "--root")
	case actionSequence:
		return runner.Run(ctx, nil, "sequence", strings.TrimSpace(value("dependency")), request.targetID)
	case actionUnsequence:
		return runner.Run(ctx, nil, "unsequence", strings.TrimSpace(value("dependency")), request.targetID)
	default:
		return "", fmt.Errorf("unsupported action %q", request.kind)
	}
}

func runPlan(ctx context.Context, runner CommandRunner, title, markdown string) (string, error) {
	file, err := os.CreateTemp("", "ergoview-plan-*.md")
	if err != nil {
		return "", err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := io.WriteString(file, markdown); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]string{"title": title})
	if err != nil {
		return "", err
	}
	return runner.Run(ctx, nil, "plan", "--file", path, string(payload))
}

func (m *Model) handleActionResult(message actionResultMsg) (Model, tea.Cmd) {
	if message.err != nil {
		if m.dialog != nil {
			m.dialog.busy = false
			m.dialog.err = message.err.Error()
		}
		return *m, nil
	}
	selectedID := message.targetID
	if message.kind == actionNewTask || message.kind == actionNewPlan {
		if fields := strings.Fields(message.output); len(fields) > 0 {
			selectedID = fields[0]
		}
	}
	m.pendingSelection = selectedID
	m.dialog = nil
	m.actionMenu = false
	m.status = message.output
	if m.status == "" {
		m.status = string(message.kind) + " complete"
	}
	if m.source == nil {
		return *m, nil
	}
	return *m, loadSnapshot(m.source)
}
