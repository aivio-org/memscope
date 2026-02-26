package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all key bindings for the MemScope TUI.
type KeyMap struct {
	// Navigation
	FocusNext     key.Binding
	FocusPrev     key.Binding
	ScrollUp      key.Binding
	ScrollDown    key.Binding
	ScrollPageUp  key.Binding
	ScrollPageDown key.Binding
	ScrollTop     key.Binding
	ScrollBottom  key.Binding

	// Actions
	Filter        key.Binding // F2: open / close filter bar
	ToggleFreeze  key.Binding // Space: freeze/unfreeze live updates
	ClearEvents   key.Binding // c: clear event log
	ResetZoom     key.Binding // r: reset timeline zoom
	ZoomIn        key.Binding // +: zoom in timeline
	ZoomOut       key.Binding // -: zoom out timeline
	Expand        key.Binding // Enter: expand node (pointer graph, Phase 3)
	ToggleDiff    key.Binding // d: toggle diff mode

	// Export / Help
	ExportJSON    key.Binding // F4: export JSON snapshot
	Help          key.Binding // F1: toggle help overlay
	ShowPanels    key.Binding // F3: cycle panel layout

	// Quit
	Quit          key.Binding
}

// DefaultKeyMap returns the standard key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		FocusNext: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		FocusPrev: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev panel"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		ScrollPageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		ScrollPageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		ScrollTop: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("home/g", "scroll to top"),
		),
		ScrollBottom: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("end/G", "scroll to bottom"),
		),
		Filter: key.NewBinding(
			key.WithKeys("f2"),
			key.WithHelp("F2", "filter"),
		),
		ToggleFreeze: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "freeze/unfreeze"),
		),
		ClearEvents: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clear log"),
		),
		ResetZoom: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reset zoom"),
		),
		ZoomIn: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "zoom in"),
		),
		ZoomOut: key.NewBinding(
			key.WithKeys("-"),
			key.WithHelp("-", "zoom out"),
		),
		Expand: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "expand"),
		),
		ToggleDiff: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "diff mode"),
		),
		ExportJSON: key.NewBinding(
			key.WithKeys("f4"),
			key.WithHelp("F4", "export JSON"),
		),
		Help: key.NewBinding(
			key.WithKeys("f1", "?"),
			key.WithHelp("F1/?", "help"),
		),
		ShowPanels: key.NewBinding(
			key.WithKeys("f3"),
			key.WithHelp("F3", "panels"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}
