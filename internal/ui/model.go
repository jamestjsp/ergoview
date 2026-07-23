package ui

import (
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

const narrowBreakpoint = 96

type focus int

const (
	focusOutline focus = iota
	focusDetail
)

type row struct {
	id    string
	depth int
}

type Options struct {
	Agent   string
	NoColor bool
}

type Model struct {
	snapshot ergo.Snapshot
	rows     []row
	selected int
	focus    focus
	width    int
	height   int
	dark     bool
	noColor  bool
	help     bool
	agent    string
	styles   styles
	detail   viewport.Model
}

func New(snapshot ergo.Snapshot, options Options) Model {
	noColor := options.NoColor || os.Getenv("NO_COLOR") != ""
	model := Model{
		snapshot: snapshot,
		dark:     true,
		noColor:  noColor,
		agent:    options.Agent,
		styles:   newStyles(true, noColor),
		detail: viewport.New(
			viewport.WithWidth(40),
			viewport.WithHeight(10),
		),
	}
	model.detail.SoftWrap = true
	model.rebuildRows("")
	model.syncDetail()
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.resizeDetail()
		m.syncDetail()
		return m, nil
	case tea.BackgroundColorMsg:
		m.dark = message.IsDark()
		m.styles = newStyles(m.dark, m.noColor)
		m.syncDetail()
		return m, nil
	case tea.KeyPressMsg:
		if command := m.updateKey(message); command != nil {
			return m, command
		}
	case tea.MouseClickMsg:
		m.updateMouseClick(message)
	case tea.MouseWheelMsg:
		if m.focus == focusDetail {
			var command tea.Cmd
			m.detail, command = m.detail.Update(message)
			return m, command
		}
		switch message.Button {
		case tea.MouseWheelUp:
			m.moveSelection(-3)
		case tea.MouseWheelDown:
			m.moveSelection(3)
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "Ergo View · " + filepath.Base(m.snapshot.Root)
	return view
}

func (m *Model) updateKey(message tea.KeyPressMsg) tea.Cmd {
	key := message.String()
	if m.help {
		switch key {
		case "?", "esc", "q", "enter":
			m.help = false
		}
		return nil
	}
	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	case "?":
		m.help = true
		return nil
	case "tab":
		m.toggleFocus()
		return nil
	case "enter":
		if m.width < narrowBreakpoint {
			m.toggleFocus()
		} else {
			m.focus = focusDetail
		}
		return nil
	}
	if m.focus == focusOutline {
		switch key {
		case "j", "down":
			m.moveSelection(1)
		case "k", "up":
			m.moveSelection(-1)
		case "g", "home":
			m.selectIndex(0)
		case "G", "end":
			m.selectIndex(len(m.rows) - 1)
		case "pgdown", "ctrl+f":
			m.moveSelection(max(1, m.contentHeight()-3))
		case "pgup", "ctrl+b":
			m.moveSelection(-max(1, m.contentHeight()-3))
		}
		return nil
	}
	var command tea.Cmd
	m.detail, command = m.detail.Update(message)
	return command
}

func (m *Model) updateMouseClick(message tea.MouseClickMsg) {
	if message.Button != tea.MouseLeft || m.help {
		return
	}
	contentY := message.Y - 2
	if contentY < 1 {
		return
	}
	rowIndex := contentY - 1
	if m.width >= narrowBreakpoint {
		outlineWidth, _ := m.paneWidths()
		if message.X >= outlineWidth {
			m.focus = focusDetail
			return
		}
		m.focus = focusOutline
	} else if m.focus == focusDetail {
		return
	}
	if rowIndex >= 0 && rowIndex < len(m.rows) {
		m.selectIndex(rowIndex)
	}
}

func (m *Model) toggleFocus() {
	if m.focus == focusOutline {
		m.focus = focusDetail
		return
	}
	m.focus = focusOutline
}

func (m *Model) moveSelection(delta int) {
	m.selectIndex(m.selected + delta)
}

func (m *Model) selectIndex(index int) {
	if len(m.rows) == 0 {
		m.selected = 0
		return
	}
	m.selected = min(max(index, 0), len(m.rows)-1)
	m.syncDetail()
}

func (m Model) selectedTask() (ergo.Task, bool) {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return ergo.Task{}, false
	}
	return m.snapshot.Task(m.rows[m.selected].id)
}

func (m *Model) rebuildRows(selectedID string) {
	m.rows = m.rows[:0]
	var add func(string, int)
	add = func(id string, depth int) {
		task, ok := m.snapshot.Task(id)
		if !ok {
			return
		}
		m.rows = append(m.rows, row{id: id, depth: depth})
		for _, childID := range task.Children {
			add(childID, depth+1)
		}
	}
	for _, rootID := range m.snapshot.Roots {
		add(rootID, 0)
	}
	m.selected = 0
	if selectedID != "" {
		for index, item := range m.rows {
			if item.id == selectedID {
				m.selected = index
				break
			}
		}
	}
}

func (m *Model) resizeDetail() {
	_, detailWidth := m.paneWidths()
	if m.width < narrowBreakpoint {
		detailWidth = m.width
	}
	frameWidth := m.styles.pane.GetHorizontalFrameSize()
	frameHeight := m.styles.pane.GetVerticalFrameSize()
	m.detail.SetWidth(max(1, detailWidth-frameWidth))
	m.detail.SetHeight(max(1, m.contentHeight()-frameHeight-1))
}

func (m Model) paneWidths() (int, int) {
	left := max(36, m.width*42/100)
	left = min(left, 58)
	right := max(1, m.width-left)
	return left, right
}

func (m Model) contentHeight() int {
	return max(1, m.height-3)
}
