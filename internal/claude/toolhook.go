package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// toolHookMatcher is the tool-name regex the PreToolUse hook fires on. It must
// include every tool the budget acts on: Read/Grep are counted, and the edit
// tools reset the per-edit counters (see loop.Budget.OnToolCall).
const toolHookMatcher = "Read|Grep|Edit|Write|NotebookEdit"

// hookSettings is the minimal Claude CLI settings document carrying a single
// PreToolUse hook. It is written to a temp file and passed via --settings.
type hookSettings struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// writeToolHookSettings creates a temp settings file wiring a PreToolUse hook
// that shells out to `quasar __budget-hook` with the budget caps. It returns
// the settings path and a cleanup func that removes the temp directory. The
// caps travel as flags so this package needs no dependency on the loop package
// that owns the budget logic.
func writeToolHookSettings(b agent.ContextBudget) (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("locate quasar binary: %w", err)
	}
	dir, err := os.MkdirTemp("", "quasar-toolhook-")
	if err != nil {
		return "", nil, fmt.Errorf("create tool-hook temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	statePath := filepath.Join(dir, "budget.json")
	command := fmt.Sprintf(
		"%s __budget-hook --state %s --max-reads-before-edit %d --max-greps-before-edit %d --max-total-reads %d",
		shellQuote(exe), shellQuote(statePath),
		b.MaxReadsBeforeEdit, b.MaxGrepsBeforeEdit, b.MaxTotalReads,
	)

	settings := hookSettings{Hooks: map[string][]hookMatcher{
		"PreToolUse": {{
			Matcher: toolHookMatcher,
			Hooks:   []hookCommand{{Type: "command", Command: command}},
		}},
	}}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("marshal tool-hook settings: %w", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write tool-hook settings: %w", err)
	}
	return settingsPath, cleanup, nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes, so
// a path with spaces or shell metacharacters survives the shell the CLI uses to
// run hook commands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
