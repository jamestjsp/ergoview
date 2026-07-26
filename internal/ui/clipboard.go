package ui

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// osc52WarningThreshold keeps the base64-encoded control sequence near 4 KiB,
// a conservative interoperability budget rather than a terminal-specific limit.
const osc52WarningThreshold = 3 * 1024

const nativeClipboardTimeout = 2 * time.Second

var (
	errClipboardWriteTimeout = errors.New("native clipboard write timed out")
	errClipboardDegraded     = errors.New("native clipboard disabled after timeout")
)

type clipboardChannel int

const (
	clipboardNative clipboardChannel = iota
	clipboardTerminal
	clipboardFallback
)

type clipboardOutcome struct {
	channel clipboardChannel
	size    int
	err     error
}

type clipboardResultMsg struct {
	outcome clipboardOutcome
	command tea.Cmd
}

type clipboardIgnoredMsg struct{}

type clipboardDestination struct {
	writer       func(string) error
	after        func(time.Duration) <-chan time.Time
	terminalOnly bool
	writeMu      sync.Mutex
	sequence     atomic.Uint64
	degraded     bool
}

func newClipboardDestination(
	writer func(string) error,
	terminalOnly bool,
) *clipboardDestination {
	return &clipboardDestination{
		writer:       writer,
		after:        time.After,
		terminalOnly: terminalOnly,
	}
}

func systemClipboardDestination() *clipboardDestination {
	return newClipboardDestination(
		clipboard.WriteAll,
		isRemoteSession(os.Getenv),
	)
}

func (destination *clipboardDestination) copy(text string) tea.Cmd {
	sequence := destination.sequence.Add(1)
	return func() tea.Msg {
		if destination.terminalOnly {
			if !destination.isLatest(sequence) {
				return clipboardIgnoredMsg{}
			}
			return terminalClipboardResult(text, clipboardTerminal, nil)
		}
		destination.writeMu.Lock()
		defer destination.writeMu.Unlock()
		if !destination.isLatest(sequence) {
			return clipboardIgnoredMsg{}
		}
		if destination.degraded {
			return terminalClipboardResult(text, clipboardFallback, errClipboardDegraded)
		}
		result := make(chan error, 1)
		go func() {
			result <- destination.writer(text)
		}()
		var err error
		select {
		case err = <-result:
		case <-destination.after(nativeClipboardTimeout):
			// The writer cannot be canceled. Degrading permanently limits the
			// residual stale-overwrite risk to this one orphaned native write.
			destination.degraded = true
			err = errClipboardWriteTimeout
		}
		if !destination.isLatest(sequence) {
			return clipboardIgnoredMsg{}
		}
		if err != nil {
			return terminalClipboardResult(text, clipboardFallback, err)
		}
		return clipboardResultMsg{
			outcome: clipboardOutcome{
				channel: clipboardNative,
				size:    len(text),
			},
		}
	}
}

func (destination *clipboardDestination) isLatest(sequence uint64) bool {
	return destination.sequence.Load() == sequence
}

func terminalClipboardResult(
	text string,
	channel clipboardChannel,
	err error,
) clipboardResultMsg {
	return clipboardResultMsg{
		outcome: clipboardOutcome{
			channel: channel,
			size:    len(text),
			err:     err,
		},
		command: tea.SetClipboard(text),
	}
}

func isRemoteSession(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != ""
}
