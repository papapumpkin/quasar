package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/papapumpkin/quasar/internal/ui"
)

// gum color palette — matches the galactic theme.
const (
	gumColorBlueshift = "#7aa2f7"
	gumColorAccent    = "#ff9e64"
	gumColorSuccess   = "#9ece6a"
)

// buildGumHailCmd creates an *exec.Cmd that runs gum to resolve a single hail.
// It builds a shell script that:
// 1. Displays hail context via gum format (to stderr)
// 2. Collects response via gum choose or gum write (selection to stdout)
// 3. Tees stdout to a temp file so we can read it after tea.ExecProcess completes
//
// The temp file path is stored in the cmd's Env as GUM_RESPONSE_FILE for retrieval.
func (m AppModel) buildGumHailCmd(hail ui.HailInfo) *exec.Cmd {
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("quasar-gum-hail-%s", hail.ID))

	var script strings.Builder

	// Build markdown context for display.
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# ⚠ HAIL: %s\n\n", hail.Summary))
	if hail.Kind != "" {
		md.WriteString(fmt.Sprintf("**Kind:** %s\n", hail.Kind))
	}
	if hail.SourceRole != "" {
		md.WriteString(fmt.Sprintf("**From:** %s", hail.SourceRole))
		if hail.Cycle > 0 {
			md.WriteString(fmt.Sprintf(" · cycle %d", hail.Cycle))
		}
		md.WriteString("\n")
	}
	if hail.Detail != "" {
		md.WriteString("\n")
		// Strip option lines from detail (they're shown separately by gum choose).
		stripped := stripOptionLines(hail.Detail)
		if stripped != "" {
			md.WriteString(stripped)
			md.WriteString("\n")
		}
	}

	escaped := shellEscape(md.String())

	// Step 1: show context via gum format.
	script.WriteString(fmt.Sprintf("printf '%%s' '%s' | %s format --type markdown >&2\n",
		escaped, m.GumBinPath))

	// Step 2: collect response.
	if len(hail.Options) > 0 {
		script.WriteString(fmt.Sprintf("%s choose --header 'Choose a resolution:' --cursor.foreground '%s' --header.foreground '%s' --selected.foreground '%s'",
			m.GumBinPath, gumColorBlueshift, gumColorAccent, gumColorSuccess))
		for _, opt := range hail.Options {
			script.WriteString(fmt.Sprintf(" '%s'", shellEscape(opt)))
		}
		// Tee stdout to file for capture.
		script.WriteString(fmt.Sprintf(" | tee '%s'\n", tmpFile))
	} else {
		script.WriteString(fmt.Sprintf("%s write --placeholder 'Type your response (Ctrl+D to send, Esc to skip)...' --header.foreground '%s' --cursor.foreground '%s'",
			m.GumBinPath, gumColorAccent, gumColorBlueshift))
		script.WriteString(fmt.Sprintf(" | tee '%s'\n", tmpFile))
	}

	cmd := exec.Command("sh", "-c", script.String())
	// Store the temp file path in env for later retrieval.
	cmd.Env = append(os.Environ(), fmt.Sprintf("GUM_RESPONSE_FILE=%s", tmpFile))
	return cmd
}

// readGumResponseFile reads the gum response from the temp file associated
// with the given command. The file path is stored in the GUM_RESPONSE_FILE
// environment variable.
func readGumResponseFile(cmd *exec.Cmd) string {
	var tmpFile string
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "GUM_RESPONSE_FILE=") {
			tmpFile = strings.TrimPrefix(env, "GUM_RESPONSE_FILE=")
			break
		}
	}
	if tmpFile == "" {
		return ""
	}
	defer os.Remove(tmpFile) // Clean up temp file.

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// shellEscape escapes single quotes in a string for safe use in shell scripts.
func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
