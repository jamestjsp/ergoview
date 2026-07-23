package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

func (m Model) viewContent() string {
	if m.width <= 0 || m.height <= 0 {
		return "Ergo View"
	}
	header := m.renderHeader()
	var body string
	if m.help {
		body = m.renderHelp()
	} else {
		switch m.view {
		case viewBoard:
			body = m.renderBoard()
		case viewDependencies:
			body = m.renderDependencies()
		default:
			if m.width < narrowBreakpoint {
				if m.focus == focusDetail {
					body = m.renderDetailPane(m.width)
				} else {
					body = m.renderOutlinePane(m.width)
				}
			} else {
				leftWidth, rightWidth := m.paneWidths()
				body = lipgloss.JoinHorizontal(
					lipgloss.Top,
					m.renderOutlinePane(leftWidth),
					m.renderDetailPane(rightWidth),
				)
			}
		}
	}
	footer := m.renderFooter()
	return m.styles.app.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

func (m Model) renderHeader() string {
	root := filepath.Base(m.snapshot.Root)
	if root == "." || root == string(filepath.Separator) {
		root = m.snapshot.Root
	}
	left := m.styles.brand.Render("ERGO VIEW") + "  " + m.styles.path.Render(root)
	labels := []string{"1 Overview", "2 Board", "3 Dependencies"}
	if m.width < narrowBreakpoint {
		labels = []string{"1 Outline", "2 Board", "3 Deps"}
	}
	tabs := m.renderTab(viewOverview, labels[0]) +
		m.renderTab(viewBoard, labels[1]) +
		m.renderTab(viewDependencies, labels[2])
	headerContentWidth := max(1, m.width-m.styles.header.GetHorizontalFrameSize())
	left = ansi.Truncate(left, max(1, headerContentWidth-lipgloss.Width(tabs)-1), "…")
	gap := max(1, headerContentWidth-lipgloss.Width(left)-lipgloss.Width(tabs))
	first := m.styles.header.Width(m.width).Render(left + strings.Repeat(" ", gap) + tabs)
	status := m.renderStatus()
	return first + "\n" + m.styles.header.Width(m.width).Render(status)
}

func (m Model) renderTab(view viewMode, label string) string {
	if m.view == view {
		return m.styles.activeTab.Render(label)
	}
	return m.styles.tab.Render(label)
}

func (m Model) renderStatus() string {
	if m.searching {
		return m.search.View()
	}
	summary := m.snapshot.Summary
	parts := []string{
		m.styles.ready.Render(fmt.Sprintf("%d ready", summary.Ready)),
		m.styles.doing.Render(fmt.Sprintf("%d doing", summary.Doing)),
		m.styles.blocked.Render(fmt.Sprintf("%d blocked", summary.Blocked)),
		m.styles.waiting.Render(fmt.Sprintf("%d waiting", summary.Waiting)),
	}
	if query := strings.TrimSpace(m.search.Value()); query != "" {
		parts = append(parts, m.styles.metadata.Render("search:"+query))
	}
	if m.filter != filterAll {
		parts = append(parts, m.styles.metadata.Render("filter:"+m.filterLabel()))
	}
	if m.containerFilter != "" {
		parts = append(parts, m.styles.metadata.Render("container:"+m.containerFilter))
	}
	if m.loadErr != nil {
		parts = append(parts, m.styles.failed.Render("reload failed: "+m.loadErr.Error()))
	} else if m.status != "" {
		parts = append(parts, m.styles.done.Render(m.status))
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderOutlinePane(width int) string {
	style := m.styles.pane
	if m.focus == focusOutline {
		style = m.styles.focusPane
	}
	innerWidth := max(1, width-style.GetHorizontalFrameSize())
	lines := []string{m.styles.paneTitle.Render("WORK")}
	paneHeight := max(1, m.contentHeight()-style.GetVerticalFrameSize())
	availableRows := max(0, paneHeight-1)
	start := 0
	if m.selected >= availableRows && availableRows > 0 {
		start = m.selected - availableRows + 1
	}
	end := min(len(m.rows), start+availableRows)
	for index := start; index < end; index++ {
		item := m.rows[index]
		task, ok := m.snapshot.Task(item.id)
		if !ok {
			continue
		}
		lines = append(lines, m.renderRow(task, item.depth, innerWidth, index == m.selected))
	}
	if len(m.rows) == 0 {
		lines = append(lines, m.styles.empty.Render("No Ergo tasks yet."))
	}
	for len(lines) < paneHeight {
		lines = append(lines, "")
	}
	return style.
		Width(width).
		Height(m.contentHeight()).
		Render(strings.Join(lines, "\n"))
}

func (m Model) renderRow(task ergo.Task, depth, width int, selected bool) string {
	symbol, label, stateStyle := m.taskPresentation(task)
	indent := strings.Repeat("  ", depth)
	prefix := indent + stateStyle.Render(symbol) + " "
	suffix := stateStyle.Render(label) + "  " + m.styles.dim.Render(task.ID)
	if task.ClaimedBy != "" {
		suffix += "  " + m.styles.doing.Render("@"+task.ClaimedBy)
	}
	titleWidth := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(suffix)-1)
	title := ansi.Truncate(task.Title, titleWidth, "…")
	line := prefix + title + strings.Repeat(" ", max(1, titleWidth-lipgloss.Width(title)+1)) + suffix
	line = ansi.Truncate(line, width, "…")
	if selected {
		return m.styles.selected.Width(width).Render(line)
	}
	if task.State == ergo.StateCanceled {
		return m.styles.canceled.Render(line)
	}
	return m.styles.row.Render(line)
}

func (m Model) taskPresentation(task ergo.Task) (string, string, lipgloss.Style) {
	switch {
	case task.Container:
		if task.Complete {
			return "◆", "DONE", m.styles.done
		}
		return "◇", fmt.Sprintf("%d/%d", completedChildren(m.snapshot, task), len(task.Children)), m.styles.brand
	case task.State == ergo.StateDoing:
		return "◐", "DOING", m.styles.doing
	case task.State == ergo.StateBlocked:
		return "!", "BLOCKED", m.styles.blocked
	case task.State == ergo.StateError:
		return "⚠", "ERROR", m.styles.failed
	case task.State == ergo.StateDone:
		return "✓", "DONE", m.styles.done
	case task.State == ergo.StateCanceled:
		return "×", "CANCELED", m.styles.canceled
	case task.Ready:
		return "○", "READY", m.styles.ready
	default:
		return "·", "WAITING", m.styles.waiting
	}
}

func completedChildren(snapshot ergo.Snapshot, task ergo.Task) int {
	count := 0
	for _, childID := range task.Children {
		child, ok := snapshot.Task(childID)
		if ok && (child.State == ergo.StateDone || child.State == ergo.StateCanceled) {
			count++
		}
	}
	return count
}

func (m Model) renderDetailPane(width int) string {
	style := m.styles.pane
	if m.focus == focusDetail {
		style = m.styles.focusPane
	}
	title := m.styles.paneTitle.Render("DETAIL")
	content := title + "\n" + m.detail.View()
	return style.
		Width(width).
		Height(m.contentHeight()).
		Render(content)
}

func (m Model) renderFooter() string {
	if m.width < narrowBreakpoint {
		items := []string{
			m.styles.footerKey.Render("j/k") + " move",
			m.styles.footerKey.Render("1/2/3") + " views",
			m.styles.footerKey.Render("/") + " search",
			m.styles.footerKey.Render("?") + " help",
		}
		return m.styles.footer.Width(m.width).Render(strings.Join(items, "  ·  "))
	}
	items := []string{
		m.styles.footerKey.Render("j/k") + " move",
		m.styles.footerKey.Render("1/2/3") + " views",
		m.styles.footerKey.Render("/") + " search",
		m.styles.footerKey.Render("f") + " filter",
		m.styles.footerKey.Render("e") + " epic",
		m.styles.footerKey.Render("x") + " clear",
		m.styles.footerKey.Render("?") + " help",
		m.styles.footerKey.Render("q") + " quit",
	}
	if m.view == viewOverview {
		items = append(items[:1], append([]string{
			m.styles.footerKey.Render("tab") + " pane",
			m.styles.footerKey.Render("enter") + " detail",
		}, items[1:]...)...)
		if m.focus == focusDetail {
			items[0] = m.styles.footerKey.Render("j/k") + " scroll"
		}
	}
	return m.styles.footer.Width(m.width).Render(strings.Join(items, "  ·  "))
}

func (m Model) renderHelp() string {
	help := []string{
		m.styles.helpTitle.Render("Ergo View keys"),
		"",
		m.styles.helpKey.Render("j / k, ↑ / ↓") + "   move or scroll",
		m.styles.helpKey.Render("home / G") + "       first / last task",
		m.styles.helpKey.Render("page up/down") + "   move one page",
		m.styles.helpKey.Render("1 / 2 / 3") + "      overview / board / dependencies",
		m.styles.helpKey.Render("/") + "              fuzzy search",
		m.styles.helpKey.Render("f") + "              cycle state filter",
		m.styles.helpKey.Render("e") + "              focus selected container",
		m.styles.helpKey.Render("x") + "              clear search and filters",
		m.styles.helpKey.Render("tab") + "            switch pane",
		m.styles.helpKey.Render("enter") + "          focus detail",
		m.styles.helpKey.Render("?") + "              toggle help",
		m.styles.helpKey.Render("q / ctrl+c") + "     quit",
		"",
		m.styles.dim.Render("Mouse selection and wheel scrolling are enabled."),
	}
	panel := m.styles.helpPanel.Render(strings.Join(help, "\n"))
	return lipgloss.Place(
		m.width,
		m.contentHeight(),
		lipgloss.Center,
		lipgloss.Center,
		panel,
	)
}
