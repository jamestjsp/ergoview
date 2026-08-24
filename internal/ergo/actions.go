package ergo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Capabilities struct {
	Major          int
	ModernCreation bool
	Journal        bool
	Draft          bool
	Fail           bool
	Open           bool
}

type dialectCache struct {
	mu    sync.Mutex
	known bool
	value Capabilities
}

var versionPattern = regexp.MustCompile(`(?i)\bergo\s+v?(\d+)(?:\.|\b)`)

func (r *Runner) Capabilities(ctx context.Context) (Capabilities, error) {
	r.dialect.mu.Lock()
	defer r.dialect.mu.Unlock()
	if r.dialect.known {
		return r.dialect.value, nil
	}
	capabilities, err := r.detectCapabilities(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	r.dialect.value = capabilities
	r.dialect.known = true
	return capabilities, nil
}

func (r *Runner) detectCapabilities(ctx context.Context) (Capabilities, error) {
	versionOutput, versionErr := r.runCommand(ctx, nil, "version")
	if match := versionPattern.FindStringSubmatch(versionOutput); len(match) == 2 {
		major, err := strconv.Atoi(match[1])
		if err == nil {
			return capabilitiesForMajor(major), nil
		}
	}
	help, helpErr := r.runCommand(ctx, nil, "--help")
	if helpErr != nil {
		if versionErr != nil {
			return Capabilities{}, versionErr
		}
		return Capabilities{}, helpErr
	}
	return capabilitiesFromHelp(help), nil
}

func capabilitiesForMajor(major int) Capabilities {
	capabilities := Capabilities{Major: major}
	if major >= 4 {
		capabilities.ModernCreation = true
	}
	if major >= 5 {
		capabilities.Journal = true
		capabilities.Fail = true
	}
	if major >= 6 {
		capabilities.Draft = true
		capabilities.Open = true
	}
	return capabilities
}

func capabilitiesFromHelp(help string) Capabilities {
	capabilities := Capabilities{
		ModernCreation: strings.Contains(help, `new epic "<title>"`),
		Journal:        strings.Contains(help, "result <id>"),
		Draft:          strings.Contains(help, "--draft"),
		Fail:           strings.Contains(help, "fail <id>"),
		Open:           strings.Contains(help, "open <id>"),
	}
	switch {
	case capabilities.Open:
		capabilities.Major = 6
	case capabilities.Journal:
		capabilities.Major = 5
	case capabilities.ModernCreation:
		capabilities.Major = 4
	default:
		capabilities.Major = 3
	}
	return capabilities
}

type Operation interface {
	ergoOperation()
}

type CreateTask struct {
	Title    string
	ParentID string
	Body     string
	Draft    bool
}

func (CreateTask) ergoOperation() {}

type CreateEpic struct {
	Title string
	Body  string
	Plan  string
	Draft bool
}

func (CreateEpic) ergoOperation() {}

type ClaimTask struct {
	TaskID string
	Agent  string
}

func (ClaimTask) ergoOperation() {}

type LifecycleKind string

const (
	LifecycleDone   LifecycleKind = "done"
	LifecycleFail   LifecycleKind = "fail"
	LifecycleBlock  LifecycleKind = "block"
	LifecycleCancel LifecycleKind = "cancel"
	LifecycleReturn LifecycleKind = "return"
)

type LifecycleTask struct {
	TaskID        string
	Kind          LifecycleKind
	Message       string
	ResultSummary string
	ResultPath    string
}

func (LifecycleTask) ergoOperation() {}

type RecordResult struct {
	TaskID  string
	Summary string
	Path    string
}

func (RecordResult) ergoOperation() {}

type RenameTask struct {
	TaskID string
	Title  string
}

func (RenameTask) ergoOperation() {}

type ReplaceBody struct {
	TaskID string
	Body   string
}

func (ReplaceBody) ergoOperation() {}

type MoveTask struct {
	TaskID   string
	ParentID string
}

func (MoveTask) ergoOperation() {}

type SetDependency struct {
	TaskID       string
	DependencyID string
	Remove       bool
}

func (SetDependency) ergoOperation() {}

func (r *Runner) Execute(ctx context.Context, operation Operation) (string, error) {
	capabilities, err := r.Capabilities(ctx)
	if err != nil {
		return "", err
	}
	switch operation := operation.(type) {
	case CreateTask:
		return r.createTask(ctx, capabilities, operation)
	case CreateEpic:
		return r.createEpic(ctx, capabilities, operation)
	case ClaimTask:
		agent := strings.TrimSpace(operation.Agent)
		if agent == "" {
			agent = strings.TrimSpace(r.agent)
		}
		if agent == "" {
			return "", errors.New("claim identity is required")
		}
		return r.runCommand(ctx, nil, "claim", operation.TaskID, "--agent", agent)
	case LifecycleTask:
		return r.runLifecycle(ctx, capabilities, operation)
	case RecordResult:
		return r.recordResult(ctx, capabilities, operation)
	case RenameTask:
		return r.runCommand(ctx, nil, "title", operation.TaskID, strings.TrimSpace(operation.Title))
	case ReplaceBody:
		return r.runCommand(ctx, strings.NewReader(operation.Body), "body", operation.TaskID)
	case MoveTask:
		if parent := strings.TrimSpace(operation.ParentID); parent != "" {
			return r.runCommand(ctx, nil, "move", operation.TaskID, parent)
		}
		return r.runCommand(ctx, nil, "move", operation.TaskID, "--root")
	case SetDependency:
		command := "sequence"
		if operation.Remove {
			command = "unsequence"
		}
		return r.runCommand(ctx, nil, command, strings.TrimSpace(operation.DependencyID), operation.TaskID)
	default:
		return "", fmt.Errorf("unsupported Ergo operation %T", operation)
	}
}

func (r *Runner) createTask(ctx context.Context, capabilities Capabilities, operation CreateTask) (string, error) {
	title := strings.TrimSpace(operation.Title)
	if title == "" {
		return "", errors.New("task title is required")
	}
	if operation.Draft && !capabilities.Draft {
		return "", unsupported("draft task creation", capabilities)
	}
	if !capabilities.ModernCreation {
		fields := map[string]string{"title": title}
		if parent := strings.TrimSpace(operation.ParentID); parent != "" {
			fields["epic"] = parent
		}
		payload, err := json.Marshal(fields)
		if err != nil {
			return "", err
		}
		return r.runCommand(ctx, strings.NewReader(operation.Body), "new", "task", string(payload))
	}
	args := []string{"new", "task", title}
	if parent := strings.TrimSpace(operation.ParentID); parent != "" {
		args = append(args, "--epic", parent)
	}
	if operation.Draft {
		args = append(args, "--draft")
	}
	return r.runCommand(ctx, strings.NewReader(operation.Body), args...)
}

func (r *Runner) createEpic(ctx context.Context, capabilities Capabilities, operation CreateEpic) (string, error) {
	title := strings.TrimSpace(operation.Title)
	if title == "" {
		return "", errors.New("epic title is required")
	}
	if operation.Draft && !capabilities.Draft {
		return "", unsupported("draft epic creation", capabilities)
	}
	path, cleanup, err := writePlanFile(operation.Plan)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if !capabilities.ModernCreation {
		if strings.TrimSpace(operation.Body) != "" {
			return "", unsupported("epic body context", capabilities)
		}
		payload, err := json.Marshal(map[string]string{"title": title})
		if err != nil {
			return "", err
		}
		return r.runCommand(ctx, nil, "plan", "--file", path, string(payload))
	}
	args := []string{"new", "epic", title, "--file", path}
	if operation.Draft {
		args = append(args, "--draft")
	}
	var body io.Reader
	if operation.Body != "" {
		body = strings.NewReader(operation.Body)
	}
	return r.runCommand(ctx, body, args...)
}

func (r *Runner) runLifecycle(ctx context.Context, capabilities Capabilities, operation LifecycleTask) (string, error) {
	command := string(operation.Kind)
	switch operation.Kind {
	case LifecycleDone, LifecycleBlock, LifecycleCancel:
	case LifecycleFail:
		if !capabilities.Fail {
			return "", unsupported("failed lifecycle outcomes", capabilities)
		}
	case LifecycleReturn:
		command = "release"
		if capabilities.Open {
			command = "open"
		}
	default:
		return "", fmt.Errorf("unknown lifecycle intent %q", operation.Kind)
	}
	args := []string{command, operation.TaskID}
	if message := strings.TrimSpace(operation.Message); message != "" {
		args = append(args, "-m", message)
	}
	if !capabilities.Journal && strings.TrimSpace(operation.ResultPath) != "" {
		args = append(args, "--result", strings.TrimSpace(operation.ResultPath))
	}
	if capabilities.Journal && (strings.TrimSpace(operation.ResultSummary) != "" || strings.TrimSpace(operation.ResultPath) != "") && strings.TrimSpace(operation.ResultSummary) == "" {
		return "", errors.New("result summary is required by Ergo 5 and newer")
	}
	output, err := r.runCommand(ctx, nil, args...)
	if err != nil || !capabilities.Journal || (strings.TrimSpace(operation.ResultSummary) == "" && strings.TrimSpace(operation.ResultPath) == "") {
		return output, err
	}
	resultOutput, resultErr := r.recordResult(ctx, capabilities, RecordResult{
		TaskID: operation.TaskID, Summary: operation.ResultSummary, Path: operation.ResultPath,
	})
	if resultErr != nil {
		return output, fmt.Errorf("lifecycle changed, but result recording failed: %w", resultErr)
	}
	if output == "" {
		return resultOutput, nil
	}
	if resultOutput == "" {
		return output, nil
	}
	return output + "\n" + resultOutput, nil
}

func (r *Runner) recordResult(ctx context.Context, capabilities Capabilities, operation RecordResult) (string, error) {
	if !capabilities.Journal {
		return "", unsupported("independent results", capabilities)
	}
	summary := strings.TrimSpace(operation.Summary)
	if summary == "" {
		return "", errors.New("result summary is required")
	}
	args := []string{"result", operation.TaskID, summary}
	if path := strings.TrimSpace(operation.Path); path != "" {
		args = append(args, "--file", path)
	}
	return r.runCommand(ctx, nil, args...)
}

func writePlanFile(markdown string) (string, func(), error) {
	file, err := os.CreateTemp("", "ergoview-plan-*.md")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := io.WriteString(file, markdown); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func unsupported(feature string, capabilities Capabilities) error {
	version := "this Ergo executable"
	if capabilities.Major > 0 {
		version = "Ergo " + strconv.Itoa(capabilities.Major)
	}
	return fmt.Errorf("%s does not support %s; use a newer Ergo executable", version, feature)
}
