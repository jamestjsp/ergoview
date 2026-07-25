package ui

import (
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

type clipboardWriteMsg struct {
	sequence uint64
	target   copyTarget
	err      error
}

type clipboardQueue struct {
	writer   func(string) error
	writeMu  sync.Mutex
	sequence atomic.Uint64
}

func writeSystemClipboard(text string) error {
	return clipboard.WriteAll(text)
}

func newClipboardQueue(writer func(string) error) *clipboardQueue {
	return &clipboardQueue{writer: writer}
}

func (q *clipboardQueue) request(target copyTarget) tea.Cmd {
	sequence := q.sequence.Add(1)
	return func() tea.Msg {
		q.writeMu.Lock()
		defer q.writeMu.Unlock()
		if !q.isLatest(sequence) {
			return clipboardWriteMsg{sequence: sequence, target: target}
		}
		return clipboardWriteMsg{
			sequence: sequence,
			target:   target,
			err:      q.writer(target.text),
		}
	}
}

func (q *clipboardQueue) isLatest(sequence uint64) bool {
	return q.sequence.Load() == sequence
}
