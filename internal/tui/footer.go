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
func (f Footer) View() string {
	var parts []string
	for _, b := range f.Bindings {
		if !b.Enabled() {
			continue
		}
		help := b.Help()
		part := styleFooterKey.Render(help.Key) + styleFooterSep.Render(":") + styleFooter.Render(help.Desc)
		parts = append(parts, part)
	}
	line := strings.Join(parts, styleFooterSep.Render("  "))
	return styleFooter.Width(f.Width).Render(line)
}

// LoopFooterBindings returns footer bindings for loop mode.
func LoopFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Quit}
}

// NebulaFooterBindings returns footer bindings for nebula mode.
func NebulaFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Up, km.Down, km.Enter, km.Pause, km.Stop, km.Quit}
}

// GateFooterBindings returns footer bindings during gate prompts.
func GateFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{km.Accept, km.Reject, km.Retry, km.Skip}
}
