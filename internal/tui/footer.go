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
	return []key.Binding{km.Up, km.Down, km.Enter, km.Beads, km.Quit}
}

// NebulaFooterBindings returns footer bindings for nebula mode.
func NebulaFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, km.Beads, km.Pause, km.Stop, km.Quit}
}

// NebulaDetailFooterBindings returns footer bindings when drilled into a phase.
func NebulaDetailFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, km.Beads, km.Back, km.Quit}
}

// DiffFileListFooterBindings returns footer bindings when the diff file list is active.
// The OpenDiff binding is always enabled because diffs are rendered inline.
func DiffFileListFooterBindings(km KeyMap) []key.Binding {
	diffToggle := km.Diff
	diffToggle.SetHelp("d", "close")
	return []key.Binding{km.Up, km.Down, km.OpenDiff, diffToggle, km.Quit}
}

// GateFooterBindings returns footer bindings during gate prompts.
// Includes Esc (back/skip) so users know they can dismiss the prompt.
func GateFooterBindings(km KeyMap) []key.Binding {
	esc := km.Back
	esc.SetHelp("esc", "skip")
	return []key.Binding{km.Accept, km.Reject, km.Retry, km.Skip, esc}
}
