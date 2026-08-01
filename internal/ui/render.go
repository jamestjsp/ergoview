package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type footerAction int

const footerActionCopy footerAction = 1

const footerSeparator = "  ·  "

type footerItem struct {
	content string
	action  footerAction
}

type footerPlacement struct {
	item       footerItem
	start, end int
}

type outlineLayout struct {
	style      lipgloss.Style
	innerWidth int
	paneHeight int
	start      int
	end        int
	firstRowY  int
}

func (m Model) viewContent() string {
	if m.width <= 0 || m.height <= 0 {
		return "Ergo View"
	}
	header := m.renderHeader()
	var body string
	if m.help {
		body = m.renderHelp()
	} else if m.dialog != nil {
		body = m.renderDialog()
	} else if m.actionMenu {
		body = m.renderActionMenu()
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
	body = strings.Join(
		fitLines(strings.Split(body, "\n"), m.contentHeight(), m.width),
		"\n",
	)
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
	status := ansi.Truncate(m.renderStatus(), headerContentWidth, "…")
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
	layout := m.outlineLayout(width)
	lines := []string{m.styles.paneTitle.Render("WORK")}
	for index := layout.start; index < layout.end; index++ {
		item := m.rows[index]
		task, ok := m.snapshot.Task(item.id)
		if !ok {
			continue
		}
		lines = append(lines, m.renderRow(task, item.depth, layout.innerWidth, index == m.selected))
	}
	if len(m.rows) == 0 {
		lines = append(lines, m.styles.empty.Render("No Ergo tasks yet — press n to create one."))
	}
	for len(lines) < layout.paneHeight {
		lines = append(lines, "")
	}
	return layout.style.
		Width(width).
		Height(m.contentHeight()).
		Render(strings.Join(lines, "\n"))
}

func (m Model) outlineLayout(width int) outlineLayout {
	style := m.styles.pane
	if m.focus == focusOutline {
		style = m.styles.focusPane
	}
	innerWidth := max(1, width-style.GetHorizontalFrameSize())
	paneHeight := max(1, m.contentHeight()-style.GetVerticalFrameSize())
	availableRows := max(0, paneHeight-1)
	start := 0
	if m.selected >= availableRows && availableRows > 0 {
		start = m.selected - availableRows + 1
	}
	end := min(len(m.rows), start+availableRows)
	firstRowY := m.styles.app.GetMarginTop() +
		m.styles.app.GetBorderTopSize() +
		m.styles.app.GetPaddingTop() +
		lipgloss.Height(m.renderHeader()) +
		style.GetMarginTop() +
		style.GetBorderTopSize() +
		style.GetPaddingTop() +
		lipgloss.Height(m.styles.paneTitle.Render("WORK"))
	return outlineLayout{
		style:      style,
		innerWidth: innerWidth,
		paneHeight: paneHeight,
		start:      start,
		end:        end,
		firstRowY:  firstRowY,
	}
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
		return "◇", progressLabel(completedChildren(m.snapshot, task), len(task.Children)), m.styles.brand
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

func progressLabel(done, total int) string {
	return progressBar(done, total) + " " + fmt.Sprintf("%d/%d", done, total)
}

func progressBar(done, total int) string {
	const cells = 4
	if total <= 0 {
		return strings.Repeat("▱", cells)
	}
	filled := done * cells / total
	if done > 0 && filled == 0 {
		filled = 1
	}
	if done < total && filled == cells {
		filled = cells - 1
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", cells-filled)
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
	return m.renderFooterItems(m.footerItems())
}

func (m Model) footerItem(key, label string) footerItem {
	return footerItem{content: m.styles.footerKey.Render(key) + " " + label}
}

func (m Model) copyFooterItem() footerItem {
	if m.dialog != nil || m.actionMenu || m.searching {
		return footerItem{}
	}
	label, ok := m.copyTargetLabel()
	if !ok {
		return footerItem{}
	}
	item := m.footerItem("c", label)
	item.action = footerActionCopy
	return item
}

func compactFooterItems(items []footerItem) []footerItem {
	compacted := make([]footerItem, 0, len(items))
	for _, item := range items {
		if item.content != "" {
			compacted = append(compacted, item)
		}
	}
	return compacted
}

func (m Model) footerPlacements(items []footerItem) []footerPlacement {
	width := max(1, m.width-m.styles.footer.GetHorizontalFrameSize())
	contentWidth := 0
	x := m.styles.app.GetMarginLeft() +
		m.styles.app.GetBorderLeftSize() +
		m.styles.app.GetPaddingLeft() +
		m.styles.footer.GetMarginLeft() +
		m.styles.footer.GetBorderLeftSize() +
		m.styles.footer.GetPaddingLeft()
	var placements []footerPlacement
	for _, item := range items {
		itemWidth := lipgloss.Width(item.content)
		candidateWidth := itemWidth
		start := x
		if len(placements) > 0 {
			candidateWidth += contentWidth + lipgloss.Width(footerSeparator)
			start += contentWidth + lipgloss.Width(footerSeparator)
		}
		if candidateWidth > width {
			break
		}
		placements = append(placements, footerPlacement{
			item:  item,
			start: start,
			end:   start + itemWidth,
		})
		contentWidth = candidateWidth
	}
	return placements
}

func (m Model) renderFooterItems(items []footerItem) string {
	placements := m.footerPlacements(items)
	contents := make([]string, 0, len(placements))
	for _, placement := range placements {
		contents = append(contents, placement.item.content)
	}
	content := strings.Join(contents, footerSeparator)
	if content == "" && len(items) > 0 {
		width := max(1, m.width-m.styles.footer.GetHorizontalFrameSize())
		content = ansi.Truncate(items[0].content, width, "…")
	}
	return m.styles.footer.Width(m.width).Render(content)
}

func (m Model) footerItems() []footerItem {
	if m.view == viewDependencies {
		items := []footerItem{
			m.footerItem("h/j/k/l", "node"),
			m.footerItem("enter", "focus"),
			m.footerItem("esc", "back"),
			m.footerItem("d", "depth"),
		}
		if m.width >= narrowBreakpoint {
			items = append(items, m.footerItem("/", "search"))
		}
		items = append(items,
			m.footerItem("a", "actions"),
			m.copyFooterItem(),
			m.footerItem("?", "help"),
		)
		if m.width >= narrowBreakpoint {
			items = append(items,
				m.footerItem("1/2/3", "views"),
				m.footerItem("q", "quit"),
			)
		}
		return compactFooterItems(items)
	}
	if m.width < narrowBreakpoint {
		return compactFooterItems([]footerItem{
			m.footerItem("j/k", "move"),
			m.footerItem("1/2/3", "views"),
			m.footerItem("/", "search"),
			m.footerItem("a", "actions"),
			m.copyFooterItem(),
			m.footerItem("n", "new"),
			m.footerItem("?", "help"),
		})
	}
	items := []footerItem{
		m.footerItem("j/k", "move"),
		m.footerItem("x", "clear"),
		m.footerItem("a", "actions"),
		m.copyFooterItem(),
		m.footerItem("n/p", "new"),
		m.footerItem("1/2/3", "views"),
		m.footerItem("/", "search"),
		m.footerItem("f", "filter"),
		m.footerItem("e", "epic"),
		m.footerItem("?", "help"),
		m.footerItem("q", "quit"),
	}
	if m.view == viewOverview {
		items = append(items[:1], append([]footerItem{
			m.footerItem("tab", "pane"),
			m.footerItem("enter", "detail"),
		}, items[1:]...)...)
		if m.focus == focusDetail {
			items[0] = m.footerItem("j/k", "scroll")
		}
	}
	return compactFooterItems(items)
}

func (m Model) renderHelp() string {
	panelWidth := min(64, max(1, m.width))
	panelHeight := m.helpView.Height() + m.styles.helpPanel.GetVerticalFrameSize()
	body := m.helpView.View()
	if m.helpView.TotalLineCount() > m.helpView.Height() {
		panelHeight++
		body += "\n" + m.styles.dim.Render(m.helpScrollHint())
	}
	panel := m.styles.helpPanel.
		Width(panelWidth).
		Height(min(m.contentHeight(), panelHeight)).
		Render(body)
	return lipgloss.Place(
		m.width,
		m.contentHeight(),
		lipgloss.Center,
		lipgloss.Center,
		panel,
	)
}

func (m Model) helpContent() string {
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
		m.styles.helpKey.Render("c") + "              copy selected ID and title, or focused detail",
		m.styles.helpKey.Render("a") + "              selected task actions",
		m.styles.helpKey.Render("n / p") + "          new task / container plan",
		m.styles.helpKey.Render("tab") + "            switch pane",
		m.styles.helpKey.Render("enter") + "          focus detail",
		m.styles.helpKey.Render("?") + "              toggle help",
		m.styles.helpKey.Render("q / ctrl+c") + "     quit",
	}
	if m.view == viewDependencies {
		help = append(help,
			"",
			m.styles.helpTitle.Render("Dependency graph"),
			m.styles.helpKey.Render("h / j / k / l")+"    move spatially between nodes",
			m.styles.helpKey.Render("enter / esc")+"      focus node / previous focus",
			m.styles.helpKey.Render("d")+"                direct / adaptive / lineage depth",
		)
	}
	help = append(help, "", m.styles.dim.Render("Mouse selection and wheel scrolling are enabled."))
	return strings.Join(help, "\n")
}

func (m Model) helpScrollHint() string {
	switch {
	case m.helpView.AtTop():
		return "↓ more below · j/k or wheel scroll"
	case m.helpView.AtBottom():
		return "↑ more above · j/k or wheel scroll"
	default:
		return "↕ more · j/k or wheel scroll"
	}
}
