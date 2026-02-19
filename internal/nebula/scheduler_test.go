package nebula

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
