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

type clipboardRequestID uint64

type clipboardRequest struct {
	id      clipboardRequestID
	command tea.Cmd
}

type clipboardResultMsg struct {
	request clipboardRequestID
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

func (destination *clipboardDestination) copy(text string) clipboardRequest {
	request := clipboardRequestID(destination.sequence.Add(1))
	return clipboardRequest{
		id:      request,
		command: destination.deliveryCommand(request, text),
	}
}

func (destination *clipboardDestination) deliveryCommand(
	request clipboardRequestID,
	text string,
) tea.Cmd {
	return func() tea.Msg {
		if destination.terminalOnly {
			if !destination.isLatest(request) {
				return clipboardIgnoredMsg{}
			}
			return terminalClipboardResult(request, text, clipboardTerminal, nil)
		}
		destination.writeMu.Lock()
		defer destination.writeMu.Unlock()
		if !destination.isLatest(request) {
			return clipboardIgnoredMsg{}
		}
		if destination.degraded {
			return terminalClipboardResult(
				request,
				text,
				clipboardFallback,
				errClipboardDegraded,
			)
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
		if !destination.isLatest(request) {
			return clipboardIgnoredMsg{}
		}
		if err != nil {
			return terminalClipboardResult(request, text, clipboardFallback, err)
		}
		return clipboardResultMsg{
			request: request,
			outcome: clipboardOutcome{
				channel: clipboardNative,
				size:    len(text),
			},
		}
	}
}

func (destination *clipboardDestination) isLatest(request clipboardRequestID) bool {
	return destination.sequence.Load() == uint64(request)
}

func terminalClipboardResult(
	request clipboardRequestID,
	text string,
	channel clipboardChannel,
	err error,
) clipboardResultMsg {
	return clipboardResultMsg{
		request: request,
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
