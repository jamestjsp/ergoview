package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/jamestjsp/ergoview/internal/ergo"
	"github.com/jamestjsp/ergoview/internal/ui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ev:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("ev", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var (
		start       string
		agent       string
		showHelp    bool
		showVersion bool
	)
	flags.StringVar(&start, "dir", "", "Ergo repository, child path, or .ergo directory")
	flags.StringVar(&agent, "agent", "", "claim identity used by task actions")
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	flags.BoolVar(&showVersion, "version", false, "show version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: ev [--dir path] [--agent identity]")
	}
	if showHelp {
		printHelp()
		return nil
	}
	if showVersion {
		fmt.Println(version)
		return nil
	}
	repository, err := ergo.Open(start)
	if err != nil {
		return err
	}
	snapshot, err := repository.Load()
	if err != nil {
		return err
	}
	program := tea.NewProgram(ui.New(snapshot, ui.Options{
		Agent:  agent,
		Source: repository,
	}))
	_, err = program.Run()
	return err
}

func printHelp() {
	fmt.Print(`Ergo View — a responsive TUI companion for Ergo.

Usage:
  ev [--dir path] [--agent identity]

Flags:
  --dir path        Ergo repository, child path, or .ergo directory
  --agent identity  claim identity used by task actions
  -h, --help        show help
  --version         show version
`)
}
