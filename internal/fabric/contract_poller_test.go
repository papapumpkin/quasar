package fabric

import (
	"context"
	"strings"
	"testing"
)

func TestContractPollerSatisfiesInterface(t *testing.T) {
	t.Parallel()
	// Compile-time check that ContractPoller implements Poller.
	var _ Poller = (*ContractPoller)(nil)
}

func TestContractPollerPoll(t *testing.T) {
	t.Parallel()

	// Shared entanglements used across test cases.
	entFoo := Entanglement{Kind: KindType, Name: "Foo", Package: "pkg/a"}
	entBar := Entanglement{Kind: KindFunction, Name: "Bar", Package: "pkg/b"}
	entBaz := Entanglement{Kind: KindInterface, Name: "Baz", Package: "pkg/c"}

	tests := []struct {
		name         string
		contracts    map[string]*PhaseContract
		matchMode    MatchMode
		phaseID      string
		snap         Snapshot
		wantDecision PollDecision
		wantReason   string // substring match
		wantMissing  int    // expected len(MissingInfo)
		wantConflict string // expected ConflictWith
	}{
		{
			name:         "no contract registered — fail open",
			contracts:    map[string]*PhaseContract{},
			phaseID:      "phase-1",
			snap:         Snapshot{},
			wantDecision: PollProceed,
			wantReason:   "no contract registered",
		},
		{
			name: "no consumed entanglements — always proceed",
			contracts: map[string]*PhaseContract{
				"phase-1": {PhaseID: "phase-1", Consumes: nil, Produces: []Entanglement{entFoo}},
			},
			phaseID:      "phase-1",
			snap:         Snapshot{},
			wantDecision: PollProceed,
			wantReason:   "no consumed entanglements",
		},
		{
			name: "all consumed entanglements fulfilled — exact match",
			contracts: map[string]*PhaseContract{
				"phase-2": {PhaseID: "phase-2", Consumes: []Entanglement{entFoo, entBar}},
			},
			matchMode: MatchExact,
			phaseID:   "phase-2",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo, entBar, entBaz},
			},
			wantDecision: PollProceed,
			wantReason:   "all contracts fulfilled",
		},
		{
			name: "partial missing — exact match",
			contracts: map[string]*PhaseContract{
				"phase-3": {PhaseID: "phase-3", Consumes: []Entanglement{entFoo, entBar}},
			},
			matchMode: MatchExact,
			phaseID:   "phase-3",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo}, // only Foo published
			},
			wantDecision: PollNeedInfo,
			wantReason:   "1/2 consumed entanglements missing",
			wantMissing:  1,
		},
		{
			name: "all missing — empty board",
			contracts: map[string]*PhaseContract{
				"phase-4": {PhaseID: "phase-4", Consumes: []Entanglement{entFoo, entBar, entBaz}},
			},
			matchMode:    MatchExact,
			phaseID:      "phase-4",
			snap:         Snapshot{},
			wantDecision: PollNeedInfo,
			wantReason:   "3/3 consumed entanglements missing",
			wantMissing:  3,
		},
		{
			name: "file claim conflict",
			contracts: map[string]*PhaseContract{
				"phase-5": {
					PhaseID:  "phase-5",
					Consumes: []Entanglement{entFoo},
					Scope:    []string{"pkg/a/foo.go"},
				},
			},
			phaseID: "phase-5",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo},
				FileClaims:    map[string]string{"pkg/a/foo.go": "phase-other"},
			},
			wantDecision: PollConflict,
			wantReason:   "claimed by phase-other",
			wantConflict: "phase-other",
		},
		{
			name: "match name mode — different package still matches",
			contracts: map[string]*PhaseContract{
				"phase-6": {
					PhaseID: "phase-6",
					Consumes: []Entanglement{
						{Kind: KindType, Name: "Foo", Package: "pkg/different"},
					},
				},
			},
			matchMode: MatchName,
			phaseID:   "phase-6",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo}, // pkg/a, not pkg/different
			},
			wantDecision: PollProceed,
			wantReason:   "all contracts fulfilled",
		},
		{
			name: "exact mode — different package does NOT match",
			contracts: map[string]*PhaseContract{
				"phase-7": {
					PhaseID: "phase-7",
					Consumes: []Entanglement{
						{Kind: KindType, Name: "Foo", Package: "pkg/different"},
					},
				},
			},
			matchMode: MatchExact,
			phaseID:   "phase-7",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo}, // pkg/a, not pkg/different
			},
			wantDecision: PollNeedInfo,
			wantReason:   "1/1 consumed entanglements missing",
			wantMissing:  1,
		},
		{
			name: "no scope conflict — file not claimed",
			contracts: map[string]*PhaseContract{
				"phase-8": {
					PhaseID:  "phase-8",
					Consumes: []Entanglement{entFoo},
					Scope:    []string{"pkg/a/foo.go"},
				},
			},
			phaseID: "phase-8",
			snap: Snapshot{
				Entanglements: []Entanglement{entFoo},
				FileClaims:    map[string]string{}, // empty claims
			},
			wantDecision: PollProceed,
			wantReason:   "all contracts fulfilled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			poller := &ContractPoller{
				Contracts: tc.contracts,
				MatchMode: tc.matchMode,
			}

			result, err := poller.Poll(context.Background(), tc.phaseID, tc.snap)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Decision != tc.wantDecision {
				t.Errorf("decision = %q, want %q", result.Decision, tc.wantDecision)
			}

			if !strings.Contains(result.Reason, tc.wantReason) {
				t.Errorf("reason = %q, want substring %q", result.Reason, tc.wantReason)
			}

			if len(result.MissingInfo) != tc.wantMissing {
				t.Errorf("missing count = %d, want %d; items: %v", len(result.MissingInfo), tc.wantMissing, result.MissingInfo)
			}

			if result.ConflictWith != tc.wantConflict {
				t.Errorf("conflict_with = %q, want %q", result.ConflictWith, tc.wantConflict)
			}
		})
	}
}

func TestEntanglementKey(t *testing.T) {
	t.Parallel()

	e := Entanglement{Kind: KindType, Name: "Foo", Package: "pkg/a"}

	tests := []struct {
		name string
		mode MatchMode
		want string
	}{
		{name: "exact", mode: MatchExact, want: "type:pkg/a:Foo"},
		{name: "name only", mode: MatchName, want: "type:Foo"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := entanglementKey(e, tc.mode)
			if got != tc.want {
				t.Errorf("entanglementKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexEntanglements(t *testing.T) {
	t.Parallel()

	entanglements := []Entanglement{
		{Kind: KindType, Name: "Foo", Package: "pkg/a"},
		{Kind: KindFunction, Name: "Bar", Package: "pkg/b"},
	}

	t.Run("exact mode", func(t *testing.T) {
		t.Parallel()
		idx := indexEntanglements(entanglements, MatchExact)
		if !idx["type:pkg/a:Foo"] {
			t.Error("expected type:pkg/a:Foo in index")
		}
		if !idx["function:pkg/b:Bar"] {
			t.Error("expected function:pkg/b:Bar in index")
		}
		if idx["type:Foo"] {
			t.Error("unexpected type:Foo in exact mode index")
		}
	})

	t.Run("name mode", func(t *testing.T) {
		t.Parallel()
		idx := indexEntanglements(entanglements, MatchName)
		if !idx["type:Foo"] {
			t.Error("expected type:Foo in index")
		}
		if !idx["function:Bar"] {
			t.Error("expected function:Bar in index")
		}
	})
}
