package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type graphCellRole uint8

const (
	graphRoleDefault graphCellRole = iota
	graphRoleEdge
	graphRoleMetadata
	graphRoleNode
	graphRoleEpic
	graphRoleFocus
	graphRoleReady
	graphRoleWaiting
	graphRoleDoing
	graphRoleBlocked
	graphRoleFailed
	graphRoleDone
	graphRoleCanceled
)

const (
	graphEdgeUp uint8 = 1 << iota
	graphEdgeDown
	graphEdgeLeft
	graphEdgeRight
)

type graphCanvasCell struct {
	text         string
	role         graphCellRole
	edgeMask     uint8
	arrow        string
	continuation bool
}

type graphCanvas struct {
	width  int
	height int
	cells  [][]graphCanvasCell
}

func newGraphCanvas(width, height int) *graphCanvas {
	width = max(1, width)
	height = max(1, height)
	cells := make([][]graphCanvasCell, height)
	for y := range cells {
		cells[y] = make([]graphCanvasCell, width)
		for x := range cells[y] {
			cells[y][x].text = " "
		}
	}
	return &graphCanvas{width: width, height: height, cells: cells}
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
	if contentWidth < 12 || contentHeight < 6 {
		return style.Width(m.width).Height(m.contentHeight()).Render(
			m.styles.empty.Render("Terminal too small for the dependency graph."),
		)
	}

	graphHeight := max(1, contentHeight-1)
	layout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: m.snapshot,
		FocusID:  selected.ID,
		Scope:    graphScopeAdaptive,
		Width:    contentWidth,
		Height:   graphHeight,
	})
	canvas := newGraphCanvas(contentWidth, graphHeight)
	canvas.drawDependencyEdges(layout)
	canvas.drawDependencyNodes(layout, m.snapshot)

	header := m.renderGraphHeader(layout, contentWidth)
	content := header + "\n" + canvas.render(m.styles)
	return style.Width(m.width).Height(m.contentHeight()).Render(content)
}

func (m Model) renderGraphHeader(layout dependencyGraphLayout, width int) string {
	nodeCount := 0
	hiddenCount := 0
	for _, node := range layout.Nodes {
		if node.Kind == graphTaskNode {
			nodeCount++
		} else {
			hiddenCount += len(node.HiddenIDs)
		}
	}
	flow := "left → right"
	if layout.Orientation == graphVertical {
		flow = "top ↓ bottom"
	}
	summary := fmt.Sprintf("adaptive  ·  %s  ·  %d shown", flow, nodeCount)
	if hiddenCount > 0 {
		summary += fmt.Sprintf("  ·  %d hidden", hiddenCount)
	}
	left := m.styles.paneTitle.Render("DEPENDENCY FLOW")
	right := m.styles.metadata.Render(summary)
	available := max(1, width-lipgloss.Width(left)-1)
	right = ansi.Truncate(right, available, "…")
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "")
}

func (c *graphCanvas) drawDependencyEdges(layout dependencyGraphLayout) {
	blocked := make([][]bool, c.height)
	for y := range blocked {
		blocked[y] = make([]bool, c.width)
	}
	for _, node := range layout.Nodes {
		for y := node.Rect.Y; y < node.Rect.Y+node.Rect.Height; y++ {
			for x := node.Rect.X; x < node.Rect.X+node.Rect.Width; x++ {
				if c.inBounds(x, y) {
					blocked[y][x] = true
				}
			}
		}
	}
	for _, edge := range layout.Edges {
		from, fromOK := layout.node(edge.From)
		to, toOK := layout.node(edge.To)
		if !fromOK || !toOK {
			continue
		}
		start, goal, arrow := graphEdgeEndpoints(from.Rect, to.Rect, layout.Orientation)
		path := c.findRoute(start, goal, blocked, layout.Orientation)
		if len(path) == 0 {
			continue
		}
		c.addEdgePath(path, layout.Orientation, arrow)
	}
	c.resolveEdgeGlyphs()
}

func graphEdgeEndpoints(from, to graphRect, orientation graphOrientation) (graphPoint, graphPoint, string) {
	if orientation == graphHorizontal {
		return graphPoint{X: from.X + from.Width, Y: from.Y + from.Height/2},
			graphPoint{X: to.X - 1, Y: to.Y + to.Height/2},
			"▶"
	}
	return graphPoint{X: from.X + from.Width/2, Y: from.Y + from.Height},
		graphPoint{X: to.X + to.Width/2, Y: to.Y - 1},
		"▼"
}

func (c *graphCanvas) findRoute(
	start graphPoint,
	goal graphPoint,
	blocked [][]bool,
	orientation graphOrientation,
) []graphPoint {
	if !c.inBounds(start.X, start.Y) || !c.inBounds(goal.X, goal.Y) {
		return nil
	}
	directions := []graphPoint{{X: 1}, {Y: 1}, {Y: -1}, {X: -1}}
	if orientation == graphVertical {
		directions = []graphPoint{{Y: 1}, {X: 1}, {X: -1}, {Y: -1}}
	}
	index := func(point graphPoint) int {
		return point.Y*c.width + point.X
	}
	previous := make([]int, c.width*c.height)
	for position := range previous {
		previous[position] = -2
	}
	queue := []graphPoint{start}
	previous[index(start)] = -1
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == goal {
			break
		}
		for _, direction := range directions {
			next := graphPoint{X: current.X + direction.X, Y: current.Y + direction.Y}
			if !c.inBounds(next.X, next.Y) || blocked[next.Y][next.X] {
				continue
			}
			nextIndex := index(next)
			if previous[nextIndex] != -2 {
				continue
			}
			previous[nextIndex] = index(current)
			queue = append(queue, next)
		}
	}
	if previous[index(goal)] == -2 {
		return nil
	}
	path := []graphPoint{goal}
	for current := index(goal); previous[current] >= 0; {
		current = previous[current]
		path = append(path, graphPoint{X: current % c.width, Y: current / c.width})
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func (c *graphCanvas) addEdgePath(path []graphPoint, orientation graphOrientation, arrow string) {
	for index, point := range path {
		mask := uint8(0)
		if index > 0 {
			mask |= graphDirectionMask(point, path[index-1])
		} else if orientation == graphHorizontal {
			mask |= graphEdgeLeft
		} else {
			mask |= graphEdgeUp
		}
		if index < len(path)-1 {
			mask |= graphDirectionMask(point, path[index+1])
		}
		cell := &c.cells[point.Y][point.X]
		cell.edgeMask |= mask
		cell.role = graphRoleEdge
	}
	goal := path[len(path)-1]
	c.cells[goal.Y][goal.X].arrow = arrow
}

func graphDirectionMask(from, to graphPoint) uint8 {
	switch {
	case to.X < from.X:
		return graphEdgeLeft
	case to.X > from.X:
		return graphEdgeRight
	case to.Y < from.Y:
		return graphEdgeUp
	default:
		return graphEdgeDown
	}
}

func (c *graphCanvas) resolveEdgeGlyphs() {
	for y := range c.cells {
		for x := range c.cells[y] {
			cell := &c.cells[y][x]
			if cell.arrow != "" {
				cell.text = cell.arrow
				cell.role = graphRoleEdge
				continue
			}
			if cell.edgeMask != 0 {
				cell.text = graphEdgeGlyph(cell.edgeMask)
				cell.role = graphRoleEdge
			}
		}
	}
}

func graphEdgeGlyph(mask uint8) string {
	switch mask {
	case graphEdgeUp, graphEdgeDown, graphEdgeUp | graphEdgeDown:
		return "│"
	case graphEdgeLeft, graphEdgeRight, graphEdgeLeft | graphEdgeRight:
		return "─"
	case graphEdgeDown | graphEdgeRight:
		return "╭"
	case graphEdgeDown | graphEdgeLeft:
		return "╮"
	case graphEdgeUp | graphEdgeRight:
		return "╰"
	case graphEdgeUp | graphEdgeLeft:
		return "╯"
	case graphEdgeUp | graphEdgeDown | graphEdgeRight:
		return "├"
	case graphEdgeUp | graphEdgeDown | graphEdgeLeft:
		return "┤"
	case graphEdgeLeft | graphEdgeRight | graphEdgeDown:
		return "┬"
	case graphEdgeLeft | graphEdgeRight | graphEdgeUp:
		return "┴"
	default:
		return "┼"
	}
}

func (c *graphCanvas) drawDependencyNodes(layout dependencyGraphLayout, snapshot ergo.Snapshot) {
	for _, node := range layout.Nodes {
		c.drawDependencyNode(node, node.ID == layout.FocusID, snapshot)
	}
}

func (c *graphCanvas) drawDependencyNode(node dependencyGraphNode, focused bool, snapshot ergo.Snapshot) {
	rect := node.Rect
	if rect.Width < 4 || rect.Height < 3 {
		return
	}
	if node.Kind != graphTaskNode {
		c.drawOverflowNode(node)
		return
	}

	borderRole := graphRoleNode
	textRole := graphRoleNode
	horizontal := "─"
	topLeft, topRight := "╭", "╮"
	bottomLeft, bottomRight := "╰", "╯"
	vertical := "│"
	if node.Task.Container {
		borderRole = graphRoleEpic
		textRole = graphRoleEpic
		horizontal = "═"
		topLeft, topRight = "╔", "╗"
		bottomLeft, bottomRight = "╚", "╝"
		vertical = "║"
	}
	if focused {
		borderRole = graphRoleFocus
		textRole = graphRoleFocus
		if !node.Task.Container {
			horizontal = "━"
			topLeft, topRight = "┏", "┓"
			bottomLeft, bottomRight = "┗", "┛"
			vertical = "┃"
		}
		c.fillRect(rect, " ", graphRoleFocus)
	}

	c.drawBox(rect, topLeft, topRight, bottomLeft, bottomRight, horizontal, vertical, borderRole)
	symbol, label, _ := Model{snapshot: snapshot}.taskPresentation(node.Task)
	stateRole := graphRoleForTask(node.Task)
	if focused {
		stateRole = graphRoleFocus
	}
	c.drawText(rect.X+1, rect.Y, symbol, stateRole, 1)
	c.drawText(rect.X+2, rect.Y, " "+node.Task.Title, textRole, rect.Width-3)

	x := rect.X + 2
	c.drawText(x, rect.Y+1, node.Task.ID, textRole, rect.Width-3)
	x += lipgloss.Width(node.Task.ID) + 1
	if node.Task.Container {
		progress := fmt.Sprintf("EPIC %d/%d", completedChildren(snapshot, node.Task), len(node.Task.Children))
		c.drawText(x, rect.Y+1, progress, textRole, rect.X+rect.Width-1-x)
		return
	}
	c.drawText(x, rect.Y+1, label, stateRole, rect.X+rect.Width-1-x)
	x += lipgloss.Width(label) + 1
	if node.Task.ParentID != "" {
		parent := "◇" + node.Task.ParentID
		role := graphRoleEpic
		if focused {
			role = graphRoleFocus
		}
		c.drawText(x, rect.Y+1, parent, role, rect.X+rect.Width-1-x)
	}
}

func (c *graphCanvas) drawOverflowNode(node dependencyGraphNode) {
	rect := node.Rect
	c.drawBox(rect, "╭", "╮", "╰", "╯", "┄", "┆", graphRoleMetadata)
	direction := "UPSTREAM"
	if node.Kind == graphDownstreamOverflow {
		direction = "DOWNSTREAM"
	}
	title := fmt.Sprintf("… +%d %s", len(node.HiddenIDs), direction)
	c.drawText(rect.X+1, rect.Y, title, graphRoleMetadata, rect.Width-2)
	c.drawText(rect.X+2, rect.Y+1, "more context", graphRoleMetadata, rect.Width-3)
}

func (c *graphCanvas) drawBox(
	rect graphRect,
	topLeft string,
	topRight string,
	bottomLeft string,
	bottomRight string,
	horizontal string,
	vertical string,
	role graphCellRole,
) {
	c.put(rect.X, rect.Y, topLeft, role)
	c.put(rect.X+rect.Width-1, rect.Y, topRight, role)
	c.put(rect.X, rect.Y+rect.Height-1, bottomLeft, role)
	c.put(rect.X+rect.Width-1, rect.Y+rect.Height-1, bottomRight, role)
	for x := rect.X + 1; x < rect.X+rect.Width-1; x++ {
		c.put(x, rect.Y, horizontal, role)
		c.put(x, rect.Y+rect.Height-1, horizontal, role)
	}
	for y := rect.Y + 1; y < rect.Y+rect.Height-1; y++ {
		c.put(rect.X, y, vertical, role)
		c.put(rect.X+rect.Width-1, y, vertical, role)
	}
}

func (c *graphCanvas) fillRect(rect graphRect, text string, role graphCellRole) {
	for y := rect.Y; y < rect.Y+rect.Height; y++ {
		for x := rect.X; x < rect.X+rect.Width; x++ {
			c.put(x, y, text, role)
		}
	}
}

func (c *graphCanvas) drawText(x, y int, text string, role graphCellRole, maxWidth int) {
	if maxWidth <= 0 || !c.inBounds(x, y) {
		return
	}
	text = ansi.Truncate(text, maxWidth, "…")
	currentX := x
	for _, character := range text {
		value := string(character)
		width := lipgloss.Width(value)
		if width == 0 {
			if currentX > x {
				c.cells[y][currentX-1].text += value
			}
			continue
		}
		if currentX+width > x+maxWidth || currentX+width > c.width {
			break
		}
		c.put(currentX, y, value, role)
		for offset := 1; offset < width; offset++ {
			cell := &c.cells[y][currentX+offset]
			cell.text = ""
			cell.role = role
			cell.continuation = true
		}
		currentX += width
	}
}

func (c *graphCanvas) put(x, y int, text string, role graphCellRole) {
	if !c.inBounds(x, y) {
		return
	}
	c.cells[y][x] = graphCanvasCell{text: text, role: role}
}

func (c *graphCanvas) inBounds(x, y int) bool {
	return x >= 0 && x < c.width && y >= 0 && y < c.height
}

func (c *graphCanvas) render(theme styles) string {
	lines := make([]string, c.height)
	for y, row := range c.cells {
		var line strings.Builder
		for start := 0; start < len(row); {
			role := row[start].role
			end := start
			var segment strings.Builder
			for end < len(row) && row[end].role == role {
				if !row[end].continuation {
					segment.WriteString(row[end].text)
				}
				end++
			}
			line.WriteString(graphRoleStyle(theme, role).Render(segment.String()))
			start = end
		}
		lines[y] = line.String()
	}
	return strings.Join(lines, "\n")
}

func graphRoleStyle(theme styles, role graphCellRole) lipgloss.Style {
	switch role {
	case graphRoleEdge:
		return theme.graphEdge
	case graphRoleMetadata:
		return theme.metadata
	case graphRoleNode:
		return theme.graphNode
	case graphRoleEpic:
		return theme.graphEpic
	case graphRoleFocus:
		return theme.graphFocus
	case graphRoleReady:
		return theme.ready
	case graphRoleWaiting:
		return theme.waiting
	case graphRoleDoing:
		return theme.doing
	case graphRoleBlocked:
		return theme.blocked
	case graphRoleFailed:
		return theme.failed
	case graphRoleDone:
		return theme.done
	case graphRoleCanceled:
		return theme.canceled
	default:
		return theme.app
	}
}

func graphRoleForTask(task ergo.Task) graphCellRole {
	switch {
	case task.Container:
		return graphRoleEpic
	case task.State == ergo.StateDoing:
		return graphRoleDoing
	case task.State == ergo.StateBlocked:
		return graphRoleBlocked
	case task.State == ergo.StateError:
		return graphRoleFailed
	case task.State == ergo.StateDone:
		return graphRoleDone
	case task.State == ergo.StateCanceled:
		return graphRoleCanceled
	case task.Ready:
		return graphRoleReady
	default:
		return graphRoleWaiting
	}
}
