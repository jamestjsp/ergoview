package ergo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type commandBuilder func(context.Context, string, ...string) *exec.Cmd

type Runner struct {
	binary *binaryPath
	root   string
	agent  string
	build  commandBuilder
}

type binaryPath struct {
	name     string
	lookPath func(string) (string, error)
	once     sync.Once
	path     string
	err      error
}

func NewRunner(binary, root, agent string) *Runner {
	if strings.TrimSpace(binary) == "" {
		binary = "ergo"
	}
	return &Runner{
		binary: &binaryPath{name: binary, lookPath: exec.LookPath},
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
	binary, err := r.binary.resolve()
	if err != nil {
		return "", &CommandError{Args: append([]string(nil), args...), Err: err}
	}
	command := r.build(ctx, binary, commandArgs...)
	command.Stdin = input
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
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

func (binary *binaryPath) resolve() (string, error) {
	binary.once.Do(func() {
		if filepath.Base(binary.name) != binary.name {
			binary.path = binary.name
			return
		}
		binary.path, binary.err = binary.lookPath(binary.name)
	})
	return binary.path, binary.err
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
