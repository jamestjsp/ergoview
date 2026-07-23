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
	perGroup := max(3, height/2)
	for _, group := range groups {
		lines = append(lines, group.style.Render(fmt.Sprintf("%s  %d", group.title, len(group.tasks))))
		limit := max(1, perGroup-2)
		for index, task := range group.tasks {
			if index == limit {
				lines = append(lines, m.styles.dim.Render(fmt.Sprintf("  +%d more", len(group.tasks)-limit)))
				break
			}
			lines = append(lines, m.renderBoardTask(task, width))
		}
		if len(group.tasks) == 0 {
			lines = append(lines, m.styles.empty.Render("  none"))
		}
		for len(lines)%perGroup != 0 {
			lines = append(lines, "")
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

func (m Model) renderDependencies() string {
	style := m.styles.focusPane
	contentWidth := max(1, m.width-style.GetHorizontalFrameSize())
	contentHeight := max(1, m.contentHeight()-style.GetVerticalFrameSize())
	selected, ok := m.selectedTask()
	if !ok {
		return style.Width(m.width).Height(m.contentHeight()).Render(
			m.styles.empty.Render("No task matches the current filters."),
		)
	}
	if m.width < narrowBreakpoint {
		var lines []string
		lines = append(lines, m.renderDependencySection("PREREQUISITES", selected.Dependencies, contentWidth)...)
		lines = append(lines, "")
		lines = append(lines, m.styles.activeTab.Render("SELECTED"))
		lines = append(lines, m.renderDependencyTask(selected, contentWidth, true))
		lines = append(lines, "")
		lines = append(lines, m.renderDependencySection("UNLOCKS", selected.Dependents, contentWidth)...)
		return style.Width(m.width).Height(m.contentHeight()).Render(
			strings.Join(fitLines(lines, contentHeight, contentWidth), "\n"),
		)
	}
	columnWidth := contentWidth / 3
	left := m.renderDependencyColumn("PREREQUISITES", selected.Dependencies, columnWidth, contentHeight)
	centerLines := []string{
		m.styles.activeTab.Render("SELECTED"),
		"",
		m.renderDependencyTask(selected, columnWidth, true),
		"",
		m.styles.metadata.Render(fmt.Sprintf("%d prerequisite · %d dependent", len(selected.Dependencies), len(selected.Dependents))),
	}
	center := strings.Join(fitLines(centerLines, contentHeight, columnWidth), "\n")
	right := m.renderDependencyColumn("UNLOCKS", selected.Dependents, contentWidth-columnWidth*2, contentHeight)
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	return style.Width(m.width).Height(m.contentHeight()).Render(content)
}

func (m Model) renderDependencyColumn(title string, ids []string, width, height int) string {
	lines := m.renderDependencySection(title, ids, width)
	return strings.Join(fitLines(lines, height, width), "\n")
}

func (m Model) renderDependencySection(title string, ids []string, width int) []string {
	lines := []string{m.styles.paneTitle.Render(title)}
	if len(ids) == 0 {
		return append(lines, m.styles.empty.Render("none"))
	}
	for _, id := range ids {
		task, ok := m.snapshot.Task(id)
		if !ok {
			lines = append(lines, m.styles.failed.Render("missing  "+id))
			continue
		}
		lines = append(lines, m.renderDependencyTask(task, width, false))
	}
	return lines
}

func (m Model) renderDependencyTask(task ergo.Task, width int, selected bool) string {
	symbol, label, stateStyle := m.taskPresentation(task)
	line := fmt.Sprintf(" %s %s  %s  %s", stateStyle.Render(symbol), task.Title, stateStyle.Render(label), m.styles.dim.Render(task.ID))
	line = ansi.Truncate(line, max(1, width-1), "…")
	if selected {
		return m.styles.selected.Width(max(1, width-1)).Render(line)
	}
	return line
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
