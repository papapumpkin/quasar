package worker

import (
	"sort"
	"strings"
	"testing"
)

func TestNewScheduler_SimpleChain(t *testing.T) {
	t.Parallel()

	// a -> b -> c (c depends on b, b depends on a)
	phases := []PhaseSpec{
		{ID: "a", Priority: 1},
		{ID: "b", Priority: 2, DependsOn: []string{"a"}},
		{ID: "c", Priority: 3, DependsOn: []string{"b"}},
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	// Single chain = single track.
	tracks := s.Tracks()
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if len(tracks[0].NodeIDs) != 3 {
		t.Errorf("expected 3 nodes in track, got %d", len(tracks[0].NodeIDs))
	}

	// Impact scores should be populated.
	scores := s.ImpactScores()
	if len(scores) != 3 {
		t.Errorf("expected 3 impact scores, got %d", len(scores))
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := scores[id]; !ok {
			t.Errorf("missing impact score for %q", id)
		}
	}

	// Ready tasks with nothing done: only a should be ready.
	ready := s.ReadyTasks(map[string]bool{})
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("expected [a] ready, got %v", ready)
	}

	// After a is done: b should be ready.
	ready = s.ReadyTasks(map[string]bool{"a": true})
	if len(ready) != 1 || ready[0] != "b" {
		t.Errorf("expected [b] ready after a done, got %v", ready)
	}

	// After a and b done: c should be ready.
	ready = s.ReadyTasks(map[string]bool{"a": true, "b": true})
	if len(ready) != 1 || ready[0] != "c" {
		t.Errorf("expected [c] ready after a,b done, got %v", ready)
	}
}

func TestNewScheduler_IndependentTracks(t *testing.T) {
	t.Parallel()

	// Two independent chains: a->b and c->d
	phases := []PhaseSpec{
		{ID: "a", Priority: 2},
		{ID: "b", Priority: 1, DependsOn: []string{"a"}},
		{ID: "c", Priority: 2},
		{ID: "d", Priority: 1, DependsOn: []string{"c"}},
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	// Two independent chains = two tracks.
	tracks := s.Tracks()
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}

	// Ready tasks with nothing done: a and c should be ready.
	ready := s.ReadyTasks(map[string]bool{})
	sort.Strings(ready)
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d: %v", len(ready), ready)
	}
	if ready[0] != "a" || ready[1] != "c" {
		t.Errorf("expected [a, c] ready, got %v", ready)
	}
}

func TestNewScheduler_ImpactSortedReady(t *testing.T) {
	t.Parallel()

	// Diamond: a and b are roots, c depends on both.
	// b has higher priority, but impact scoring determines order.
	phases := []PhaseSpec{
		{ID: "a", Priority: 1},
		{ID: "b", Priority: 1},
		{ID: "c", Priority: 1, DependsOn: []string{"a", "b"}},
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	// Both a and b should be ready, sorted by impact score.
	ready := s.ReadyTasks(map[string]bool{})
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d: %v", len(ready), ready)
	}

	// Verify that ready tasks are sorted by impact (descending).
	scores := s.ImpactScores()
	if scores[ready[0]] < scores[ready[1]] {
		t.Errorf("ready tasks not sorted by impact: %q (%.4f) < %q (%.4f)",
			ready[0], scores[ready[0]], ready[1], scores[ready[1]])
	}
}

func TestNewScheduler_TrackForTask(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c"}, // independent
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	// a and b should be in the same track.
	trackA := s.TrackForTask("a")
	trackB := s.TrackForTask("b")
	trackC := s.TrackForTask("c")

	if trackA != trackB {
		t.Errorf("a and b should be in same track, got %d and %d", trackA, trackB)
	}
	if trackA == trackC {
		t.Errorf("a and c should be in different tracks, both got %d", trackA)
	}

	// Unknown task returns -1.
	if got := s.TrackForTask("unknown"); got != -1 {
		t.Errorf("TrackForTask(unknown) = %d, want -1", got)
	}
}

func TestTrackParallelism(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phases     []PhaseSpec
		maxWorkers int
		wantMin    int // minimum expected parallelism
		wantMax    int // maximum expected parallelism
	}{
		{
			name: "single track caps at 1",
			phases: []PhaseSpec{
				{ID: "a"},
				{ID: "b", DependsOn: []string{"a"}},
			},
			maxWorkers: 4,
			wantMin:    1,
			wantMax:    1,
		},
		{
			name: "two independent tracks",
			phases: []PhaseSpec{
				{ID: "a"},
				{ID: "b"},
			},
			maxWorkers: 4,
			wantMin:    2,
			wantMax:    2,
		},
		{
			name: "max workers caps tracks",
			phases: []PhaseSpec{
				{ID: "a"},
				{ID: "b"},
				{ID: "c"},
				{ID: "d"},
			},
			maxWorkers: 2,
			wantMin:    2,
			wantMax:    2,
		},
		{
			name:       "empty phases",
			phases:     []PhaseSpec{},
			maxWorkers: 4,
			wantMin:    0,
			wantMax:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if len(tt.phases) == 0 {
				got := TrackParallelism(nil, tt.maxWorkers)
				if got != 0 {
					t.Errorf("TrackParallelism(nil) = %d, want 0", got)
				}
				return
			}

			s, err := NewScheduler(tt.phases)
			if err != nil {
				t.Fatalf("NewScheduler failed: %v", err)
			}

			got := TrackParallelism(s.Tracks(), tt.maxWorkers)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("TrackParallelism() = %d, want [%d, %d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestNewScheduler_SinglePhase(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "only"},
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	tracks := s.Tracks()
	if len(tracks) != 1 {
		t.Errorf("expected 1 track, got %d", len(tracks))
	}

	ready := s.ReadyTasks(map[string]bool{})
	if len(ready) != 1 || ready[0] != "only" {
		t.Errorf("expected [only] ready, got %v", ready)
	}

	ready = s.ReadyTasks(map[string]bool{"only": true})
	if len(ready) != 0 {
		t.Errorf("expected no ready tasks after completion, got %v", ready)
	}
}

func TestNewScheduler_Analyzer(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
	}

	s, err := NewScheduler(phases)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}

	if s.Analyzer() == nil {
		t.Error("Analyzer() should not return nil")
	}
	if s.Analyzer().Len() != 2 {
		t.Errorf("Analyzer().Len() = %d, want 2", s.Analyzer().Len())
	}
}

func TestNewScheduler_MissingDependency(t *testing.T) {
	t.Parallel()

	// Phase b depends on a non-existent phase "missing".
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"missing"}},
	}

	_, err := NewScheduler(phases)
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
	if !strings.Contains(err.Error(), "adding dependency") {
		t.Errorf("error should mention 'adding dependency', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("error should mention phase ID %q, got: %v", "b", err)
	}
	if !strings.Contains(err.Error(), `"missing"`) {
		t.Errorf("error should mention missing dep %q, got: %v", "missing", err)
	}
}

func TestNewScheduler_CyclicDependency(t *testing.T) {
	t.Parallel()

	// a -> b -> a (cycle)
	phases := []PhaseSpec{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}

	_, err := NewScheduler(phases)
	if err == nil {
		t.Fatal("expected error for cyclic dependency, got nil")
	}
	// The error should come from the AddDependency step which detects cycles.
	if !strings.Contains(err.Error(), "adding dependency") && !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle or adding dependency, got: %v", err)
	}
}

func TestNewScheduler_DuplicatePhaseID(t *testing.T) {
	t.Parallel()

	// Two phases with the same ID.
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "a"},
	}

	_, err := NewScheduler(phases)
	if err == nil {
		t.Fatal("expected error for duplicate phase ID, got nil")
	}
	if !strings.Contains(err.Error(), "adding task") {
		t.Errorf("error should mention 'adding task', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("error should mention phase ID %q, got: %v", "a", err)
	}
}

func TestAllPending(t *testing.T) {
	t.Parallel()

	t.Run("returns all non-done phases", func(t *testing.T) {
		t.Parallel()
		// a -> b -> c (chain)
		phases := []PhaseSpec{
			{ID: "a", Priority: 1},
			{ID: "b", Priority: 2, DependsOn: []string{"a"}},
			{ID: "c", Priority: 3, DependsOn: []string{"b"}},
		}

		s, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler failed: %v", err)
		}

		// With nothing done, all phases are pending.
		pending := s.AllPending(map[string]bool{})
		if len(pending) != 3 {
			t.Fatalf("expected 3 pending, got %d: %v", len(pending), pending)
		}

		// Unlike ReadyTasks, AllPending includes phases with unsatisfied deps.
		ready := s.ReadyTasks(map[string]bool{})
		if len(ready) != 1 {
			t.Fatalf("expected 1 ready, got %d", len(ready))
		}
		// AllPending returns b and c even though their deps aren't done.
		pendingSet := make(map[string]bool)
		for _, id := range pending {
			pendingSet[id] = true
		}
		if !pendingSet["b"] || !pendingSet["c"] {
			t.Errorf("expected b and c in pending despite unsatisfied deps, got %v", pending)
		}
	})

	t.Run("excludes done phases", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "a"},
			{ID: "b"},
			{ID: "c"},
		}

		s, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler failed: %v", err)
		}

		done := map[string]bool{"a": true, "c": true}
		pending := s.AllPending(done)
		if len(pending) != 1 || pending[0] != "b" {
			t.Errorf("expected [b] pending, got %v", pending)
		}
	})

	t.Run("returns empty when all done", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "a"},
			{ID: "b"},
		}

		s, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler failed: %v", err)
		}

		done := map[string]bool{"a": true, "b": true}
		pending := s.AllPending(done)
		if len(pending) != 0 {
			t.Errorf("expected empty pending, got %v", pending)
		}
	})

	t.Run("sorted by impact score descending", func(t *testing.T) {
		t.Parallel()
		// Diamond: a and b are roots, c depends on both.
		// a and b should have higher impact scores than c (they block c).
		phases := []PhaseSpec{
			{ID: "a", Priority: 1},
			{ID: "b", Priority: 1},
			{ID: "c", Priority: 1, DependsOn: []string{"a", "b"}},
		}

		s, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler failed: %v", err)
		}

		pending := s.AllPending(map[string]bool{})
		if len(pending) != 3 {
			t.Fatalf("expected 3 pending, got %d", len(pending))
		}

		scores := s.ImpactScores()
		for i := 1; i < len(pending); i++ {
			if scores[pending[i-1]] < scores[pending[i]] {
				t.Errorf("pending not sorted by impact: %q (%.4f) < %q (%.4f)",
					pending[i-1], scores[pending[i-1]], pending[i], scores[pending[i]])
			}
		}
	})
}

func TestEffectiveParallelism(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phases     []PhaseSpec
		waveIDs    []string
		maxWorkers int
		want       int
	}{
		{
			name: "three independent non-overlapping phases",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"cmd/"}},
				{ID: "b", Scope: []string{"internal/loop/"}},
				{ID: "c", Scope: []string{"internal/agent/"}},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       3,
		},
		{
			name: "three phases two overlapping scopes",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"internal/"}},
				{ID: "b", Scope: []string{"internal/loop/"}},
				{ID: "c", Scope: []string{"cmd/"}},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       2,
		},
		{
			name: "single phase",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"cmd/"}},
			},
			waveIDs:    []string{"a"},
			maxWorkers: 10,
			want:       1,
		},
		{
			name: "max workers caps parallelism",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"dir1/"}},
				{ID: "b", Scope: []string{"dir2/"}},
				{ID: "c", Scope: []string{"dir3/"}},
				{ID: "d", Scope: []string{"dir4/"}},
				{ID: "e", Scope: []string{"dir5/"}},
			},
			waveIDs:    []string{"a", "b", "c", "d", "e"},
			maxWorkers: 2,
			want:       2,
		},
		{
			name: "allow scope overlap does not reduce parallelism",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"internal/"}, AllowScopeOverlap: true},
				{ID: "b", Scope: []string{"internal/loop/"}},
				{ID: "c", Scope: []string{"cmd/"}},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       3,
		},
		{
			name: "connected phases are not conflicts",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"internal/"}, DependsOn: []string{"b"}},
				{ID: "b", Scope: []string{"internal/loop/"}},
				{ID: "c", Scope: []string{"cmd/"}},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       3,
		},
		{
			name: "empty wave",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"cmd/"}},
			},
			waveIDs:    []string{},
			maxWorkers: 10,
			want:       0,
		},
		{
			name: "phases with no scopes have no overlap",
			phases: []PhaseSpec{
				{ID: "a"},
				{ID: "b"},
				{ID: "c"},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       3,
		},
		{
			name: "all phases overlap — serialized to 1",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"**/*"}},
				{ID: "b", Scope: []string{"**/*"}},
				{ID: "c", Scope: []string{"**/*"}},
			},
			waveIDs:    []string{"a", "b", "c"},
			maxWorkers: 10,
			want:       1,
		},
		{
			name: "only one side has AllowScopeOverlap",
			phases: []PhaseSpec{
				{ID: "a", Scope: []string{"internal/"}, AllowScopeOverlap: false},
				{ID: "b", Scope: []string{"internal/"}, AllowScopeOverlap: true},
			},
			waveIDs:    []string{"a", "b"},
			maxWorkers: 10,
			want:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, _ := phasesToDAG(tt.phases)
			wave := Wave{Number: 1, NodeIDs: tt.waveIDs}
			got := EffectiveParallelism(wave, tt.phases, d, tt.maxWorkers)
			if got != tt.want {
				t.Errorf("EffectiveParallelism() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWaveParallelism(t *testing.T) {
	t.Parallel()

	// Setup: wave 1 has a,b,c (no deps); wave 2 has d,e (depend on wave 1).
	// a and b overlap scopes, c and d are independent, e overlaps with d.
	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"internal/"}},
		{ID: "b", Scope: []string{"internal/loop/"}},
		{ID: "c", Scope: []string{"cmd/"}},
		{ID: "d", Scope: []string{"docs/"}, DependsOn: []string{"a"}},
		{ID: "e", Scope: []string{"docs/"}, DependsOn: []string{"b"}},
	}
	d, _ := phasesToDAG(phases)

	waves := []Wave{
		{Number: 1, NodeIDs: []string{"a", "b", "c"}},
		{Number: 2, NodeIDs: []string{"d", "e"}},
	}

	got := WaveParallelism(waves, phases, d, 10)
	if len(got) != 2 {
		t.Fatalf("WaveParallelism() returned %d values, want 2", len(got))
	}

	// Wave 1: a and b overlap (internal/ and internal/loop/), c is independent.
	// Greedy: pick a (no conflicts), skip b (conflicts with a), pick c → independent set = {a, c} = 2
	if got[0] != 2 {
		t.Errorf("wave 1 parallelism = %d, want 2", got[0])
	}

	// Wave 2: d and e both have docs/ scope → conflict → 1
	if got[1] != 1 {
		t.Errorf("wave 2 parallelism = %d, want 1", got[1])
	}
}
