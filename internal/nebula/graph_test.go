package nebula

import "testing"

func TestHasPath(t *testing.T) {
	t.Parallel()

	// Graph: a → b → c (a depends on b, b depends on c)
	phases := []PhaseSpec{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"c"}},
		{ID: "c"},
		{ID: "d"}, // isolated node
	}
	g := NewGraph(phases)

	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"direct dependency", "a", "b", true},
		{"transitive dependency", "a", "c", true},
		{"no reverse path", "b", "a", false},
		{"no reverse transitive", "c", "a", false},
		{"same node", "a", "a", false},
		{"isolated to connected", "d", "a", false},
		{"connected to isolated", "a", "d", false},
		{"between isolated nodes", "d", "d", false},
		{"unknown from", "x", "a", false},
		{"unknown to", "a", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := g.HasPath(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("HasPath(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestConnected(t *testing.T) {
	t.Parallel()

	// Graph: a → b → c, d isolated
	phases := []PhaseSpec{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"c"}},
		{ID: "c"},
		{ID: "d"},
	}
	g := NewGraph(phases)

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"forward", "a", "b", true},
		{"reverse", "b", "a", true},
		{"forward transitive", "a", "c", true},
		{"reverse transitive", "c", "a", true},
		{"unconnected", "a", "d", false},
		{"same node", "a", "a", false},
		{"both isolated", "d", "d", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := g.Connected(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Connected(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestConnected_DisconnectedComponents(t *testing.T) {
	t.Parallel()

	// Two separate components: a → b, c → d
	phases := []PhaseSpec{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"d"}},
		{ID: "d"},
	}
	g := NewGraph(phases)

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"same component forward", "a", "b", true},
		{"same component reverse", "b", "a", true},
		{"cross component a-c", "a", "c", false},
		{"cross component c-a", "c", "a", false},
		{"cross component a-d", "a", "d", false},
		{"cross component b-c", "b", "c", false},
		{"within second component", "c", "d", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := g.Connected(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Connected(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestHasPath_DisconnectedComponents(t *testing.T) {
	t.Parallel()

	// Two separate components: a → b, c → d
	phases := []PhaseSpec{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"d"}},
		{ID: "d"},
	}
	g := NewGraph(phases)

	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"within first component", "a", "b", true},
		{"cross component a-c", "a", "c", false},
		{"cross component a-d", "a", "d", false},
		{"cross component c-b", "c", "b", false},
		{"within second component", "c", "d", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := g.HasPath(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("HasPath(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
