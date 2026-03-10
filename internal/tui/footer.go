package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// Footer renders context-sensitive keybinding hints.
type Footer struct {
	Width    int
	Bindings []key.Binding
}

// View renders the footer as a single line of keybinding hints.
// In compact mode (narrow terminals), shows only key hints without descriptions.
func (f Footer) View() string {
	compact := f.Width < CompactWidth

	var parts []string
	for _, b := range f.Bindings {
		if !b.Enabled() {
			continue
		}
		help := b.Help()
		var part string
		if compact {
			// Compact: key only, no description.
			part = styleFooterKey.Render(help.Key)
		} else {
			part = styleFooterKey.Render(help.Key) + styleFooterSep.Render(":") + styleFooterDesc.Render(help.Desc)
		}
		parts = append(parts, part)
	}
	sep := styleFooterSep.Render("  ")
	if compact {
		sep = styleFooterSep.Render(" ")
	}
	line := strings.Join(parts, sep)
	return styleFooter.Width(f.Width).Render(line)
}

// LoopFooterBindings returns footer bindings for loop mode.
func LoopFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Quit}
}

// NebulaFooterBindings returns footer bindings for nebula mode.
func NebulaFooterBindings(km KeyMap) []key.Binding {
	boardToggle := km.BoardToggle
	boardToggle.SetHelp("v", "board")
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, boardToggle, km.Pause, km.Stop, km.Quit}
}

// NebulaDetailFooterBindings returns footer bindings when drilled into a phase.
func NebulaDetailFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, km.Back, km.Quit}
}

// DiffFileListFooterBindings returns footer bindings when the diff file list is active.
// The OpenDiff binding is always enabled because diffs are rendered inline.
func DiffFileListFooterBindings(km KeyMap) []key.Binding {
	diffToggle := km.Diff
	diffToggle.SetHelp("d", "close")
	return []key.Binding{km.Up, km.Down, km.OpenDiff, km.SideBySide, diffToggle, km.Quit}
}

// DiffFileOpenFooterBindings returns footer bindings when viewing a single file's diff.
func DiffFileOpenFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.ToggleHunk, km.SideBySide, km.Back, km.Quit}
}

// HomeFooterBindings returns footer bindings for home mode.
func HomeFooterBindings(km KeyMap) []key.Binding {
	enter := key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "run"),
	)
	filter := key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "filter"),
	)
	return []key.Binding{km.Up, km.Down, enter, filter, km.Info, km.Quit}
}

// CockpitFooterBindings returns footer bindings when the board view is active.
// Shows tab navigation, board toggle, and cockpit-specific actions.
func CockpitFooterBindings(km KeyMap) []key.Binding {
	tab := key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "tabs"),
	)
	boardToggle := km.BoardToggle
	boardToggle.SetHelp("v", "table")
	return []key.Binding{km.Up, km.Down, km.Enter, tab, boardToggle, km.Info, km.Pause, km.Stop, km.Quit}
}

// SidebarFooterBindings returns footer bindings when the sidebar has focus.
// Includes tree-specific shortcuts (C/D/=/z) for the expandable tree view.
func SidebarFooterBindings(km KeyMap) []key.Binding {
	focus := key.NewBinding(
		key.WithKeys("right", "tab"),
		key.WithHelp("→/tab", "main"),
	)
	return []key.Binding{
		km.Up, km.Down, km.Enter, focus,
		km.TreeSortCost, km.TreeSortDuration,
		km.TreeCollapse, km.TreeZoom,
		km.Quit,
	}
}

// GateFooterBindings returns footer bindings during gate prompts.
// Includes Esc (back/skip) so users know they can dismiss the prompt.
func GateFooterBindings(km KeyMap) []key.Binding {
	esc := km.Back
	esc.SetHelp("esc", "skip")
	return []key.Binding{km.Accept, km.Reject, km.Retry, km.Skip, esc}
}

// HailOverlayFooterBindings returns footer bindings when the hail dialog overlay is active.
func HailOverlayFooterBindings(km KeyMap) []key.Binding {
	enter := key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "add"),
	)
	submit := key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "submit"),
	)
	tab := key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "scroll"),
	)
	esc := km.Back
	esc.SetHelp("esc", "dismiss")
	return []key.Binding{enter, submit, tab, esc}
}

// DialogFooterBindings returns footer bindings for the interactive dialog overlay.
func DialogFooterBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "close")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "scroll")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	}
}

// HailListFooterBindings returns footer bindings when the hail list overlay is active.
func HailListFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Back}
}
