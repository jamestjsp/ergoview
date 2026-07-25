package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

type clipboardWriteMsg struct {
	target copyTarget
	err    error
}

func writeSystemClipboard(text string) error {
	return clipboard.WriteAll(text)
}

func writeClipboard(writer func(string) error, target copyTarget) tea.Cmd {
	return func() tea.Msg {
		return clipboardWriteMsg{
			target: target,
			err:    writer(target.text),
		}
	}
}
