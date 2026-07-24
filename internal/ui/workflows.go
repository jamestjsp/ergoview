package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type boardGroup struct {
	title string
	style lipgloss.Style
	tasks []ergo.Task
}

func (m Model) renderBoard() string {
	groups := m.boardGroups()
	style := m.styles.focusPane
	contentWidth := max(1, m.width-style.GetHorizontalFrameSize())
	contentHeight := max(1, m.contentHeight()-style.GetVerticalFrameSize())
	var content string
	if m.width < narrowBreakpoint {
		var lines []string
		for _, group := range groups {
			lines = append(lines, group.style.Render(fmt.Sprintf("%s  %d", group.title, len(group.tasks))))
			for _, task := range group.tasks {
				lines = append(lines, m.renderBoardTask(task, contentWidth))
			}
			if len(group.tasks) == 0 {
				lines = append(lines, m.styles.empty.Render("  none"))
			}
			lines = append(lines, "")
		}
		content = strings.Join(fitLines(lines, contentHeight, contentWidth), "\n")
	} else {
		columnWidth := max(20, contentWidth/3)
		columns := []string{
			m.renderBoardColumn(groups[0:2], columnWidth, contentHeight),
			m.renderBoardColumn(groups[2:4], columnWidth, contentHeight),
			m.renderBoardColumn(groups[4:6], contentWidth-columnWidth*2, contentHeight),
		}
		content = lipgloss.JoinHorizontal(lipgloss.Top, columns...)
	}
	return style.Width(m.width).Height(m.contentHeight()).Render(content)
}

func (m Model) boardGroups() []boardGroup {
	groups := []boardGroup{
		{title: "READY", style: m.styles.ready},
		{title: "WAITING", style: m.styles.waiting},
		{title: "DOING", style: m.styles.doing},
		{title: "BLOCKED", style: m.styles.blocked},
		{title: "DONE", style: m.styles.done},
		{title: "CANCELED", style: m.styles.canceled},
	}
	for _, item := range m.rows {
		task, ok := m.snapshot.Task(item.id)
		if !ok || task.Container {
			continue
		}
		switch {
		case task.Ready:
			groups[0].tasks = append(groups[0].tasks, task)
		case task.Waiting:
			groups[1].tasks = append(groups[1].tasks, task)
		case task.State == ergo.StateDoing:
			groups[2].tasks = append(groups[2].tasks, task)
		case task.State == ergo.StateBlocked || task.State == ergo.StateError:
			groups[3].tasks = append(groups[3].tasks, task)
		case task.State == ergo.StateDone:
			groups[4].tasks = append(groups[4].tasks, task)
		case task.State == ergo.StateCanceled:
			groups[5].tasks = append(groups[5].tasks, task)
		}
	}
	return groups
}

func (m Model) renderBoardColumn(groups []boardGroup, width, height int) string {
	var lines []string
	for index, group := range groups {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, group.style.Render(fmt.Sprintf("%s  %d", group.title, len(group.tasks))))
		if len(group.tasks) == 0 {
			lines = append(lines, m.styles.empty.Render("  none"))
			continue
		}
		// Reserve room for each remaining group: spacer, header, one row.
		reserved := (len(groups) - index - 1) * 3
		available := max(1, height-len(lines)-reserved)
		if len(group.tasks) > available {
			shown := max(0, available-1)
			for _, task := range group.tasks[:shown] {
				lines = append(lines, m.renderBoardTask(task, width))
			}
			lines = append(lines, m.styles.dim.Render(fmt.Sprintf("  +%d more", len(group.tasks)-shown)))
			continue
		}
		for _, task := range group.tasks {
			lines = append(lines, m.renderBoardTask(task, width))
		}
	}
	return strings.Join(fitLines(lines, height, width), "\n")
}

func (m Model) renderBoardTask(task ergo.Task, width int) string {
	symbol, _, stateStyle := m.taskPresentation(task)
	text := fmt.Sprintf(" %s %s  %s", stateStyle.Render(symbol), task.Title, m.styles.dim.Render(task.ID))
	text = ansi.Truncate(text, max(1, width-1), "…")
	if selected, ok := m.selectedTask(); ok && selected.ID == task.ID {
		return m.styles.selected.Width(max(1, width-1)).Render(text)
	}
	return text
}

func fitLines(lines []string, height, width int) []string {
	if len(lines) > height {
		lines = append([]string(nil), lines[:height]...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for index, line := range lines {
		line = ansi.Truncate(line, width, "")
		lines[index] = line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	}
	return lines
}
