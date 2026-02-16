package nebula

import "testing"

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
			graph := NewGraph(tt.phases)
			wave := Wave{Number: 1, PhaseIDs: tt.waveIDs}
			got := EffectiveParallelism(wave, tt.phases, graph, tt.maxWorkers)
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
	graph := NewGraph(phases)

	waves := []Wave{
		{Number: 1, PhaseIDs: []string{"a", "b", "c"}},
		{Number: 2, PhaseIDs: []string{"d", "e"}},
	}

	got := WaveParallelism(waves, phases, graph, 10)
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
