package fabric

import (
	"strings"
	"testing"
	"time"
)

func TestRenderSnapshot_SummaryLine(t *testing.T) {
	t.Parallel()

	t.Run("all counts reflected", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Completed:  []string{"a", "b", "c"},
			InProgress: []string{"d", "e"},
			Blocked:    []string{"f"},
			Entanglements: []Entanglement{
				{Producer: "a", Kind: KindInterface, Name: "Foo", Package: "pkg"},
			},
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryFileConflict, Detail: "conflict", SourceTask: "d"},
				{Kind: DiscoveryBudgetAlert, Detail: "budget", SourceTask: "e"},
			},
		}
		got := RenderSnapshot(snap)
		want := "## Fabric State (3 completed, 2 in-progress, 1 blocked, 1 entanglements, 2 unresolved)"
		if !strings.Contains(got, want) {
			t.Errorf("summary line mismatch\ngot:  %s\nwant substring: %s", firstLine(got), want)
		}
	})

	t.Run("zero counts", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{}
		got := RenderSnapshot(snap)
		want := "## Fabric State (0 completed, 0 in-progress, 0 blocked, 0 entanglements, 0 unresolved)"
		if !strings.Contains(got, want) {
			t.Errorf("summary line mismatch\ngot:  %s\nwant substring: %s", firstLine(got), want)
		}
	})
}

func TestRenderSnapshot_EntanglementFileLocations(t *testing.T) {
	t.Parallel()

	t.Run("file path appended when present", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Entanglements: []Entanglement{
				{
					Producer:  "phase-a",
					Kind:      KindInterface,
					Name:      "Fabric",
					Signature: "Fabric(path string) error",
					Package:   "internal/fabric",
					File:      "internal/fabric/fabric.go",
				},
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "(internal/fabric/fabric.go)") {
			t.Error("expected file path in entanglement rendering")
		}
		if !strings.Contains(got, "Fabric(path string) error") {
			t.Error("expected signature in entanglement rendering")
		}
	})

	t.Run("no file path when empty", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Entanglements: []Entanglement{
				{
					Producer: "phase-a",
					Kind:     KindType,
					Name:     "User",
					Package:  "models",
				},
			},
		}
		got := RenderSnapshot(snap)
		if strings.Contains(got, "()") {
			t.Error("should not render empty parens when file is empty")
		}
		if !strings.Contains(got, "- type User\n") {
			t.Error("expected simple rendering without file path")
		}
	})

	t.Run("name used when signature is empty", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Entanglements: []Entanglement{
				{
					Producer: "phase-a",
					Kind:     KindFunction,
					Name:     "NewFabric",
					Package:  "pkg",
					File:     "pkg/factory.go",
				},
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "function NewFabric (pkg/factory.go)") {
			t.Errorf("expected name with file path, got:\n%s", got)
		}
	})
}

func TestRenderSnapshot_FileClaimContext(t *testing.T) {
	t.Parallel()

	t.Run("with state and cycle info", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			FileClaims: map[string]string{
				"internal/loop/loop.go": "phase-context",
			},
			PhaseStates: map[string]string{
				"phase-context": "in_progress",
			},
			PhaseCycles: map[string][2]int{
				"phase-context": {2, 5},
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "phase-context (in_progress, cycle 2/5)") {
			t.Errorf("expected enriched claim context, got:\n%s", got)
		}
	})

	t.Run("with state only", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			FileClaims: map[string]string{
				"file.go": "phase-x",
			},
			PhaseStates: map[string]string{
				"phase-x": "running",
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "phase-x (running)") {
			t.Errorf("expected state-only context, got:\n%s", got)
		}
	})

	t.Run("with cycle only", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			FileClaims: map[string]string{
				"file.go": "phase-y",
			},
			PhaseCycles: map[string][2]int{
				"phase-y": {1, 3},
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "phase-y (cycle 1/3)") {
			t.Errorf("expected cycle-only context, got:\n%s", got)
		}
	})

	t.Run("plain claim when no enrichment data", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			FileClaims: map[string]string{
				"file.go": "phase-z",
			},
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "file.go → phase-z\n") {
			t.Errorf("expected plain claim, got:\n%s", got)
		}
	})

	t.Run("no enrichment for unknown owner", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			FileClaims: map[string]string{
				"file.go": "phase-unknown",
			},
			PhaseStates: map[string]string{
				"phase-other": "running",
			},
		}
		got := RenderSnapshot(snap)
		// Should not have parens since the owner isn't in the state map.
		if strings.Contains(got, "phase-unknown (") {
			t.Errorf("should not show enrichment for unknown owner, got:\n%s", got)
		}
	})
}

func TestRenderSnapshot_DiscoveryTimestamps(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return fixedNow }

	t.Run("just now", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryFileConflict, Detail: "conflict", SourceTask: "phase-a",
					CreatedAt: fixedNow.Add(-30 * time.Second)},
			},
			Now: nowFn,
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "just now") {
			t.Errorf("expected 'just now', got:\n%s", got)
		}
	})

	t.Run("minutes ago", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryEntanglementDispute, Detail: "mismatch", SourceTask: "phase-b",
					CreatedAt: fixedNow.Add(-3 * time.Minute)},
			},
			Now: nowFn,
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "3m ago") {
			t.Errorf("expected '3m ago', got:\n%s", got)
		}
	})

	t.Run("hours ago", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryBudgetAlert, Detail: "over budget", SourceTask: "phase-c",
					CreatedAt: fixedNow.Add(-2 * time.Hour)},
			},
			Now: nowFn,
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "2h ago") {
			t.Errorf("expected '2h ago', got:\n%s", got)
		}
	})

	t.Run("days ago", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryMissingDependency, Detail: "dep missing", SourceTask: "phase-d",
					CreatedAt: fixedNow.Add(-48 * time.Hour)},
			},
			Now: nowFn,
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "2d ago") {
			t.Errorf("expected '2d ago', got:\n%s", got)
		}
	})

	t.Run("with affects field", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			UnresolvedDiscoveries: []Discovery{
				{Kind: DiscoveryFileConflict, Detail: "claimed by both",
					SourceTask: "phase-b", Affects: "phase-a",
					CreatedAt: fixedNow.Add(-5 * time.Minute)},
			},
			Now: nowFn,
		}
		got := RenderSnapshot(snap)
		if !strings.Contains(got, "affects: phase-a") {
			t.Error("expected affects field in output")
		}
		if !strings.Contains(got, "5m ago") {
			t.Error("expected timestamp in output")
		}
	})
}

func TestRenderSnapshot_PulseGrouping(t *testing.T) {
	t.Parallel()

	t.Run("grouped by kind with headers", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Pulses: []Pulse{
				{TaskID: "phase-a", Kind: PulseDecision, Content: "switched to generics"},
				{TaskID: "phase-b", Kind: PulseDecision, Content: "using WAL mode"},
				{TaskID: "phase-c", Kind: PulseFailure, Content: "gosec fails on line 42"},
				{TaskID: "phase-d", Kind: PulseNote, Content: "found performance bottleneck"},
				{TaskID: "phase-e", Kind: PulseReviewerFeedback, Content: "needs more tests"},
			},
		}
		got := RenderSnapshot(snap)

		if !strings.Contains(got, "**Decisions:**") {
			t.Error("expected Decisions header")
		}
		if !strings.Contains(got, "**Failures:**") {
			t.Error("expected Failures header")
		}
		if !strings.Contains(got, "**Notes:**") {
			t.Error("expected Notes header")
		}
		if !strings.Contains(got, "**Reviewer Feedback:**") {
			t.Error("expected Reviewer Feedback header")
		}
		if !strings.Contains(got, "[phase-a] switched to generics") {
			t.Error("expected decision content")
		}
		if !strings.Contains(got, "[phase-c] gosec fails on line 42") {
			t.Error("expected failure content")
		}
	})

	t.Run("canonical ordering: decisions before failures before notes", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Pulses: []Pulse{
				{TaskID: "a", Kind: PulseNote, Content: "note"},
				{TaskID: "b", Kind: PulseDecision, Content: "decision"},
				{TaskID: "c", Kind: PulseFailure, Content: "failure"},
			},
		}
		got := RenderSnapshot(snap)
		dIdx := strings.Index(got, "**Decisions:**")
		fIdx := strings.Index(got, "**Failures:**")
		nIdx := strings.Index(got, "**Notes:**")
		if dIdx < 0 || fIdx < 0 || nIdx < 0 {
			t.Fatalf("missing pulse headers in:\n%s", got)
		}
		if dIdx >= fIdx {
			t.Error("decisions should appear before failures")
		}
		if fIdx >= nIdx {
			t.Error("failures should appear before notes")
		}
	})

	t.Run("no pulses section when empty", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{}
		got := RenderSnapshot(snap)
		if strings.Contains(got, "### Shared Context") {
			t.Error("should not render Shared Context when no pulses")
		}
	})

	t.Run("unknown pulse kinds render after canonical ones", func(t *testing.T) {
		t.Parallel()
		snap := Snapshot{
			Pulses: []Pulse{
				{TaskID: "a", Kind: "custom_kind", Content: "custom content"},
				{TaskID: "b", Kind: PulseDecision, Content: "a decision"},
			},
		}
		got := RenderSnapshot(snap)
		dIdx := strings.Index(got, "**Decisions:**")
		cIdx := strings.Index(got, "**Custom_kind:**")
		if dIdx < 0 || cIdx < 0 {
			t.Fatalf("missing expected headers in:\n%s", got)
		}
		if dIdx >= cIdx {
			t.Error("canonical kinds should appear before custom kinds")
		}
	})
}

func TestRenderSnapshot_BackwardCompatibility(t *testing.T) {
	t.Parallel()

	// This test validates that the enhanced rendering is a superset of the old format.
	// All substring checks from existing tests should still pass.
	snap := Snapshot{
		Entanglements: []Entanglement{
			{Producer: "phase-core", Kind: KindType, Name: "User", Package: "models"},
		},
		FileClaims: map[string]string{
			"internal/bar.go": "phase-b",
		},
		Completed:  []string{"phase-core"},
		InProgress: []string{"phase-api"},
	}
	got := RenderSnapshot(snap)

	checks := []struct {
		name string
		want string
	}{
		{"fabric state header", "## Fabric State"},
		{"completed phases section", "### Completed Phases"},
		{"completed phase ID", "phase-core"},
		{"entanglements section", "### Available Entanglements"},
		{"file claims section", "### Active File Claims"},
		{"file claim path", "internal/bar.go"},
		{"in-progress section", "### In-Progress Phases"},
		{"in-progress phase ID", "phase-api"},
		{"unresolved section", "### Unresolved Discoveries"},
		{"none for empty discoveries", "(none)"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(got, c.want) {
				t.Errorf("backward compatibility: missing %q in output:\n%s", c.want, got)
			}
		})
	}
}

func TestRelativeTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero seconds", now, "just now"},
		{"30 seconds ago", now.Add(-30 * time.Second), "just now"},
		{"59 seconds ago", now.Add(-59 * time.Second), "just now"},
		{"1 minute ago", now.Add(-1 * time.Minute), "1m ago"},
		{"3 minutes ago", now.Add(-3 * time.Minute), "3m ago"},
		{"59 minutes ago", now.Add(-59 * time.Minute), "59m ago"},
		{"1 hour ago", now.Add(-1 * time.Hour), "1h ago"},
		{"2 hours ago", now.Add(-2 * time.Hour), "2h ago"},
		{"23 hours ago", now.Add(-23 * time.Hour), "23h ago"},
		{"1 day ago", now.Add(-24 * time.Hour), "1d ago"},
		{"3 days ago", now.Add(-72 * time.Hour), "3d ago"},
		{"future time clamps to just now", now.Add(5 * time.Minute), "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := relativeTime(now, tt.t)
			if got != tt.want {
				t.Errorf("relativeTime(%v) = %q, want %q", tt.t, got, tt.want)
			}
		})
	}
}

func TestRenderClaimContext(t *testing.T) {
	t.Parallel()

	t.Run("nil maps", func(t *testing.T) {
		t.Parallel()
		got := renderClaimContext("owner", nil, nil)
		if got != "" {
			t.Errorf("expected empty string for nil maps, got %q", got)
		}
	})

	t.Run("empty maps", func(t *testing.T) {
		t.Parallel()
		got := renderClaimContext("owner", map[string]string{}, map[string][2]int{})
		if got != "" {
			t.Errorf("expected empty string for empty maps, got %q", got)
		}
	})

	t.Run("state only", func(t *testing.T) {
		t.Parallel()
		states := map[string]string{"owner": "running"}
		got := renderClaimContext("owner", states, nil)
		if got != " (running)" {
			t.Errorf("got %q, want %q", got, " (running)")
		}
	})

	t.Run("cycle only", func(t *testing.T) {
		t.Parallel()
		cycles := map[string][2]int{"owner": {3, 5}}

		// nil states map is safe — len(nil)==0 but cycles is non-nil,
		// so the guard doesn't short-circuit.
		got := renderClaimContext("owner", nil, cycles)
		if got != " (cycle 3/5)" {
			t.Errorf("nil states: got %q, want %q", got, " (cycle 3/5)")
		}

		// Empty states map also produces cycle-only context.
		got = renderClaimContext("owner", map[string]string{}, cycles)
		if got != " (cycle 3/5)" {
			t.Errorf("empty states: got %q, want %q", got, " (cycle 3/5)")
		}
	})

	t.Run("both state and cycle", func(t *testing.T) {
		t.Parallel()
		states := map[string]string{"owner": "in_progress"}
		cycles := map[string][2]int{"owner": {2, 5}}
		got := renderClaimContext("owner", states, cycles)
		if got != " (in_progress, cycle 2/5)" {
			t.Errorf("got %q, want %q", got, " (in_progress, cycle 2/5)")
		}
	})
}

func TestPulseKindTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want string
	}{
		{PulseDecision, "Decisions"},
		{PulseFailure, "Failures"},
		{PulseNote, "Notes"},
		{PulseReviewerFeedback, "Reviewer Feedback"},
		{"custom", "Custom"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()
			got := pulseKindTitle(tt.kind)
			if got != tt.want {
				t.Errorf("pulseKindTitle(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestRenderSnapshot_FullIntegration(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	snap := Snapshot{
		Entanglements: []Entanglement{
			{Producer: "phase-a", Kind: KindInterface, Name: "Fabric",
				Signature: "SetPhaseState, GetPhaseState", Package: "fabric",
				File: "internal/fabric/fabric.go"},
			{Producer: "phase-a", Kind: KindFunction, Name: "NewSQLiteFabric",
				Signature: "NewSQLiteFabric(path string) (*SQLiteFabric, error)", Package: "fabric",
				File: "internal/fabric/sqlite.go"},
		},
		FileClaims: map[string]string{
			"internal/loop/loop.go":    "phase-context",
			"internal/agent/prompt.go": "phase-scanner",
		},
		Completed:  []string{"phase-a"},
		InProgress: []string{"phase-context", "phase-scanner"},
		Blocked:    []string{"phase-blocked"},
		UnresolvedDiscoveries: []Discovery{
			{Kind: DiscoveryFileConflict, Detail: "sqlite.go claimed by both",
				SourceTask: "phase-b", Affects: "phase-a",
				CreatedAt: fixedNow.Add(-3 * time.Minute)},
			{Kind: DiscoveryEntanglementDispute, Detail: "Invoker sig mismatch",
				SourceTask: "phase-c",
				CreatedAt:  fixedNow.Add(-10 * time.Second)},
		},
		Pulses: []Pulse{
			{TaskID: "phase-a", Kind: PulseDecision, Content: "Switched to generics"},
			{TaskID: "phase-b", Kind: PulseDecision, Content: "Using WAL mode"},
			{TaskID: "phase-c", Kind: PulseFailure, Content: "gosec fails on line 42"},
		},
		PhaseStates: map[string]string{
			"phase-context": "in_progress",
			"phase-scanner": "in_progress",
		},
		PhaseCycles: map[string][2]int{
			"phase-context": {2, 5},
			"phase-scanner": {1, 5},
		},
		Now: func() time.Time { return fixedNow },
	}

	got := RenderSnapshot(snap)

	checks := []struct {
		name string
		want string
	}{
		{"summary line", "## Fabric State (1 completed, 2 in-progress, 1 blocked, 2 entanglements, 2 unresolved)"},
		{"entanglement file location", "(internal/fabric/fabric.go)"},
		{"entanglement signature", "SetPhaseState, GetPhaseState"},
		{"function file location", "(internal/fabric/sqlite.go)"},
		{"claim with state and cycle", "phase-context (in_progress, cycle 2/5)"},
		{"claim with state and cycle 2", "phase-scanner (in_progress, cycle 1/5)"},
		{"discovery timestamp minutes", "3m ago"},
		{"discovery timestamp just now", "just now"},
		{"discovery affects", "affects: phase-a"},
		{"pulse decisions header", "**Decisions:**"},
		{"pulse failures header", "**Failures:**"},
		{"pulse decision content", "[phase-a] Switched to generics"},
		{"pulse failure content", "[phase-c] gosec fails on line 42"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(got, c.want) {
				t.Errorf("missing %q in output:\n%s", c.want, got)
			}
		})
	}
}

// firstLine returns the first line of a string.
func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}
