package ui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
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

type viewMode int

const (
	viewOverview viewMode = iota
	viewBoard
	viewDependencies
)

type stateFilter int

const (
	filterAll stateFilter = iota
	filterActive
	filterReady
	filterWaiting
	filterDoing
	filterBlocked
	filterClosed
)

type row struct {
	id    string
	depth int
}

type copyTarget struct {
	text, label, status string
}

type SnapshotSource interface {
	LoadIfChanged() (ergo.Snapshot, bool, error)
}

type CommandRunner interface {
	Run(context.Context, io.Reader, ...string) (string, error)
}

type Options struct {
	Agent   string
	NoColor bool
	Source  SnapshotSource
	Runner  CommandRunner
}

type Model struct {
	snapshot          ergo.Snapshot
	source            SnapshotSource
	runner            CommandRunner
	rows              []row
	selected          int
	focus             focus
	view              viewMode
	filter            stateFilter
	containerFilter   string
	searching         bool
	search            textinput.Model
	actionMenu        bool
	dialog            *dialog
	pendingSelection  string
	width             int
	height            int
	dark              bool
	noColor           bool
	help              bool
	agent             string
	styles            styles
	detail            viewport.Model
	helpView          viewport.Model
	status            string
	loadErr           error
	graphFocusID      string
	graphFocusHistory []string
	graphScope        graphScope
}

func New(snapshot ergo.Snapshot, options Options) Model {
	noColor := options.NoColor || os.Getenv("NO_COLOR") != ""
	search := textinput.New()
	search.Prompt = "/ "
	search.CharLimit = 160
	model := Model{
		snapshot: snapshot,
		source:   options.Source,
		runner:   options.Runner,
		dark:     true,
		noColor:  noColor,
		agent:    options.Agent,
		styles:   newStyles(true, noColor),
		search:   search,
		detail: viewport.New(
			viewport.WithWidth(40),
			viewport.WithHeight(10),
		),
		helpView: viewport.New(
			viewport.WithWidth(40),
			viewport.WithHeight(10),
		),
		graphScope: graphScopeAdaptive,
	}
	model.detail.SoftWrap = true
	model.helpView.SoftWrap = true
	model.rebuildRows("")
	model.graphFocusID = model.selectedID()
	model.syncDetail()
	return model
}

func (m Model) Init() tea.Cmd {
	commands := []tea.Cmd{tea.RequestBackgroundColor}
	if m.source != nil {
		commands = append(commands, nextReload())
	}
	return tea.Batch(commands...)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		if m.dialog != nil {
			m.dialog.resize(m.dialogWidth())
		}
		m.resizeDetail()
		m.syncDetail()
		if m.help {
			m.syncHelpView(false)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		m.dark = message.IsDark()
		m.styles = newStyles(m.dark, m.noColor)
		m.syncSearchStyles()
		if m.dialog != nil {
			m.dialog.applyStyles(m.dark)
		}
		m.syncDetail()
		if m.help {
			m.syncHelpView(false)
		}
		return m, nil
	case reloadTickMsg:
		if m.source == nil {
			return m, nil
		}
		return m, tea.Batch(loadSnapshot(m.source), nextReload())
	case snapshotLoadedMsg:
		if message.err != nil {
			m.loadErr = message.err
			return m, nil
		}
		m.loadErr = nil
		if message.changed {
			selectedID := m.pendingSelection
			if selectedID == "" {
				selectedID = m.selectedID()
			}
			m.snapshot = message.snapshot
			m.rebuildRows(selectedID)
			m.syncDetail()
			m.pendingSelection = ""
			m.status = "Reloaded external Ergo changes"
		}
		return m, nil
	case actionResultMsg:
		updated, command := m.handleActionResult(message)
		return updated, command
	case tea.KeyPressMsg:
		m.status = ""
		if m.dialog != nil {
			updated, command := m.updateDialog(message)
			return updated, command
		}
		if m.actionMenu {
			updated, command := m.updateActionMenu(message)
			return updated, command
		}
		if m.searching {
			updated, command := m.updateSearch(message)
			return updated, command
		}
		if command := m.updateKey(message); command != nil {
			return m, command
		}
	case tea.MouseClickMsg:
		m.status = ""
		if command := m.updateMouseClick(message); command != nil {
			return m, command
		}
	case tea.MouseWheelMsg:
		if m.help {
			var command tea.Cmd
			m.helpView, command = m.helpView.Update(message)
			return m, command
		}
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
			return nil
		}
		var command tea.Cmd
		m.helpView, command = m.helpView.Update(message)
		return command
	}
	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	case "n":
		m.openDialog(actionNewTask)
		return nil
	case "p":
		m.openDialog(actionNewPlan)
		return nil
	case "a":
		if _, ok := m.selectedTask(); ok {
			m.actionMenu = true
		}
		return nil
	case "c":
		return m.copySelection()
	case "/":
		m.searching = true
		m.search.Focus()
		return nil
	case "o", "1":
		m.setView(viewOverview)
		return nil
	case "b", "2":
		m.setView(viewBoard)
		return nil
	case "g", "3":
		m.setView(viewDependencies)
		return nil
	case "f":
		m.cycleFilter()
		return nil
	case "e":
		m.filterToSelectedContainer()
		return nil
	case "x":
		m.clearFilters()
		return nil
	case "?":
		m.help = true
		m.syncHelpView(true)
		return nil
	case "tab":
		m.toggleFocus()
		return nil
	case "enter":
		if m.view == viewDependencies {
			m.focusGraphSelection()
			return nil
		}
		if m.width < narrowBreakpoint {
			m.toggleFocus()
		} else {
			m.focus = focusDetail
		}
		return nil
	}
	if m.view == viewDependencies {
		switch key {
		case "h", "left":
			m.moveGraphSelection(graphPoint{X: -1})
		case "j", "down":
			m.moveGraphSelection(graphPoint{Y: 1})
		case "k", "up":
			m.moveGraphSelection(graphPoint{Y: -1})
		case "l", "right":
			m.moveGraphSelection(graphPoint{X: 1})
		case "d":
			m.cycleGraphScope()
		case "esc":
			m.restoreGraphFocus()
		}
		return nil
	}
	if m.focus == focusOutline {
		switch key {
		case "j", "down":
			m.moveSelection(1)
		case "k", "up":
			m.moveSelection(-1)
		case "home":
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

func (m *Model) updateMouseClick(message tea.MouseClickMsg) tea.Cmd {
	if message.Button != tea.MouseLeft || m.help {
		return nil
	}
	if message.Y == m.height-1 {
		return m.updateFooterMouseClick(message)
	}
	if m.view == viewDependencies {
		m.updateGraphMouseClick(message)
		return nil
	}
	if m.view != viewOverview {
		return nil
	}
	contentY := message.Y - 2
	if contentY < 1 {
		return nil
	}
	rowIndex := contentY - 1
	if m.width >= narrowBreakpoint {
		outlineWidth, _ := m.paneWidths()
		if message.X >= outlineWidth {
			m.focus = focusDetail
			return nil
		}
		m.focus = focusOutline
	} else if m.focus == focusDetail {
		return nil
	}
	if rowIndex >= 0 && rowIndex < len(m.rows) {
		m.selectIndex(rowIndex)
	}
	return nil
}

func (m *Model) updateFooterMouseClick(message tea.MouseClickMsg) tea.Cmd {
	if message.Y != m.height-1 {
		return nil
	}
	for _, placement := range m.footerPlacements(m.footerItems()) {
		if placement.item.action == footerActionCopy &&
			message.X >= placement.start && message.X < placement.end {
			return m.copySelection()
		}
	}
	return nil
}

func (m *Model) copySelection() tea.Cmd {
	target, ok := m.selectedCopyTarget()
	if !ok {
		return nil
	}
	m.status = target.status
	return tea.SetClipboard(target.text)
}

func (m Model) selectedCopyTarget() (copyTarget, bool) {
	id := m.selectedID()
	body, ok := m.snapshot.TaskBody(id)
	if !ok {
		return copyTarget{}, false
	}
	if m.view == viewOverview && m.focus == focusDetail && strings.TrimSpace(body) != "" {
		return copyTarget{
			text:   body,
			label:  "copy body",
			status: "Copied " + id + " body to clipboard",
		}, true
	}
	return copyTarget{
		text:   id,
		label:  "copy ID",
		status: "Copied " + id + " to clipboard",
	}, true
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

func (m *Model) selectTaskID(id string) bool {
	for index, item := range m.rows {
		if item.id == id {
			m.selected = index
			m.syncDetail()
			return true
		}
	}
	return false
}

func (m Model) selectedTask() (ergo.Task, bool) {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return ergo.Task{}, false
	}
	return m.snapshot.Task(m.rows[m.selected].id)
}

func (m Model) selectedID() string {
	if len(m.rows) > 0 && m.selected >= 0 && m.selected < len(m.rows) {
		return m.rows[m.selected].id
	}
	return ""
}

func (m *Model) rebuildRows(selectedID string) {
	m.rows = m.rows[:0]
	var collect func(string, int) bool
	collect = func(id string, depth int) bool {
		task, ok := m.snapshot.Task(id)
		if !ok {
			return false
		}
		var childRows []row
		for _, childID := range task.Children {
			before := len(m.rows)
			if collect(childID, depth+1) {
				childRows = append(childRows, m.rows[before:]...)
			}
			m.rows = m.rows[:before]
		}
		if !m.taskMatches(task) && len(childRows) == 0 {
			return false
		}
		m.rows = append(m.rows, row{id: id, depth: depth})
		m.rows = append(m.rows, childRows...)
		return true
	}
	if m.containerFilter != "" {
		collect(m.containerFilter, 0)
	} else {
		for _, rootID := range m.snapshot.Roots {
			collect(rootID, 0)
		}
	}
	if m.view == viewBoard {
		m.sortRowsForBoard()
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
	if m.rowIndex(m.graphFocusID) < 0 {
		m.graphFocusID = m.selectedID()
		m.graphFocusHistory = nil
	}
}

func (m Model) taskMatches(task ergo.Task) bool {
	if !matchesQuery(task, m.search.Value()) {
		return false
	}
	if task.Container {
		return m.filter == filterAll || m.filter == filterActive
	}
	switch m.filter {
	case filterAll:
		return true
	case filterActive:
		return task.State != ergo.StateDone && task.State != ergo.StateCanceled
	case filterReady:
		return task.Ready
	case filterWaiting:
		return task.Waiting
	case filterDoing:
		return task.State == ergo.StateDoing
	case filterBlocked:
		return task.State == ergo.StateBlocked || task.State == ergo.StateError
	case filterClosed:
		return task.State == ergo.StateDone || task.State == ergo.StateCanceled
	default:
		return true
	}
}

func matchesQuery(task ergo.Task, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	value := strings.ToLower(strings.Join([]string{task.ID, task.Title, task.Body, task.ClaimedBy}, " "))
	if strings.Contains(value, query) {
		return true
	}
	queryRunes := []rune(query)
	index := 0
	for _, character := range value {
		if index < len(queryRunes) && character == queryRunes[index] {
			index++
		}
	}
	return index == len(queryRunes)
}

func (m *Model) sortRowsForBoard() {
	order := func(task ergo.Task) int {
		switch {
		case task.Container:
			return 7
		case task.Ready:
			return 0
		case task.Waiting:
			return 1
		case task.State == ergo.StateDoing:
			return 2
		case task.State == ergo.StateBlocked || task.State == ergo.StateError:
			return 3
		case task.State == ergo.StateDone:
			return 4
		case task.State == ergo.StateCanceled:
			return 5
		default:
			return 6
		}
	}
	sort.SliceStable(m.rows, func(left, right int) bool {
		leftTask, _ := m.snapshot.Task(m.rows[left].id)
		rightTask, _ := m.snapshot.Task(m.rows[right].id)
		leftOrder, rightOrder := order(leftTask), order(rightTask)
		if leftOrder == rightOrder {
			return leftTask.ID < rightTask.ID
		}
		return leftOrder < rightOrder
	})
}

func (m *Model) setView(view viewMode) {
	selectedID := m.selectedID()
	previousView := m.view
	m.view = view
	m.focus = focusOutline
	m.rebuildRows(selectedID)
	if view == viewDependencies && previousView != viewDependencies {
		m.graphFocusID = m.selectedID()
		m.graphFocusHistory = nil
	}
	m.syncDetail()
}

func (m *Model) cycleFilter() {
	selectedID := m.selectedID()
	m.filter = (m.filter + 1) % (filterClosed + 1)
	m.rebuildRows(selectedID)
	m.syncDetail()
}

func (m *Model) filterToSelectedContainer() {
	task, ok := m.selectedTask()
	if !ok {
		return
	}
	selectedID := task.ID
	switch {
	case task.Container:
		m.containerFilter = task.ID
	case task.ParentID != "":
		m.containerFilter = task.ParentID
	default:
		return
	}
	m.rebuildRows(selectedID)
	m.syncDetail()
}

func (m *Model) clearFilters() {
	selectedID := m.selectedID()
	m.filter = filterAll
	m.containerFilter = ""
	m.search.SetValue("")
	m.rebuildRows(selectedID)
	m.syncDetail()
}

func (m Model) filterLabel() string {
	switch m.filter {
	case filterActive:
		return "active"
	case filterReady:
		return "ready"
	case filterWaiting:
		return "waiting"
	case filterDoing:
		return "doing"
	case filterBlocked:
		return "blocked"
	case filterClosed:
		return "closed"
	default:
		return "all"
	}
}

func (m *Model) updateSearch(message tea.KeyPressMsg) (Model, tea.Cmd) {
	switch message.String() {
	case "esc", "enter":
		m.searching = false
		m.search.Blur()
		return *m, nil
	}
	selectedID := m.selectedID()
	var command tea.Cmd
	m.search, command = m.search.Update(message)
	m.rebuildRows(selectedID)
	m.syncDetail()
	return *m, command
}

func (m *Model) syncSearchStyles() {
	if m.dark {
		m.search.SetStyles(textinput.DefaultDarkStyles())
		return
	}
	m.search.SetStyles(textinput.DefaultLightStyles())
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

func (m *Model) syncHelpView(reset bool) {
	panelWidth := min(64, max(1, m.width))
	frameWidth := m.styles.helpPanel.GetHorizontalFrameSize()
	frameHeight := m.styles.helpPanel.GetVerticalFrameSize()
	availableHeight := max(1, m.contentHeight()-frameHeight)
	offset := m.helpView.YOffset()

	m.helpView.SetWidth(max(1, panelWidth-frameWidth))
	m.helpView.SetHeight(availableHeight)
	m.helpView.SetContent(m.helpContent())
	if m.helpView.TotalLineCount() > availableHeight && availableHeight > 1 {
		m.helpView.SetHeight(availableHeight - 1)
	}
	if reset {
		m.helpView.GotoTop()
		return
	}
	m.helpView.SetYOffset(offset)
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

type reloadTickMsg time.Time

type snapshotLoadedMsg struct {
	snapshot ergo.Snapshot
	changed  bool
	err      error
}

func nextReload() tea.Cmd {
	return tea.Tick(time.Second, func(value time.Time) tea.Msg {
		return reloadTickMsg(value)
	})
}

func loadSnapshot(source SnapshotSource) tea.Cmd {
	return func() tea.Msg {
		snapshot, changed, err := source.LoadIfChanged()
		return snapshotLoadedMsg{snapshot: snapshot, changed: changed, err: err}
	}
}
