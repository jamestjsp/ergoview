package ui

import (
	"errors"
	"fmt"
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

type clipboardWriteMsg struct {
	sequence    uint64
	interaction uint64
	target      copyTarget
	err         error
}

type clipboardQueue struct {
	writer   func(string) error
	after    func(time.Duration) <-chan time.Time
	writeMu  sync.Mutex
	sequence atomic.Uint64
	degraded bool
}

func writeSystemClipboard(text string) error {
	return clipboard.WriteAll(text)
}

func newClipboardQueue(writer func(string) error) *clipboardQueue {
	return &clipboardQueue{
		writer: writer,
		after:  time.After,
	}
}

func (q *clipboardQueue) request(target copyTarget, interaction uint64) tea.Cmd {
	sequence := q.sequence.Add(1)
	return func() tea.Msg {
		q.writeMu.Lock()
		defer q.writeMu.Unlock()
		if !q.isLatest(sequence) {
			return clipboardWriteMsg{
				sequence:    sequence,
				interaction: interaction,
				target:      target,
			}
		}
		if q.degraded {
			return clipboardWriteMsg{
				sequence:    sequence,
				interaction: interaction,
				target:      target,
				err:         errClipboardDegraded,
			}
		}
		result := make(chan error, 1)
		go func() {
			result <- q.writer(target.text)
		}()
		var err error
		select {
		case err = <-result:
		case <-q.after(nativeClipboardTimeout):
			// The writer cannot be canceled. Degrading permanently limits the
			// residual stale-overwrite risk to this one orphaned native write.
			q.degraded = true
			err = errClipboardWriteTimeout
		}
		return clipboardWriteMsg{
			sequence:    sequence,
			interaction: interaction,
			target:      target,
			err:         err,
		}
	}
}

func (q *clipboardQueue) isLatest(sequence uint64) bool {
	return q.sequence.Load() == sequence
}

func terminalClipboardStatus(target copyTarget, systemUnavailable bool) string {
	if len(target.text) > osc52WarningThreshold {
		size := float64(len(target.text)) / 1024
		if systemUnavailable {
			return fmt.Sprintf(
				"System clipboard unavailable; sent %.1f KB via terminal — large payloads may truncate",
				size,
			)
		}
		return fmt.Sprintf(
			"Sent %.1f KB via terminal clipboard — large payloads may truncate",
			size,
		)
	}
	if systemUnavailable {
		return "System clipboard unavailable; tried terminal clipboard"
	}
	return target.terminalStatus
}
