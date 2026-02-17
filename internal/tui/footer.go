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
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, km.Beads, km.NewPhase, km.EditPhase, km.Pause, km.Stop, km.Quit}
}

// NebulaDetailFooterBindings returns footer bindings when drilled into a phase.
func NebulaDetailFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Info, km.Beads, km.Back, km.Quit}
}

// GateFooterBindings returns footer bindings during gate prompts.
func GateFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Accept, km.Reject, km.Retry, km.Skip}
}
