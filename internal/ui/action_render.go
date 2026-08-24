package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m Model) renderActionMenu() string {
	task, ok := m.selectedTask()
	if !ok {
		return ""
	}
	lines := []string{
		m.styles.helpTitle.Render("Task actions"),
		m.styles.brand.Render(task.Title) + "  " + m.styles.metadata.Render(task.ID),
		"",
	}
	if task.Container {
		lines = append(lines,
			m.actionLine("t", "rename"),
			m.actionLine("e", "edit body"),
			m.actionLine("s", "add dependency"),
			m.actionLine("u", "remove dependency"),
		)
	} else {
		primary := []struct {
			key, label string
			kind       actionKind
		}{
			{key: "c", label: "claim or resume", kind: actionClaim},
			{key: "d", label: "mark done", kind: actionDone},
			{key: "f", label: "mark failed", kind: actionFail},
			{key: "b", label: "mark blocked", kind: actionBlock},
			{key: "r", label: "return to todo", kind: actionReturn},
			{key: "x", label: "cancel", kind: actionCancel},
			{key: "v", label: "record result", kind: actionResult},
		}
		for _, item := range primary {
			if m.actionAvailable(task, item.kind) {
				lines = append(lines, m.actionLine(item.key, item.label))
			}
		}
		lines = append(lines,
			"",
			m.actionLine("t", "rename"),
			m.actionLine("e", "edit body"),
			m.actionLine("m", "move"),
			m.actionLine("s", "add dependency"),
			m.actionLine("u", "remove dependency"),
		)
	}
	if m.capabilitiesErr != nil {
		lines = append(lines, "", m.styles.failed.Render("Ergo capability check failed"))
	}
	lines = append(lines, "", m.styles.dim.Render("Esc closes this menu."))
	panel := m.styles.helpPanel.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) actionLine(key, label string) string {
	return m.styles.helpKey.Render(key) + "  " + label
}

func (m Model) renderDialog() string {
	current := m.dialog
	if current == nil {
		return ""
	}
	lines := []string{
		m.styles.helpTitle.Render(strings.ToUpper(string(current.kind))),
	}
	if current.targetID != "" {
		lines = append(lines, m.styles.brand.Render(current.targetTitle)+"  "+m.styles.metadata.Render(current.targetID))
	}
	lines = append(lines, "")
	switch {
	case current.busy:
		lines = append(lines, m.styles.doing.Render("Running Ergo…"))
	case current.confirm:
		lines = append(lines,
			m.styles.blocked.Render("Confirm "+string(current.kind)+"?"),
			"",
			m.styles.helpKey.Render("y / Enter")+"  confirm",
			m.styles.helpKey.Render("n / Esc")+"    cancel",
		)
	default:
		field := current.fields[current.index]
		lines = append(lines,
			m.styles.paneTitle.Render(fmt.Sprintf("%s  ·  %d of %d", field.label, current.index+1, len(current.fields))),
			"",
		)
		if field.multiline {
			lines = append(lines, current.area.View(), "", m.styles.dim.Render("Ctrl+S continues · Esc cancels"))
		} else {
			lines = append(lines, current.input.View(), "", m.styles.dim.Render("Enter continues · Esc cancels"))
		}
	}
	if current.err != "" {
		lines = append(lines, "", m.styles.errorBox.Render(current.err))
	}
	panel := m.styles.helpPanel.
		Width(m.dialogWidth()).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) dialogWidth() int {
	return min(76, max(36, m.width-8))
}
