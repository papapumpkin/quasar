package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the TUI.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Pause    key.Binding
	Stop     key.Binding
	Retry    key.Binding
	Quit     key.Binding
	Accept   key.Binding
	Reject   key.Binding
	Skip     key.Binding
	Info     key.Binding
	Diff     key.Binding
	Beads    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Diff file list keys.
	OpenDiff key.Binding

	// Board/table view toggle.
	BoardToggle key.Binding

	// Hail list — opens the pending hails overlay.
	HailList key.Binding
}

// DefaultKeyMap returns the default keybinding configuration.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "detail"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause"),
		),
		Stop: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "stop"),
		),
		Retry: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "retry"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Accept: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "accept"),
		),
		Reject: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "reject"),
		),
		Skip: key.NewBinding(
			key.WithKeys("k"),
			key.WithHelp("k", "skip"),
		),
		Info: key.NewBinding(
			key.WithKeys("i", "?"),
			key.WithHelp("?/i", "info"),
		),
		Diff: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "diff"),
		),
		Beads: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "beads"),
			key.WithDisabled(),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("home/g", "top"),
		),
		End: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("end/G", "bottom"),
		),
		OpenDiff: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("⏎", "open diff"),
		),
		BoardToggle: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "board"),
		),
		HailList: key.NewBinding(
			key.WithKeys("H"),
			key.WithHelp("H", "hails"),
			key.WithDisabled(),
		),
	}
}

// GateKeyMap returns keybindings active during gate prompts.
// This disables navigation keys that conflict with gate actions.
func GateKeyMap() KeyMap {
	km := DefaultKeyMap()
	km.Up.SetEnabled(false)
	km.Down.SetEnabled(false)
	km.Pause.SetEnabled(false)
	km.Stop.SetEnabled(false)
	km.Retry.SetEnabled(true)
	return km
}
