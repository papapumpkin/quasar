package tui

import "testing"

func TestPhaseHealth_Derivation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phase  PhaseEntry
		expect PhaseHealth
	}{
		{"early cycle green", PhaseEntry{Status: PhaseWorking, Cycles: 1, MaxCycles: 5}, HealthGreen},
		{"60pct boundary green", PhaseEntry{Status: PhaseWorking, Cycles: 3, MaxCycles: 5}, HealthGreen},
		{"over 60pct yellow", PhaseEntry{Status: PhaseWorking, Cycles: 4, MaxCycles: 5}, HealthYellow},
		{"over 60pct satisfied", PhaseEntry{Status: PhaseWorking, Cycles: 4, MaxCycles: 5, ReviewerSatisfied: true}, HealthGreen},
		{"final cycle red", PhaseEntry{Status: PhaseWorking, Cycles: 5, MaxCycles: 5}, HealthRed},
		{"final satisfied green", PhaseEntry{Status: PhaseWorking, Cycles: 5, MaxCycles: 5, ReviewerSatisfied: true}, HealthGreen},
		{"hails override red", PhaseEntry{Status: PhaseWorking, Cycles: 1, MaxCycles: 5, HasPendingHails: true}, HealthRed},
		{"gate yellow", PhaseEntry{Status: PhaseGate}, HealthYellow},
		{"done green", PhaseEntry{Status: PhaseDone}, HealthGreen},
		{"failed green", PhaseEntry{Status: PhaseFailed}, HealthGreen},
		{"waiting green", PhaseEntry{Status: PhaseWaiting}, HealthGreen},
		{"no max cycles green", PhaseEntry{Status: PhaseWorking, Cycles: 3, MaxCycles: 0}, HealthGreen},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.phase.Health()
			if got != tc.expect {
				t.Errorf("Health() = %d, want %d", got, tc.expect)
			}
		})
	}
}

func TestPhaseStatusFromString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		expect PhaseStatus
	}{
		{"done", PhaseDone},
		{"failed", PhaseFailed},
		{"in_progress", PhaseWorking},
		{"skipped", PhaseSkipped},
		{"gate", PhaseGate},
		{"unknown", PhaseWaiting},
		{"", PhaseWaiting},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := PhaseStatusFromString(tc.input)
			if got != tc.expect {
				t.Errorf("PhaseStatusFromString(%q) = %d, want %d", tc.input, got, tc.expect)
			}
		})
	}
}
