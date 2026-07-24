package ui

import tea "charm.land/bubbletea/v2"

func (m Model) graphLayoutForView() dependencyGraphLayout {
	width, height := m.graphCanvasSize()
	focusID := m.graphFocusID
	if _, ok := m.snapshot.Task(focusID); !ok {
		focusID = m.selectedID()
	}
	layout := buildDependencyGraphLayout(dependencyGraphRequest{
		Snapshot: m.snapshot,
		FocusID:  focusID,
		Scope:    m.graphScope,
		Width:    width,
		Height:   height,
	})
	centerID := m.selectedID()
	centerNode, ok := layout.node(centerID)
	if !ok {
		centerNode, ok = layout.node(focusID)
	}
	if !ok {
		return layout
	}
	center := centerNode.Rect.center()
	offsetX := 0
	offsetY := 0
	if layout.Width > width {
		offsetX = min(max(0, center.X-width/2), layout.Width-width)
	}
	if layout.Height > height {
		offsetY = min(max(0, center.Y-height/2), layout.Height-height)
	}
	if offsetX != 0 || offsetY != 0 {
		for index := range layout.Nodes {
			layout.Nodes[index].Rect.X -= offsetX
			layout.Nodes[index].Rect.Y -= offsetY
		}
	}
	layout.Width = width
	layout.Height = height
	return layout
}

func (m Model) graphCanvasSize() (int, int) {
	style := m.styles.focusPane
	width := max(1, m.width-style.GetHorizontalFrameSize())
	height := max(1, m.contentHeight()-style.GetVerticalFrameSize()-1)
	return width, height
}

func (m *Model) moveGraphSelection(direction graphPoint) {
	layout := m.graphLayoutForView()
	selected, ok := layout.node(m.selectedID())
	if !ok {
		if m.selectTaskID(layout.FocusID) {
			return
		}
		return
	}
	origin := selected.Rect.center()
	bestID := ""
	bestScore := int(^uint(0) >> 1)
	overflowCandidate := false
	overflowScore := int(^uint(0) >> 1)
	for _, candidate := range layout.Nodes {
		if candidate.ID == selected.ID || !graphRectVisible(candidate.Rect, layout.Width, layout.Height) {
			continue
		}
		target := candidate.Rect.center()
		delta := graphPoint{X: target.X - origin.X, Y: target.Y - origin.Y}
		primary, secondary, inDirection := graphDirectionalDistance(delta, direction)
		if !inDirection {
			continue
		}
		score := primary*100 + secondary*secondary
		if candidate.Kind != graphTaskNode {
			if score < overflowScore {
				overflowScore = score
				overflowCandidate = true
			}
			continue
		}
		if m.rowIndex(candidate.ID) < 0 {
			continue
		}
		if score < bestScore || (score == bestScore && candidate.ID < bestID) {
			bestID = candidate.ID
			bestScore = score
		}
	}
	if bestID != "" {
		m.selectTaskID(bestID)
		return
	}
	if overflowCandidate {
		m.expandGraphScope()
	}
}

func graphDirectionalDistance(delta, direction graphPoint) (int, int, bool) {
	switch {
	case direction.X < 0:
		return -delta.X, absInt(delta.Y), delta.X < 0
	case direction.X > 0:
		return delta.X, absInt(delta.Y), delta.X > 0
	case direction.Y < 0:
		return -delta.Y, absInt(delta.X), delta.Y < 0
	default:
		return delta.Y, absInt(delta.X), delta.Y > 0
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func graphRectVisible(rect graphRect, width, height int) bool {
	return rect.X < width && rect.Y < height && rect.X+rect.Width > 0 && rect.Y+rect.Height > 0
}

func (m *Model) focusGraphSelection() {
	selectedID := m.selectedID()
	if selectedID == "" || selectedID == m.graphFocusID {
		return
	}
	if m.graphFocusID != "" {
		m.graphFocusHistory = append(m.graphFocusHistory, m.graphFocusID)
	}
	m.graphFocusID = selectedID
	m.status = "Focused dependency graph on " + selectedID
}

func (m *Model) restoreGraphFocus() {
	if len(m.graphFocusHistory) == 0 {
		return
	}
	index := len(m.graphFocusHistory) - 1
	previous := m.graphFocusHistory[index]
	m.graphFocusHistory = m.graphFocusHistory[:index]
	m.graphFocusID = previous
	m.selectTaskID(previous)
	m.status = "Returned graph focus to " + previous
}

func (m *Model) cycleGraphScope() {
	switch m.graphScope {
	case graphScopeDirect:
		m.graphScope = graphScopeAdaptive
	case graphScopeAdaptive:
		m.graphScope = graphScopeLineage
	default:
		m.graphScope = graphScopeDirect
	}
	m.status = "Graph depth: " + m.graphScope.label()
}

func (m *Model) expandGraphScope() {
	switch m.graphScope {
	case graphScopeDirect:
		m.graphScope = graphScopeAdaptive
	case graphScopeAdaptive:
		m.graphScope = graphScopeLineage
	default:
		return
	}
	m.status = "Expanded graph depth to " + m.graphScope.label()
}

func (m *Model) updateGraphMouseClick(message tea.MouseClickMsg) {
	style := m.styles.focusPane
	x := message.X - style.GetHorizontalFrameSize()/2
	y := message.Y - 2 - style.GetVerticalFrameSize()/2 - 1
	layout := m.graphLayoutForView()
	for _, node := range layout.Nodes {
		if !node.Rect.contains(x, y) {
			continue
		}
		if node.Kind == graphTaskNode {
			if !m.selectTaskID(node.ID) {
				m.status = "Task " + node.ID + " is outside the current filters"
			}
			return
		}
		m.expandGraphScope()
		return
	}
}

func (m Model) rowIndex(id string) int {
	for index, item := range m.rows {
		if item.id == id {
			return index
		}
	}
	return -1
}

func (s graphScope) label() string {
	switch s {
	case graphScopeDirect:
		return "direct"
	case graphScopeLineage:
		return "lineage"
	default:
		return "adaptive"
	}
}
