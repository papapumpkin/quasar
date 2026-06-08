package loop

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func decodeHookOutput(t *testing.T, raw []byte) ToolHookOutput {
	t.Helper()
	var out ToolHookOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal hook output %q: %v", raw, err)
	}
	return out
}

func TestDecideToolHook(t *testing.T) {
	t.Parallel()

	t.Run("hard cap denies the tool call", func(t *testing.T) {
		t.Parallel()
		b := Budget{MaxTotalReads: 2, SoftAdvisory: true}
		b.totalReads = 2 // already at the cap
		out := decideToolHook(&b, ToolHookEvent{ToolName: "Read"})
		if out.HookSpecificOutput.PermissionDecision != hookDecisionDeny {
			t.Errorf("expected deny at hard cap, got %q", out.HookSpecificOutput.PermissionDecision)
		}
		if !strings.Contains(out.HookSpecificOutput.PermissionDecisionReason, "hard cap") {
			t.Errorf("expected hard-cap reason, got %q", out.HookSpecificOutput.PermissionDecisionReason)
		}
	})

	t.Run("soft advisory allows with a reason", func(t *testing.T) {
		t.Parallel()
		b := Budget{MaxReadsBeforeEdit: 1, MaxTotalReads: 30, SoftAdvisory: true}
		b.readsSinceEdit = 1 // next read exceeds the soft limit
		out := decideToolHook(&b, ToolHookEvent{ToolName: "Read"})
		if out.HookSpecificOutput.PermissionDecision != hookDecisionAllow {
			t.Errorf("expected allow for soft advisory, got %q", out.HookSpecificOutput.PermissionDecision)
		}
		if !strings.Contains(out.HookSpecificOutput.PermissionDecisionReason, "system-reminder") {
			t.Errorf("expected system-reminder advisory, got %q", out.HookSpecificOutput.PermissionDecisionReason)
		}
	})

	t.Run("normal call allows with no reason", func(t *testing.T) {
		t.Parallel()
		b := DefaultBudget()
		out := decideToolHook(&b, ToolHookEvent{ToolName: "Read"})
		if out.HookSpecificOutput.PermissionDecision != hookDecisionAllow {
			t.Errorf("expected allow, got %q", out.HookSpecificOutput.PermissionDecision)
		}
		if out.HookSpecificOutput.PermissionDecisionReason != "" {
			t.Errorf("expected no reason for a normal call, got %q", out.HookSpecificOutput.PermissionDecisionReason)
		}
	})
}

func TestRunToolHookPersistsAcrossCalls(t *testing.T) {
	t.Parallel()

	state := filepath.Join(t.TempDir(), "budget.json")
	defaults := Budget{MaxReadsBeforeEdit: 8, MaxTotalReads: 30, SoftAdvisory: true}

	// Fire eight Reads — none should advise (counters persist between calls
	// via the state file, mirroring the separate hook processes the CLI runs).
	for i := 1; i <= 8; i++ {
		out := runOneHookCall(t, state, defaults, "Read")
		if out.HookSpecificOutput.PermissionDecisionReason != "" {
			t.Fatalf("read %d: unexpected advisory %q", i, out.HookSpecificOutput.PermissionDecisionReason)
		}
	}

	// The ninth Read crosses MaxReadsBeforeEdit and must advise (but allow).
	ninth := runOneHookCall(t, state, defaults, "Read")
	if ninth.HookSpecificOutput.PermissionDecision != hookDecisionAllow {
		t.Errorf("ninth read should be allowed, got %q", ninth.HookSpecificOutput.PermissionDecision)
	}
	if !strings.Contains(ninth.HookSpecificOutput.PermissionDecisionReason, "9 Reads") {
		t.Errorf("expected advisory mentioning 9 Reads, got %q", ninth.HookSpecificOutput.PermissionDecisionReason)
	}
}

func TestRunToolHookFailsOpenOnBadEvent(t *testing.T) {
	t.Parallel()
	state := filepath.Join(t.TempDir(), "budget.json")
	var out bytes.Buffer
	if err := RunToolHook(state, DefaultBudget(), strings.NewReader("not json"), &out); err != nil {
		t.Fatalf("RunToolHook returned error on bad event: %v", err)
	}
	got := decodeHookOutput(t, out.Bytes())
	if got.HookSpecificOutput.PermissionDecision != hookDecisionAllow {
		t.Errorf("expected fail-open allow on malformed event, got %q", got.HookSpecificOutput.PermissionDecision)
	}
}

func runOneHookCall(t *testing.T, state string, defaults Budget, tool string) ToolHookOutput {
	t.Helper()
	in, err := json.Marshal(ToolHookEvent{ToolName: tool})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var out bytes.Buffer
	if err := RunToolHook(state, defaults, bytes.NewReader(in), &out); err != nil {
		t.Fatalf("RunToolHook: %v", err)
	}
	return decodeHookOutput(t, out.Bytes())
}
