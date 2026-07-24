package ui

import "charm.land/lipgloss/v2"

type styles struct {
	app        lipgloss.Style
	header     lipgloss.Style
	brand      lipgloss.Style
	path       lipgloss.Style
	tab        lipgloss.Style
	activeTab  lipgloss.Style
	pane       lipgloss.Style
	focusPane  lipgloss.Style
	paneTitle  lipgloss.Style
	row        lipgloss.Style
	selected   lipgloss.Style
	dim        lipgloss.Style
	ready      lipgloss.Style
	waiting    lipgloss.Style
	doing      lipgloss.Style
	blocked    lipgloss.Style
	failed     lipgloss.Style
	done       lipgloss.Style
	canceled   lipgloss.Style
	metadata   lipgloss.Style
	section    lipgloss.Style
	footer     lipgloss.Style
	footerKey  lipgloss.Style
	helpPanel  lipgloss.Style
	helpTitle  lipgloss.Style
	helpKey    lipgloss.Style
	empty      lipgloss.Style
	errorBox   lipgloss.Style
	graphEdge  lipgloss.Style
	graphNode  lipgloss.Style
	graphEpic  lipgloss.Style
	graphFocus lipgloss.Style
}

func newStyles(dark, noColor bool) styles {
	border := lipgloss.Color("#475569")
	muted := lipgloss.Color("#94A3B8")
	text := lipgloss.Color("#E2E8F0")
	surface := lipgloss.Color("#111827")
	selected := lipgloss.Color("#243447")
	brand := lipgloss.Color("#A78BFA")
	yellow := lipgloss.Color("#FBBF24")
	cyan := lipgloss.Color("#22D3EE")
	red := lipgloss.Color("#FB7185")
	green := lipgloss.Color("#34D399")
	if !dark {
		border = lipgloss.Color("#94A3B8")
		muted = lipgloss.Color("#64748B")
		text = lipgloss.Color("#172033")
		surface = lipgloss.Color("#F8FAFC")
		selected = lipgloss.Color("#E0E7FF")
		brand = lipgloss.Color("#6D28D9")
		yellow = lipgloss.Color("#A16207")
		cyan = lipgloss.Color("#0369A1")
		red = lipgloss.Color("#BE123C")
		green = lipgloss.Color("#047857")
	}
	if noColor {
		border = nil
		muted = nil
		text = nil
		surface = nil
		selected = nil
		brand = nil
		yellow = nil
		cyan = nil
		red = nil
		green = nil
	}
	return styles{
		app:       lipgloss.NewStyle().Foreground(text),
		header:    lipgloss.NewStyle().Padding(0, 1),
		brand:     lipgloss.NewStyle().Bold(true).Foreground(brand),
		path:      lipgloss.NewStyle().Foreground(muted),
		tab:       lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		activeTab: lipgloss.NewStyle().Bold(true).Foreground(brand).Padding(0, 1),
		pane: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		focusPane: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(brand).
			Padding(0, 1),
		paneTitle: lipgloss.NewStyle().Bold(true).Foreground(muted),
		row:       lipgloss.NewStyle(),
		selected:  lipgloss.NewStyle().Bold(true).Background(selected),
		dim:       lipgloss.NewStyle().Faint(true).Foreground(muted),
		ready:     lipgloss.NewStyle().Bold(true).Foreground(yellow),
		waiting:   lipgloss.NewStyle().Foreground(muted),
		doing:     lipgloss.NewStyle().Bold(true).Foreground(cyan),
		blocked:   lipgloss.NewStyle().Bold(true).Foreground(red),
		failed:    lipgloss.NewStyle().Bold(true).Foreground(red),
		done:      lipgloss.NewStyle().Foreground(green),
		canceled:  lipgloss.NewStyle().Faint(true).Foreground(muted),
		metadata:  lipgloss.NewStyle().Foreground(muted),
		section:   lipgloss.NewStyle().Bold(true).Foreground(brand).MarginTop(1),
		footer: lipgloss.NewStyle().
			Foreground(muted).
			Background(surface).
			Padding(0, 1),
		footerKey: lipgloss.NewStyle().Bold(true).Foreground(text),
		helpPanel: lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(brand).
			Padding(1, 2),
		helpTitle: lipgloss.NewStyle().Bold(true).Foreground(brand),
		helpKey:   lipgloss.NewStyle().Bold(true).Foreground(yellow),
		empty:     lipgloss.NewStyle().Italic(true).Foreground(muted),
		errorBox: lipgloss.NewStyle().
			Foreground(red).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(red).
			PaddingLeft(1),
		graphEdge: lipgloss.NewStyle().Foreground(muted),
		graphNode: lipgloss.NewStyle().Foreground(text),
		graphEpic: lipgloss.NewStyle().Bold(true).Foreground(brand),
		graphFocus: lipgloss.NewStyle().
			Bold(true).
			Foreground(text).
			Background(selected),
	}
}
