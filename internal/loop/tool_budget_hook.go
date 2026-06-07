package loop

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// This file bridges the in-process Budget to the Claude CLI's PreToolUse hook.
// Because the CLI runs tools inside the subprocess, the only place Quasar can
// gate a tool *before* it executes is a PreToolUse hook the CLI invokes per
// tool call. Each hook fires as a separate process, so the Budget's running
// counters are persisted to a small JSON state file between calls.

// ToolHookEvent is the subset of the Claude CLI PreToolUse payload the budget
// hook reads: the name of the tool about to run.
type ToolHookEvent struct {
	ToolName string `json:"tool_name"`
}

// permissionDecision values understood by the Claude CLI PreToolUse hook.
const (
	hookDecisionAllow = "allow"
	hookDecisionDeny  = "deny"
)

// ToolHookOutput is the JSON the budget hook writes to stdout, matching the
// Claude CLI PreToolUse hook contract. A "deny" decision rejects the tool call
// (the hard cap); an "allow" decision with a reason carries a soft advisory the
// model sees on its next turn.
type ToolHookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
}

// HookSpecificOutput carries the PreToolUse decision and the reason surfaced to
// the model (an advisory or a hard-cap rejection).
type HookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// budgetSnapshot is the JSON-serializable form of a Budget, including its
// running counters, so the per-tool hook process can resume state across the
// separate processes the CLI spawns for each tool call.
type budgetSnapshot struct {
	MaxReadsBeforeEdit int  `json:"max_reads_before_edit"`
	MaxGrepsBeforeEdit int  `json:"max_greps_before_edit"`
	MaxTotalReads      int  `json:"max_total_reads"`
	SoftAdvisory       bool `json:"soft_advisory"`
	ReadsSinceEdit     int  `json:"reads_since_edit"`
	GrepsSinceEdit     int  `json:"greps_since_edit"`
	TotalReads         int  `json:"total_reads"`
	AdvisedReads       bool `json:"advised_reads"`
	AdvisedGreps       bool `json:"advised_greps"`
}

func (b *Budget) toSnapshot() budgetSnapshot {
	return budgetSnapshot{
		MaxReadsBeforeEdit: b.MaxReadsBeforeEdit,
		MaxGrepsBeforeEdit: b.MaxGrepsBeforeEdit,
		MaxTotalReads:      b.MaxTotalReads,
		SoftAdvisory:       b.SoftAdvisory,
		ReadsSinceEdit:     b.readsSinceEdit,
		GrepsSinceEdit:     b.grepsSinceEdit,
		TotalReads:         b.totalReads,
		AdvisedReads:       b.advisedReads,
		AdvisedGreps:       b.advisedGreps,
	}
}

func (s budgetSnapshot) toBudget() Budget {
	return Budget{
		MaxReadsBeforeEdit: s.MaxReadsBeforeEdit,
		MaxGrepsBeforeEdit: s.MaxGrepsBeforeEdit,
		MaxTotalReads:      s.MaxTotalReads,
		SoftAdvisory:       s.SoftAdvisory,
		readsSinceEdit:     s.ReadsSinceEdit,
		grepsSinceEdit:     s.GrepsSinceEdit,
		totalReads:         s.TotalReads,
		advisedReads:       s.AdvisedReads,
		advisedGreps:       s.AdvisedGreps,
	}
}

// WriteBudgetState writes the budget (caps + counters) to path as JSON. The
// invoker calls this once to seed the per-invocation state file before handing
// the hook to the CLI.
func WriteBudgetState(path string, b Budget) error {
	data, err := json.Marshal(b.toSnapshot())
	if err != nil {
		return fmt.Errorf("marshal budget state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write budget state %q: %w", path, err)
	}
	return nil
}

// loadBudgetState reads a budget snapshot from path. A missing or empty file
// yields the supplied defaults (the caps for this invocation), so the first
// tool call of an invocation starts from the configured budget and a lost
// state file degrades to nudging rather than spurious blocking.
func loadBudgetState(path string, defaults Budget) Budget {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return defaults
	}
	var s budgetSnapshot
	if json.Unmarshal(data, &s) != nil {
		return defaults
	}
	return s.toBudget()
}

// decideToolHook applies the budget to one tool call and renders the CLI hook
// decision. It mutates b's counters; the caller persists the updated state.
func decideToolHook(b *Budget, ev ToolHookEvent) ToolHookOutput {
	proceed, advisory := b.OnToolCall(ToolCall{Name: ev.ToolName})
	decision := hookDecisionAllow
	if !proceed {
		decision = hookDecisionDeny
	}
	return ToolHookOutput{HookSpecificOutput: HookSpecificOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       decision,
		PermissionDecisionReason: advisory,
	}}
}

// RunToolHook is the entry point invoked by the hidden budget-hook subcommand.
// It reads a PreToolUse event from in, applies the persisted budget at
// statePath (seeding from defaults on the first call of an invocation), writes
// the updated state back, and emits the CLI hook decision to out. A read/parse
// failure allows the tool (fail-open) so a hook glitch never silently blocks
// legitimate work.
func RunToolHook(statePath string, defaults Budget, in io.Reader, out io.Writer) error {
	var ev ToolHookEvent
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		// Fail open: allow the tool rather than blocking on a malformed event.
		return writeHookOutput(out, allowOutput())
	}

	b := loadBudgetState(statePath, defaults)
	decision := decideToolHook(&b, ev)
	if err := WriteBudgetState(statePath, b); err != nil {
		// Persisting failed: still allow this call so we never block on IO error.
		fmt.Fprintf(os.Stderr, "[budget-hook] %v\n", err)
	}
	return writeHookOutput(out, decision)
}

// allowOutput is the no-op decision used on the fail-open paths.
func allowOutput() ToolHookOutput {
	return ToolHookOutput{HookSpecificOutput: HookSpecificOutput{
		HookEventName:      "PreToolUse",
		PermissionDecision: hookDecisionAllow,
	}}
}

func writeHookOutput(out io.Writer, o ToolHookOutput) error {
	data, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("marshal hook output: %w", err)
	}
	if _, err := out.Write(data); err != nil {
		return fmt.Errorf("write hook output: %w", err)
	}
	return nil
}
