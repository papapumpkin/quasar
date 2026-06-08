package claude

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestWriteToolHookSettings(t *testing.T) {
	t.Parallel()

	b := agent.ContextBudget{
		MaxReadsBeforeEdit: 8,
		MaxGrepsBeforeEdit: 6,
		MaxTotalReads:      30,
		EnableToolHook:     true,
	}
	path, cleanup, err := writeToolHookSettings(b)
	if err != nil {
		t.Fatalf("writeToolHookSettings: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var parsed hookSettings
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("settings is not valid JSON: %v", err)
	}

	pre, ok := parsed.Hooks["PreToolUse"]
	if !ok || len(pre) != 1 || len(pre[0].Hooks) != 1 {
		t.Fatalf("expected one PreToolUse command hook, got %+v", parsed.Hooks)
	}
	cmd := pre[0].Hooks[0].Command
	for _, want := range []string{"__budget-hook", "--state", "--max-reads-before-edit 8", "--max-total-reads 30"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("hook command missing %q: %s", want, cmd)
		}
	}
	if pre[0].Matcher != toolHookMatcher {
		t.Errorf("matcher = %q, want %q", pre[0].Matcher, toolHookMatcher)
	}

	// cleanup must remove the temp directory.
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected settings file removed after cleanup, stat err = %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/usr/bin/quasar":     "'/usr/bin/quasar'",
		"/path with spaces/q": "'/path with spaces/q'",
		"/it's/weird/path":    `'/it'\''s/weird/path'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncationPolicyForUsesBudget(t *testing.T) {
	t.Parallel()

	t.Run("explicit budget overrides role default", func(t *testing.T) {
		t.Parallel()
		a := agent.Agent{Role: agent.RoleCoder, ContextBudget: &agent.ContextBudget{ToolResultMaxBytes: 4096}}
		if got := truncationPolicyFor(a).MaxBytesPerResult; got != 4096 {
			t.Errorf("MaxBytesPerResult = %d, want 4096", got)
		}
	})

	t.Run("coder default truncates; reviewer default does not", func(t *testing.T) {
		t.Parallel()
		// The coder produces a prose/diff result that stays bounded, so its
		// policy carries a positive byte cap. The reviewer produces a strict
		// JSON decision (ResultIsStructured), so truncation is disabled entirely
		// — a marker spliced into the middle would corrupt the payload.
		coder := truncationPolicyFor(agent.Agent{Role: agent.RoleCoder}).MaxBytesPerResult
		reviewer := truncationPolicyFor(agent.Agent{Role: agent.RoleReviewer}).MaxBytesPerResult
		if coder <= 0 {
			t.Errorf("expected positive coder cap, got %d", coder)
		}
		if reviewer != 0 {
			t.Errorf("expected reviewer truncation disabled (cap 0), got %d", reviewer)
		}
	})
}
