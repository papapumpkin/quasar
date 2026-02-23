package fabric

import (
	"context"
	"strings"
	"testing"
)

func TestPollDecisionValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    PollDecision
		want string
	}{
		{"proceed", PollProceed, "PROCEED"},
		{"need_info", PollNeedInfo, "NEED_INFO"},
		{"conflict", PollConflict, "CONFLICT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.d) != tt.want {
				t.Errorf("PollDecision = %q, want %q", tt.d, tt.want)
			}
		})
	}
}

func TestPollResultFields(t *testing.T) {
	t.Parallel()

	t.Run("proceed result", func(t *testing.T) {
		t.Parallel()
		r := PollResult{
			Decision: PollProceed,
			Reason:   "all entanglements available",
		}
		if r.Decision != PollProceed {
			t.Errorf("Decision = %q, want %q", r.Decision, PollProceed)
		}
		if r.Reason == "" {
			t.Error("Reason should not be empty")
		}
		if len(r.MissingInfo) != 0 {
			t.Errorf("MissingInfo = %v, want empty", r.MissingInfo)
		}
		if r.ConflictWith != "" {
			t.Errorf("ConflictWith = %q, want empty", r.ConflictWith)
		}
	})

	t.Run("need_info result", func(t *testing.T) {
		t.Parallel()
		r := PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing type UserService",
			MissingInfo: []string{"UserService", "AuthProvider"},
		}
		if r.Decision != PollNeedInfo {
			t.Errorf("Decision = %q, want %q", r.Decision, PollNeedInfo)
		}
		if len(r.MissingInfo) != 2 {
			t.Errorf("MissingInfo length = %d, want 2", len(r.MissingInfo))
		}
		if r.MissingInfo[0] != "UserService" {
			t.Errorf("MissingInfo[0] = %q, want %q", r.MissingInfo[0], "UserService")
		}
	})

	t.Run("conflict result", func(t *testing.T) {
		t.Parallel()
		r := PollResult{
			Decision:     PollConflict,
			Reason:       "file claim conflict on internal/auth/auth.go",
			ConflictWith: "phase-auth",
		}
		if r.Decision != PollConflict {
			t.Errorf("Decision = %q, want %q", r.Decision, PollConflict)
		}
		if r.ConflictWith != "phase-auth" {
			t.Errorf("ConflictWith = %q, want %q", r.ConflictWith, "phase-auth")
		}
	})
}

func TestBlockedTracker(t *testing.T) {
	t.Parallel()

	t.Run("block and get", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		result := PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing entanglement",
			MissingInfo: []string{"Foo"},
		}
		bt.Block("phase-1", result)

		bp := bt.Get("phase-1")
		if bp == nil {
			t.Fatal("Get returned nil for blocked phase")
		}
		if bp.PhaseID != "phase-1" {
			t.Errorf("PhaseID = %q, want %q", bp.PhaseID, "phase-1")
		}
		if bp.RetryCount != 0 {
			t.Errorf("RetryCount = %d, want 0", bp.RetryCount)
		}
		if bp.LastResult.Decision != PollNeedInfo {
			t.Errorf("LastResult.Decision = %q, want %q", bp.LastResult.Decision, PollNeedInfo)
		}
		if bp.BlockedAt.IsZero() {
			t.Error("BlockedAt should not be zero")
		}
	})

	t.Run("retry increments count", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		first := PollResult{Decision: PollNeedInfo, Reason: "first"}
		second := PollResult{Decision: PollNeedInfo, Reason: "second"}
		third := PollResult{Decision: PollConflict, Reason: "third", ConflictWith: "phase-x"}

		bt.Block("phase-1", first)
		bt.Block("phase-1", second)
		bt.Block("phase-1", third)

		bp := bt.Get("phase-1")
		if bp == nil {
			t.Fatal("Get returned nil")
		}
		if bp.RetryCount != 2 {
			t.Errorf("RetryCount = %d, want 2", bp.RetryCount)
		}
		if bp.LastResult.Decision != PollConflict {
			t.Errorf("LastResult.Decision = %q, want %q", bp.LastResult.Decision, PollConflict)
		}
		if bp.LastResult.ConflictWith != "phase-x" {
			t.Errorf("LastResult.ConflictWith = %q, want %q", bp.LastResult.ConflictWith, "phase-x")
		}
	})

	t.Run("unblock removes phase", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		bt.Block("phase-1", PollResult{Decision: PollNeedInfo, Reason: "blocked"})
		bt.Unblock("phase-1")

		if bp := bt.Get("phase-1"); bp != nil {
			t.Errorf("expected nil after unblock, got %+v", bp)
		}
		if bt.Len() != 0 {
			t.Errorf("Len = %d, want 0", bt.Len())
		}
	})

	t.Run("unblock nonexistent is no-op", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		// Should not panic.
		bt.Unblock("nonexistent")
		if bt.Len() != 0 {
			t.Errorf("Len = %d, want 0", bt.Len())
		}
	})

	t.Run("get nonexistent returns nil", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		if bp := bt.Get("nonexistent"); bp != nil {
			t.Errorf("expected nil, got %+v", bp)
		}
	})

	t.Run("all returns snapshot", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		bt.Block("phase-1", PollResult{Decision: PollNeedInfo, Reason: "a"})
		bt.Block("phase-2", PollResult{Decision: PollConflict, Reason: "b", ConflictWith: "phase-1"})

		all := bt.All()
		if len(all) != 2 {
			t.Fatalf("All() returned %d phases, want 2", len(all))
		}

		ids := make(map[string]bool)
		for _, bp := range all {
			ids[bp.PhaseID] = true
		}
		if !ids["phase-1"] || !ids["phase-2"] {
			t.Errorf("All() missing expected phases, got IDs: %v", ids)
		}
	})

	t.Run("len tracks count", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		if bt.Len() != 0 {
			t.Errorf("initial Len = %d, want 0", bt.Len())
		}
		bt.Block("a", PollResult{Decision: PollNeedInfo, Reason: "x"})
		bt.Block("b", PollResult{Decision: PollNeedInfo, Reason: "y"})
		if bt.Len() != 2 {
			t.Errorf("Len = %d, want 2", bt.Len())
		}
		bt.Unblock("a")
		if bt.Len() != 1 {
			t.Errorf("Len = %d, want 1", bt.Len())
		}
	})
}

func TestBlockedTrackerEscalation(t *testing.T) {
	t.Parallel()

	t.Run("no escalation below threshold", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		r := PollResult{Decision: PollNeedInfo, Reason: "missing"}
		for i := 0; i < MaxPollRetries; i++ {
			bt.Block("phase-1", r)
		}

		// RetryCount is MaxPollRetries-1 because the first Block sets count=0.
		bp := bt.Get("phase-1")
		if bp.RetryCount != MaxPollRetries-1 {
			t.Errorf("RetryCount = %d, want %d", bp.RetryCount, MaxPollRetries-1)
		}
		if bt.NeedsEscalation("phase-1") {
			t.Error("should not need escalation yet")
		}
	})

	t.Run("escalation at threshold", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		r := PollResult{Decision: PollNeedInfo, Reason: "missing"}
		// Block once (count=0), then block MaxPollRetries more times.
		for i := 0; i <= MaxPollRetries; i++ {
			bt.Block("phase-1", r)
		}

		bp := bt.Get("phase-1")
		if bp.RetryCount != MaxPollRetries {
			t.Errorf("RetryCount = %d, want %d", bp.RetryCount, MaxPollRetries)
		}
		if !bt.NeedsEscalation("phase-1") {
			t.Error("should need escalation at threshold")
		}
	})

	t.Run("nonexistent phase does not need escalation", func(t *testing.T) {
		t.Parallel()
		bt := NewBlockedTracker()

		if bt.NeedsEscalation("nonexistent") {
			t.Error("nonexistent phase should not need escalation")
		}
	})
}

func TestStateConstantsAlignWithScanning(t *testing.T) {
	t.Parallel()

	// Verify that the phase state constants used in scanning are defined and
	// match expected string values for the fabric's state column.
	states := map[string]string{
		"queued":         StateQueued,
		"scanning":       StateScanning,
		"running":        StateRunning,
		"blocked":        StateBlocked,
		"done":           StateDone,
		"failed":         StateFailed,
		"human_decision": StateHumanDecision,
	}

	for want, got := range states {
		if got != want {
			t.Errorf("State constant = %q, want %q", got, want)
		}
	}
}

// mockPoller implements Poller for testing.
type mockPoller struct {
	decision PollDecision
	reason   string
	missing  []string
	conflict string
	err      error
}

func (m *mockPoller) Poll(_ context.Context, _ string, _ FabricSnapshot) (PollResult, error) {
	if m.err != nil {
		return PollResult{}, m.err
	}
	return PollResult{
		Decision:     m.decision,
		Reason:       m.reason,
		MissingInfo:  m.missing,
		ConflictWith: m.conflict,
	}, nil
}

func TestPollerInterface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	snap := FabricSnapshot{}

	t.Run("proceed decision", func(t *testing.T) {
		t.Parallel()
		p := &mockPoller{decision: PollProceed, reason: "all good"}

		result, err := p.Poll(ctx, "phase-1", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollProceed {
			t.Errorf("Decision = %q, want %q", result.Decision, PollProceed)
		}
	})

	t.Run("need_info decision", func(t *testing.T) {
		t.Parallel()
		p := &mockPoller{
			decision: PollNeedInfo,
			reason:   "missing type",
			missing:  []string{"UserService"},
		}

		result, err := p.Poll(ctx, "phase-2", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollNeedInfo {
			t.Errorf("Decision = %q, want %q", result.Decision, PollNeedInfo)
		}
		if len(result.MissingInfo) != 1 || result.MissingInfo[0] != "UserService" {
			t.Errorf("MissingInfo = %v, want [UserService]", result.MissingInfo)
		}
	})

	t.Run("conflict decision", func(t *testing.T) {
		t.Parallel()
		p := &mockPoller{
			decision: PollConflict,
			reason:   "file conflict",
			conflict: "phase-auth",
		}

		result, err := p.Poll(ctx, "phase-3", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollConflict {
			t.Errorf("Decision = %q, want %q", result.Decision, PollConflict)
		}
		if result.ConflictWith != "phase-auth" {
			t.Errorf("ConflictWith = %q, want %q", result.ConflictWith, "phase-auth")
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		t.Parallel()
		wantErr := "fabric unavailable"
		p := &mockPoller{err: errForTest(wantErr)}

		_, err := p.Poll(ctx, "phase-4", snap)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Errorf("error = %q, want containing %q", err.Error(), wantErr)
		}
	})
}

// errForTest returns a simple error for test assertions.
type errForTest string

func (e errForTest) Error() string { return string(e) }
