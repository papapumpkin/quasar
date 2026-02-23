package board

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPushbackHandler_NeedInfo_PlausibleProducer(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing type definitions",
			MissingInfo: []string{"phase-producer"},
		},
	}
	inProgress := []string{"phase-producer"}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry when plausible producer exists, got %q", action)
	}
}

func TestPushbackHandler_NeedInfo_PlausibleProducer_HardCap(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 6, // at 2*MaxRetries hard cap
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing type definitions",
			MissingInfo: []string{"phase-producer"},
		},
	}
	inProgress := []string{"phase-producer"}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionEscalate {
		t.Errorf("expected ActionEscalate when plausible producer exists but 2*MaxRetries reached, got %q", action)
	}
}

func TestPushbackHandler_NeedInfo_PlausibleProducer_BelowHardCap(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 5, // below 2*MaxRetries (6)
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing type definitions",
			MissingInfo: []string{"phase-producer"},
		},
	}
	inProgress := []string{"phase-producer"}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry when plausible producer exists and below 2*MaxRetries, got %q", action)
	}
}

func TestPushbackHandler_NeedInfo_NoProducer_Escalates(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 3, // at max retries
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing UserService interface",
			MissingInfo: []string{"UserService"},
		},
	}
	inProgress := []string{"phase-unrelated"}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionEscalate {
		t.Errorf("expected ActionEscalate when no producer and max retries reached, got %q", action)
	}
}

func TestPushbackHandler_NeedInfo_NoProducer_RetryBeforeMax(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 1, // below max
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing UserService interface",
			MissingInfo: []string{"UserService"},
		},
	}
	inProgress := []string{"phase-unrelated"}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry when below max retries, got %q", action)
	}
}

func TestPushbackHandler_Conflict_FileClaim_Retries(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: PollResult{
			Decision:     PollConflict,
			Reason:       "file claimed by another phase",
			ConflictWith: "phase-owner",
		},
	}
	inProgress := []string{"phase-owner"}
	snap := BoardSnapshot{
		FileClaims: map[string]string{
			"internal/foo/bar.go": "phase-owner",
		},
	}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for file claim conflict, got %q", action)
	}
}

func TestPushbackHandler_Conflict_Interface_Escalates(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-consumer",
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: PollResult{
			Decision:     PollConflict,
			Reason:       "contradictory type signatures for Processor interface",
			ConflictWith: "phase-other",
		},
	}
	inProgress := []string{"phase-other"}
	snap := BoardSnapshot{
		FileClaims: map[string]string{}, // no file claims for the conflicting phase
	}

	action := h.Handle(context.Background(), bp, inProgress, snap)
	if action != ActionEscalate {
		t.Errorf("expected ActionEscalate for interface conflict, got %q", action)
	}
}

func TestPushbackHandler_DefaultMaxRetries(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{} // zero MaxRetries → default
	if h.maxRetries() != DefaultMaxRetries {
		t.Errorf("expected default max retries %d, got %d", DefaultMaxRetries, h.maxRetries())
	}
	if DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries should be 3, got %d", DefaultMaxRetries)
	}
}

func TestPushbackHandler_CustomMaxRetries(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 7}
	if h.maxRetries() != 7 {
		t.Errorf("expected custom max retries 7, got %d", h.maxRetries())
	}
}

func TestPushbackHandler_RetryCountResetOnSuccessfulPoll(t *testing.T) {
	t.Parallel()

	// This tests that BlockedTracker + PushbackHandler work together:
	// when a phase is unblocked and re-blocked, its retry count resets.
	tracker := NewBlockedTracker()

	// Block the phase twice (simulating retries).
	tracker.Block("phase-a", PollResult{Decision: PollNeedInfo, MissingInfo: []string{"Foo"}})
	tracker.Block("phase-a", PollResult{Decision: PollNeedInfo, MissingInfo: []string{"Foo"}})

	bp := tracker.Get("phase-a")
	if bp.RetryCount != 1 {
		t.Fatalf("expected retry count 1 after two blocks, got %d", bp.RetryCount)
	}

	// Unblock (simulating successful poll / proceed).
	tracker.Unblock("phase-a")
	if tracker.Get("phase-a") != nil {
		t.Fatal("expected phase to be unblocked")
	}

	// Re-block (new cycle) — retry count should start at 0 again.
	tracker.Block("phase-a", PollResult{Decision: PollNeedInfo, MissingInfo: []string{"Bar"}})
	bp = tracker.Get("phase-a")
	if bp.RetryCount != 0 {
		t.Errorf("expected retry count 0 after re-block, got %d", bp.RetryCount)
	}
}

func TestPushbackHandler_ProceedOnUnknownDecision(t *testing.T) {
	t.Parallel()

	h := &PushbackHandler{MaxRetries: 3}
	bp := &BlockedPhase{
		PhaseID:    "phase-x",
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: PollResult{
			Decision: PollProceed,
			Reason:   "all good",
		},
	}
	snap := BoardSnapshot{}

	action := h.Handle(context.Background(), bp, nil, snap)
	if action != ActionProceed {
		t.Errorf("expected ActionProceed for non-blocked decision, got %q", action)
	}
}

func TestEscalationMessage_NeedInfo(t *testing.T) {
	t.Parallel()

	bp := &BlockedPhase{
		PhaseID:    "auth-service",
		BlockedAt:  time.Now(),
		RetryCount: 3,
		LastResult: PollResult{
			Decision:    PollNeedInfo,
			Reason:      "missing UserStore interface definition",
			MissingInfo: []string{"UserStore"},
		},
	}

	msg := EscalationMessage(bp, 3)

	checks := []string{
		"PHASE BLOCKED: auth-service",
		"Reason: NEED_INFO",
		"Details: missing UserStore interface definition",
		"Retries: 3/3",
		"Suggestion: add missing dependency",
	}
	for _, want := range checks {
		if !strings.Contains(msg, want) {
			t.Errorf("escalation message missing %q\ngot:\n%s", want, msg)
		}
	}
}

func TestEscalationMessage_Conflict(t *testing.T) {
	t.Parallel()

	bp := &BlockedPhase{
		PhaseID:    "api-handler",
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: PollResult{
			Decision:     PollConflict,
			Reason:       "contradictory Processor signatures",
			ConflictWith: "data-pipeline",
		},
	}

	msg := EscalationMessage(bp, 3)

	checks := []string{
		"PHASE BLOCKED: api-handler",
		"Reason: CONFLICT",
		"Details: contradictory Processor signatures",
		"Retries: 0/3",
		`resolve conflict with phase "data-pipeline"`,
	}
	for _, want := range checks {
		if !strings.Contains(msg, want) {
			t.Errorf("escalation message missing %q\ngot:\n%s", want, msg)
		}
	}
}

func TestEscalationMessage_ConflictNoTarget(t *testing.T) {
	t.Parallel()

	bp := &BlockedPhase{
		PhaseID:    "api-handler",
		BlockedAt:  time.Now(),
		RetryCount: 1,
		LastResult: PollResult{
			Decision: PollConflict,
			Reason:   "multiple conflicting contracts",
		},
	}

	msg := EscalationMessage(bp, 5)

	if !strings.Contains(msg, "resolve the conflicting contracts") {
		t.Errorf("expected generic conflict suggestion\ngot:\n%s", msg)
	}
	if !strings.Contains(msg, "Retries: 1/5") {
		t.Errorf("expected retry count 1/5\ngot:\n%s", msg)
	}
}

func TestHasPlausibleProducer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		missingInfo []string
		inProgress  []string
		want        bool
	}{
		{
			name:        "exact match",
			missingInfo: []string{"phase-auth"},
			inProgress:  []string{"phase-auth"},
			want:        true,
		},
		{
			name:        "phase ID in missing info string",
			missingInfo: []string{"phase-auth types"},
			inProgress:  []string{"phase-auth"},
			want:        true,
		},
		{
			name:        "case insensitive",
			missingInfo: []string{"Phase-Auth"},
			inProgress:  []string{"phase-auth"},
			want:        true,
		},
		{
			name:        "no match",
			missingInfo: []string{"UserService"},
			inProgress:  []string{"phase-db", "phase-api"},
			want:        false,
		},
		{
			name:        "empty in-progress",
			missingInfo: []string{"Foo"},
			inProgress:  nil,
			want:        false,
		},
		{
			name:        "empty missing info",
			missingInfo: nil,
			inProgress:  []string{"phase-a"},
			want:        false,
		},
		{
			name:        "short phase ID skipped",
			missingInfo: []string{"the database connection id is missing"},
			inProgress:  []string{"db"},
			want:        false,
		},
		{
			name:        "short phase ID at boundary skipped",
			missingInfo: []string{"id lookup failed"},
			inProgress:  []string{"id"},
			want:        false,
		},
		{
			name:        "reverse direction not matched",
			missingInfo: []string{"db"},
			inProgress:  []string{"phase-db-migration"},
			want:        false,
		},
		{
			name:        "phase ID at min length matches",
			missingInfo: []string{"need auth service"},
			inProgress:  []string{"auth"},
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasPlausibleProducer(tt.missingInfo, tt.inProgress)
			if got != tt.want {
				t.Errorf("hasPlausibleProducer(%v, %v) = %v, want %v",
					tt.missingInfo, tt.inProgress, got, tt.want)
			}
		})
	}
}

func TestIsFileClaimConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		conflictWith string
		fileClaims   map[string]string
		want         bool
	}{
		{
			name:         "phase has file claims",
			conflictWith: "phase-owner",
			fileClaims:   map[string]string{"foo.go": "phase-owner"},
			want:         true,
		},
		{
			name:         "phase has no file claims",
			conflictWith: "phase-other",
			fileClaims:   map[string]string{"foo.go": "phase-owner"},
			want:         false,
		},
		{
			name:         "empty conflict target",
			conflictWith: "",
			fileClaims:   map[string]string{"foo.go": "phase-owner"},
			want:         false,
		},
		{
			name:         "no claims at all",
			conflictWith: "phase-owner",
			fileClaims:   map[string]string{},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap := BoardSnapshot{FileClaims: tt.fileClaims}
			got := isFileClaimConflict(tt.conflictWith, snap)
			if got != tt.want {
				t.Errorf("isFileClaimConflict(%q, snap) = %v, want %v",
					tt.conflictWith, got, tt.want)
			}
		})
	}
}

func TestPushbackActionConstants(t *testing.T) {
	t.Parallel()

	// Verify action string values are stable (used in serialization/logging).
	if ActionRetry != "retry" {
		t.Errorf("ActionRetry = %q, want %q", ActionRetry, "retry")
	}
	if ActionEscalate != "escalate" {
		t.Errorf("ActionEscalate = %q, want %q", ActionEscalate, "escalate")
	}
	if ActionProceed != "proceed" {
		t.Errorf("ActionProceed = %q, want %q", ActionProceed, "proceed")
	}
}
