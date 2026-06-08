package constellations

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestConflictResolverStarLoadable asserts the embedded conflict-resolver star
// loads, lists the conflict-resolution-rules skill, and denies every write/git
// mutation tool — its perimeter must permit only Read/Edit and read-only Bash.
func TestConflictResolverStarLoadable(t *testing.T) {
	t.Parallel()
	star, err := artifacts.New(embeddedResolver{}).LoadStar("conflict-resolver")
	if err != nil {
		t.Fatalf("LoadStar(conflict-resolver): %v", err)
	}

	if !containsString(star.Skills, "conflict-resolution-rules") {
		t.Errorf("star skills = %v, want conflict-resolution-rules listed", star.Skills)
	}
	for _, forbidden := range []string{
		"Write", "Bash(git push *)", "Bash(git commit *)",
		"Bash(git merge *)", "Bash(git reset *)", "Bash(git checkout *)",
	} {
		if !containsString(star.Tools.Denied, forbidden) {
			t.Errorf("star tools.denied = %v, want %q denied", star.Tools.Denied, forbidden)
		}
		if containsString(star.Tools.Allowed, forbidden) {
			t.Errorf("star tools.allowed = %v, must NOT allow %q", star.Tools.Allowed, forbidden)
		}
	}
}

// containsString reports whether s is in xs.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// fixtureConflictContext builds a fully-populated conflictContext for the
// renderer tests: two workstreams with specs, diffs, and entanglements, plus a
// markers-mode collision.
func fixtureConflictContext() conflictContext {
	return conflictContext{
		Mode: conflictModeMarkers,
		A: workstream{
			Label: "A", RunID: "run-a", PhaseID: "rename-integrations-to-sensors", Title: "Rename",
			Problem:  "TicketSource is the wrong name.",
			Solution: "Introduce Sensor.",
			Diff:     "+type Sensor interface{}",
			Entanglements: []fabric.Entanglement{
				{Name: "Sensor", Kind: fabric.KindType, Status: fabric.StatusInFlight, CurrentSignature: "type Sensor interface{ Poll() }"},
				{Name: "FromTicket", Kind: fabric.KindFunction, Status: fabric.StatusDeprecated},
			},
		},
		B: workstream{
			Label: "B", RunID: "run-b", PhaseID: "github-sensor", Title: "GitHub sensor",
			Problem:  "Need a github sensor.",
			Solution: "Implement it.",
			Diff:     "+func New() *GitHub",
			Entanglements: []fabric.Entanglement{
				{Name: "Scheduler", Kind: fabric.KindType, Status: fabric.StatusInFlight, Signature: "type Scheduler struct{}"},
			},
		},
		Files:    []string{"internal/sensors/sensors.go", "internal/sensors/registry.go"},
		Worktree: "/tmp/wt",
	}
}

// TestRenderConflictContext asserts the renderer is deterministic and emits both
// workstreams' specs, diffs, and entanglements plus the markers collision block.
func TestRenderConflictContext(t *testing.T) {
	t.Parallel()
	cc := fixtureConflictContext()
	got := renderConflictContext(cc)

	if got != renderConflictContext(cc) {
		t.Fatal("renderConflictContext is not deterministic for identical input")
	}

	for _, want := range []string{
		"## Workstream A (run run-a, phase rename-integrations-to-sensors, \"Rename\")",
		"### Spec — Problem\nTicketSource is the wrong name.",
		"### Spec — Solution\nIntroduce Sensor.",
		"+type Sensor interface{}",
		"- Sensor (type, in_flight, signature=\"type Sensor interface{ Poll() }\")",
		"- FromTicket (function, deprecated)",
		"## Workstream B (run run-b, phase github-sensor, \"GitHub sensor\")",
		"- Scheduler (type, in_flight, signature=\"type Scheduler struct{}\")",
		"- Mode: markers",
		"- Conflicted files: internal/sensors/sensors.go, internal/sensors/registry.go",
		"## What you must do",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q:\n%s", want, got)
		}
	}
}

// TestRenderConflictContextNoMarkers asserts no_markers mode carries the build
// output instead of a conflicted-files list, and absent specs degrade to
// placeholders rather than failing.
func TestRenderConflictContextNoMarkers(t *testing.T) {
	t.Parallel()
	cc := conflictContext{
		Mode:        conflictModeNoMarkers,
		A:           workstream{Label: "A"},
		B:           workstream{Label: "B"},
		BuildOutput: "undefined: FromTicket",
	}
	got := renderConflictContext(cc)
	for _, want := range []string{
		"- Mode: no_markers",
		"- Build error output:",
		"undefined: FromTicket",
		"(not provided)",  // empty specs degrade
		"(none recorded)", // empty entanglements degrade
		"phase unknown",   // empty phase id degrades
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q:\n%s", want, got)
		}
	}
}

// TestOpRenderConflictContextRejectsBadMode asserts the operator rejects a
// missing or unknown mode rather than producing an ambiguous block.
func TestOpRenderConflictContextRejectsBadMode(t *testing.T) {
	t.Parallel()
	rt := &Runtime{}
	for _, mode := range []any{nil, "", "weird"} {
		_, err := opRenderConflictContext(context.Background(), rt, nil, map[string]any{"mode": mode})
		if err == nil {
			t.Errorf("mode %v: expected error, got nil", mode)
		}
	}
}

// TestOpRenderConflictContextMarkersInput drives the operator with the inputs the
// merge gate supplies today (mode + conflicted files), with no entanglement
// store wired, and asserts it produces a context block.
func TestOpRenderConflictContextMarkersInput(t *testing.T) {
	t.Parallel()
	rt := &Runtime{}
	out, err := opRenderConflictContext(context.Background(), rt, nil, map[string]any{
		"mode":  conflictModeMarkers,
		"files": []any{"internal/loop/loop.go"}, // []any mimics a TOML round-trip
	})
	if err != nil {
		t.Fatalf("opRenderConflictContext: %v", err)
	}
	ctxBlock, _ := out["context"].(string)
	if !strings.Contains(ctxBlock, "internal/loop/loop.go") {
		t.Errorf("context missing conflicted file:\n%s", ctxBlock)
	}
}

// TestOpConflictResolutionDecision covers the happy paths: a resolved+green
// verdict and a self-declared needs_human verdict both parse into the routing
// fields.
func TestOpConflictResolutionDecision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		output      string
		wantStatus  string
		wantPassed  bool
		wantChanged int
		wantReason  string
	}{
		{
			name:        "resolved and green",
			output:      `{"status":"resolved","files_changed":["a.go","b.go"],"build_passed":true,"escalation_reason":null}`,
			wantStatus:  "resolved",
			wantPassed:  true,
			wantChanged: 2,
			wantReason:  "",
		},
		{
			name:        "resolved but build still red",
			output:      `{"status":"resolved","files_changed":["a.go"],"build_passed":false,"escalation_reason":null}`,
			wantStatus:  "resolved",
			wantPassed:  false,
			wantChanged: 1,
			wantReason:  "",
		},
		{
			name:        "resolver requests human review",
			output:      `{"status":"needs_human","files_changed":[],"build_passed":false,"escalation_reason":"ambiguous multi-error build"}`,
			wantStatus:  "needs_human",
			wantPassed:  false,
			wantChanged: 0,
			wantReason:  "ambiguous multi-error build",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := opConflictResolutionDecision(context.Background(), nil, nil, map[string]any{"output": tc.output})
			if err != nil {
				t.Fatalf("opConflictResolutionDecision: %v", err)
			}
			if out["status"] != tc.wantStatus {
				t.Errorf("status = %v, want %q", out["status"], tc.wantStatus)
			}
			if out["build_passed"] != tc.wantPassed {
				t.Errorf("build_passed = %v, want %v", out["build_passed"], tc.wantPassed)
			}
			if out["files_changed"] != tc.wantChanged {
				t.Errorf("files_changed = %v, want %d", out["files_changed"], tc.wantChanged)
			}
			if out["escalation_reason"] != tc.wantReason {
				t.Errorf("escalation_reason = %v, want %q", out["escalation_reason"], tc.wantReason)
			}
		})
	}
}

// TestOpConflictResolutionDecisionEscalation asserts the universal guards
// short-circuit to needs_human BEFORE trusting the resolver: a config-file
// conflict and a delete-vs-modify on a protected path both escalate even when the
// resolver claimed resolved+green.
func TestOpConflictResolutionDecisionEscalation(t *testing.T) {
	t.Parallel()
	resolvedGreen := `{"status":"resolved","files_changed":["go.mod"],"build_passed":true,"escalation_reason":null}`
	cases := []struct {
		name       string
		args       map[string]any
		wantReason string
	}{
		{
			name:       "config file conflict",
			args:       map[string]any{"output": resolvedGreen, "files": []string{"go.mod"}},
			wantReason: "config-file conflict",
		},
		{
			name:       "config file conflict nested path",
			args:       map[string]any{"output": resolvedGreen, "files": []string{"vendor/x/package.json"}},
			wantReason: "config-file conflict",
		},
		{
			name:       "delete vs modify on protected path",
			args:       map[string]any{"output": resolvedGreen, "delete_modify": []string{"internal/loop/loop.go"}},
			wantReason: "delete-vs-modify collision",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := opConflictResolutionDecision(context.Background(), nil, nil, tc.args)
			if err != nil {
				t.Fatalf("opConflictResolutionDecision: %v", err)
			}
			if out["status"] != conflictResolverStatusHuman {
				t.Fatalf("status = %v, want needs_human", out["status"])
			}
			reason, _ := out["escalation_reason"].(string)
			if !strings.Contains(reason, tc.wantReason) {
				t.Errorf("escalation_reason = %q, want substring %q", reason, tc.wantReason)
			}
		})
	}
}

// TestOpConflictResolutionDecisionAllowsNonProtectedDelete asserts a
// delete-vs-modify outside the protected source roots does NOT short-circuit, so
// the resolver's verdict still drives routing.
func TestOpConflictResolutionDecisionAllowsNonProtectedDelete(t *testing.T) {
	t.Parallel()
	out, err := opConflictResolutionDecision(context.Background(), nil, nil, map[string]any{
		"output":        `{"status":"resolved","files_changed":["docs/x.md"],"build_passed":true,"escalation_reason":null}`,
		"delete_modify": []string{"docs/x.md"},
	})
	if err != nil {
		t.Fatalf("opConflictResolutionDecision: %v", err)
	}
	if out["status"] != conflictResolverStatusResolved {
		t.Errorf("status = %v, want resolved (non-protected delete must not escalate)", out["status"])
	}
}

// TestOpConflictResolutionDecisionErrors covers malformed input and every schema
// violation; each must produce a field-path error naming the offending field.
func TestOpConflictResolutionDecisionErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		args      map[string]any
		wantSubst string
	}{
		{
			name:      "missing input",
			args:      map[string]any{},
			wantSubst: `missing string input "output"`,
		},
		{
			name:      "invalid json",
			args:      map[string]any{"output": "not json"},
			wantSubst: "parse conflict-resolution-result-v1",
		},
		{
			name:      "unknown field",
			args:      map[string]any{"output": `{"status":"resolved","build_passed":true,"extra":1}`},
			wantSubst: "parse conflict-resolution-result-v1",
		},
		{
			name:      "missing status",
			args:      map[string]any{"output": `{"build_passed":true}`},
			wantSubst: `field "status": required`,
		},
		{
			name:      "bad status",
			args:      map[string]any{"output": `{"status":"merged","build_passed":true}`},
			wantSubst: `field "status": must be resolved or needs_human`,
		},
		{
			name:      "missing build_passed",
			args:      map[string]any{"output": `{"status":"resolved"}`},
			wantSubst: `field "build_passed": required`,
		},
		{
			name:      "needs_human without reason",
			args:      map[string]any{"output": `{"status":"needs_human","build_passed":false}`},
			wantSubst: `field "escalation_reason": required when status is needs_human`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := opConflictResolutionDecision(context.Background(), nil, nil, tc.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubst)
			}
		})
	}
}
