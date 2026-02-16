package nebula

import (
	"context"
	"testing"
	"time"
)

func TestCollectAgentmailMetricsNilDB(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordConflict("p1")

	before := m.Snapshot()

	err := CollectAgentmailMetrics(context.Background(), nil, m, time.Now())
	if err != nil {
		t.Fatalf("unexpected error for nil db: %v", err)
	}

	after := m.Snapshot()
	if after.TotalConflicts != before.TotalConflicts {
		t.Errorf("TotalConflicts changed from %d to %d; nil db should be no-op",
			before.TotalConflicts, after.TotalConflicts)
	}
}

func TestApplyAgentmailMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		existingConfl  int
		am             AgentmailMetrics
		wantTotalConfl int
	}{
		{
			name:           "zero agentmail conflicts",
			existingConfl:  2,
			am:             AgentmailMetrics{ConflictCount: 0},
			wantTotalConfl: 2,
		},
		{
			name:           "agentmail conflicts added",
			existingConfl:  1,
			am:             AgentmailMetrics{ConflictCount: 3},
			wantTotalConfl: 4,
		},
		{
			name:           "no existing conflicts",
			existingConfl:  0,
			am:             AgentmailMetrics{ConflictCount: 5},
			wantTotalConfl: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := NewMetrics("test")
			for i := 0; i < tc.existingConfl; i++ {
				m.RecordPhaseStart("p", 0)
				m.RecordConflict("p")
			}

			applyAgentmailMetrics(m, tc.am)

			snap := m.Snapshot()
			if snap.TotalConflicts != tc.wantTotalConfl {
				t.Errorf("TotalConflicts = %d, want %d",
					snap.TotalConflicts, tc.wantTotalConfl)
			}
		})
	}
}

func TestAgentmailMetricsZeroValue(t *testing.T) {
	t.Parallel()

	// Applying zero-value AgentmailMetrics should not change anything.
	m := NewMetrics("test")
	applyAgentmailMetrics(m, AgentmailMetrics{})

	snap := m.Snapshot()
	if snap.TotalConflicts != 0 {
		t.Errorf("TotalConflicts = %d, want 0 after zero-value apply", snap.TotalConflicts)
	}
}

func TestAgentmailMetricsStruct(t *testing.T) {
	t.Parallel()

	am := AgentmailMetrics{
		ConflictCount: 3,
		ChangeVolume:  12,
		ActiveClaims:  5,
		AvgClaimAge:   10 * time.Second,
	}

	if am.ConflictCount != 3 {
		t.Errorf("ConflictCount = %d, want 3", am.ConflictCount)
	}
	if am.ChangeVolume != 12 {
		t.Errorf("ChangeVolume = %d, want 12", am.ChangeVolume)
	}
	if am.ActiveClaims != 5 {
		t.Errorf("ActiveClaims = %d, want 5", am.ActiveClaims)
	}
	if am.AvgClaimAge != 10*time.Second {
		t.Errorf("AvgClaimAge = %v, want 10s", am.AvgClaimAge)
	}
}
