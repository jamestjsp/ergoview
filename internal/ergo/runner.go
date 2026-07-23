package ergo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type commandBuilder func(context.Context, string, ...string) *exec.Cmd

type Runner struct {
	binary string
	root   string
	agent  string
	build  commandBuilder
}

func NewRunner(binary, root, agent string) *Runner {
	if strings.TrimSpace(binary) == "" {
		binary = "ergo"
	}
	return &Runner{
		binary: binary,
		root:   root,
		agent:  agent,
		build:  exec.CommandContext,
	}
}

func (r *Runner) WithAgent(agent string) *Runner {
	copy := *r
	copy.agent = strings.TrimSpace(agent)
	return &copy
}

func (r *Runner) Run(ctx context.Context, input io.Reader, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("ergo command required")
	}
	commandArgs := []string{"--dir", r.root}
	if r.agent != "" {
		commandArgs = append(commandArgs, "--agent", r.agent)
	}
	commandArgs = append(commandArgs, args...)
	command := r.build(ctx, r.binary, commandArgs...)
	command.Stdin = input
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	text := strings.TrimSpace(output.String())
	if err == nil {
		return text, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return text, ctxErr
	}
	return text, &CommandError{
		Args:   append([]string(nil), args...),
		Output: text,
		Err:    err,
	}
}

type CommandError struct {
	Args   []string
	Output string
	Err    error
}

func (e *CommandError) Error() string {
	command := "ergo " + strings.Join(e.Args, " ")
	if e.Output == "" {
		return fmt.Sprintf("%s: %v", command, e.Err)
	}
	return fmt.Sprintf("%s: %s", command, e.Output)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}
