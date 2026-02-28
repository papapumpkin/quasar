package tui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func TestEntanglementView_EmptyRendersPlaceholder(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Width = 80

	view := ev.View()

	if !strings.Contains(view, "No entanglements") {
		t.Errorf("expected placeholder text, got: %q", view)
	}
}

func TestEntanglementView_CardRendersID(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 42, Name: "FooService", Producer: "phase-a", Consumer: "phase-b", Status: fabric.StatusPending, Signature: "func Foo() error"},
	}
	ev.SetSize(80, 40)

	view := ev.View()

	if !strings.Contains(view, "#42") {
		t.Errorf("expected entanglement ID #42 in view, got: %q", view)
	}
	if !strings.Contains(view, "FooService") {
		t.Errorf("expected entanglement name FooService in view, got: %q", view)
	}
}

func TestEntanglementView_CardRendersParties(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "Svc", Producer: "alpha", Consumer: "beta", Status: fabric.StatusFulfilled, Signature: "type Svc interface{}"},
	}
	ev.SetSize(80, 40)

	view := ev.View()

	if !strings.Contains(view, "alpha") {
		t.Errorf("expected producer 'alpha' in view")
	}
	if !strings.Contains(view, "beta") {
		t.Errorf("expected consumer 'beta' in view")
	}
	if !strings.Contains(view, "→") {
		t.Errorf("expected arrow '→' between parties in view")
	}
}

func TestEntanglementView_NilConsumerRendersWildcard(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "Svc", Producer: "alpha", Consumer: "", Status: fabric.StatusPending, Signature: "type X struct{}"},
	}
	ev.SetSize(80, 40)

	view := ev.View()

	// Consumer should render as "*" when empty.
	if !strings.Contains(view, "→ *") {
		t.Errorf("expected wildcard consumer '→ *' in view, got: %q", view)
	}
}

func TestEntanglementView_StatusColors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		label  string
	}{
		{"pending", fabric.StatusPending, "pending"},
		{"fulfilled", fabric.StatusFulfilled, "fulfilled"},
		{"disputed", fabric.StatusDisputed, "disputed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ev := NewEntanglementView()
			ev.Entanglements = []fabric.Entanglement{
				{ID: 1, Name: "X", Producer: "p", Status: tt.status, Signature: "func()"},
			}
			ev.SetSize(80, 40)

			view := ev.View()

			if !strings.Contains(view, tt.label) {
				t.Errorf("expected status label %q in view", tt.label)
			}
		})
	}
}

func TestEntanglementView_InterfaceBodyRendered(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "X", Producer: "p", Status: fabric.StatusPending, Signature: "func DoWork(ctx context.Context) error"},
	}
	ev.SetSize(120, 40)

	view := ev.View()

	if !strings.Contains(view, "func DoWork") {
		t.Errorf("expected interface body in view, got: %q", view)
	}
}

func TestEntanglementView_GroupedByProducer(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "A", Producer: "phase-b", Status: fabric.StatusPending, Signature: "func A()"},
		{ID: 2, Name: "B", Producer: "phase-a", Status: fabric.StatusFulfilled, Signature: "func B()"},
		{ID: 3, Name: "C", Producer: "phase-b", Status: fabric.StatusFulfilled, Signature: "func C()"},
	}
	ev.SetSize(80, 80)

	view := ev.View()

	// Both producer headers should appear.
	if !strings.Contains(view, "phase-a") {
		t.Errorf("expected producer header 'phase-a' in view")
	}
	if !strings.Contains(view, "phase-b") {
		t.Errorf("expected producer header 'phase-b' in view")
	}

	// phase-a should appear before phase-b (alphabetical order).
	idxA := strings.Index(view, "◆ phase-a")
	idxB := strings.Index(view, "◆ phase-b")
	if idxA < 0 || idxB < 0 {
		t.Fatalf("expected both producer headers with ◆ prefix")
	}
	if idxA >= idxB {
		t.Errorf("expected phase-a group before phase-b group (alphabetical)")
	}
}

func TestEntanglementView_SortedByStatusWithinGroup(t *testing.T) {
	t.Parallel()

	groups := groupEntanglements([]fabric.Entanglement{
		{ID: 1, Name: "A", Producer: "p", Status: fabric.StatusFulfilled, Signature: "func A()"},
		{ID: 2, Name: "B", Producer: "p", Status: fabric.StatusDisputed, Signature: "func B()"},
		{ID: 3, Name: "C", Producer: "p", Status: fabric.StatusPending, Signature: "func C()"},
	})

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if len(g.Entanglements) != 3 {
		t.Fatalf("expected 3 entanglements in group, got %d", len(g.Entanglements))
	}

	// Order should be: disputed, pending, fulfilled.
	if g.Entanglements[0].Status != fabric.StatusDisputed {
		t.Errorf("expected first entanglement to be disputed, got %q", g.Entanglements[0].Status)
	}
	if g.Entanglements[1].Status != fabric.StatusPending {
		t.Errorf("expected second entanglement to be pending, got %q", g.Entanglements[1].Status)
	}
	if g.Entanglements[2].Status != fabric.StatusFulfilled {
		t.Errorf("expected third entanglement to be fulfilled, got %q", g.Entanglements[2].Status)
	}
}

func TestEntanglementView_CursorNavigation(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "A", Producer: "p", Status: fabric.StatusPending, Signature: "func A()"},
		{ID: 2, Name: "B", Producer: "p", Status: fabric.StatusFulfilled, Signature: "func B()"},
		{ID: 3, Name: "C", Producer: "q", Status: fabric.StatusDisputed, Signature: "func C()"},
	}
	ev.SetSize(80, 40)

	// Initial cursor at 0.
	if ev.Cursor != 0 {
		t.Errorf("expected initial cursor 0, got %d", ev.Cursor)
	}

	// MoveDown increments cursor.
	ev.MoveDown()
	if ev.Cursor != 1 {
		t.Errorf("expected cursor 1 after MoveDown, got %d", ev.Cursor)
	}

	// MoveDown again.
	ev.MoveDown()
	if ev.Cursor != 2 {
		t.Errorf("expected cursor 2 after second MoveDown, got %d", ev.Cursor)
	}

	// MoveDown at end stays at max.
	ev.MoveDown()
	if ev.Cursor != 2 {
		t.Errorf("expected cursor clamped at 2, got %d", ev.Cursor)
	}

	// MoveUp decrements.
	ev.MoveUp()
	if ev.Cursor != 1 {
		t.Errorf("expected cursor 1 after MoveUp, got %d", ev.Cursor)
	}

	// MoveUp to 0.
	ev.MoveUp()
	if ev.Cursor != 0 {
		t.Errorf("expected cursor 0 after MoveUp, got %d", ev.Cursor)
	}

	// MoveUp at 0 stays at 0.
	ev.MoveUp()
	if ev.Cursor != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", ev.Cursor)
	}
}

func TestEntanglementView_ClampCursor(t *testing.T) {
	t.Parallel()

	t.Run("empty list clamps to zero", func(t *testing.T) {
		t.Parallel()
		ev := EntanglementView{Cursor: 5}
		ev.ClampCursor()
		if ev.Cursor != 0 {
			t.Errorf("expected cursor 0 for empty list, got %d", ev.Cursor)
		}
	})

	t.Run("cursor beyond end clamps to last", func(t *testing.T) {
		t.Parallel()
		ev := EntanglementView{
			Cursor: 10,
			Entanglements: []fabric.Entanglement{
				{ID: 1, Producer: "p", Status: fabric.StatusPending},
				{ID: 2, Producer: "p", Status: fabric.StatusFulfilled},
			},
		}
		ev.ClampCursor()
		if ev.Cursor != 1 {
			t.Errorf("expected cursor 1 (last index), got %d", ev.Cursor)
		}
	})

	t.Run("negative cursor clamps to zero", func(t *testing.T) {
		t.Parallel()
		ev := EntanglementView{
			Cursor: -3,
			Entanglements: []fabric.Entanglement{
				{ID: 1, Producer: "p", Status: fabric.StatusPending},
			},
		}
		ev.ClampCursor()
		if ev.Cursor != 0 {
			t.Errorf("expected cursor 0, got %d", ev.Cursor)
		}
	})
}

func TestGroupEntanglements_Empty(t *testing.T) {
	t.Parallel()
	groups := groupEntanglements(nil)
	if groups != nil {
		t.Errorf("expected nil groups for empty input, got %v", groups)
	}
}

func TestGroupEntanglements_MultipleProducers(t *testing.T) {
	t.Parallel()
	groups := groupEntanglements([]fabric.Entanglement{
		{ID: 1, Producer: "beta", Status: fabric.StatusPending},
		{ID: 2, Producer: "alpha", Status: fabric.StatusFulfilled},
		{ID: 3, Producer: "beta", Status: fabric.StatusDisputed},
	})

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Alphabetical order: alpha before beta.
	if groups[0].Producer != "alpha" {
		t.Errorf("expected first group 'alpha', got %q", groups[0].Producer)
	}
	if groups[1].Producer != "beta" {
		t.Errorf("expected second group 'beta', got %q", groups[1].Producer)
	}

	// beta group should have disputed before pending.
	if len(groups[1].Entanglements) != 2 {
		t.Fatalf("expected 2 entanglements in beta group, got %d", len(groups[1].Entanglements))
	}
	if groups[1].Entanglements[0].Status != fabric.StatusDisputed {
		t.Errorf("expected first in beta to be disputed, got %q", groups[1].Entanglements[0].Status)
	}
}

func TestStatusPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   string
		expected int
	}{
		{fabric.StatusDisputed, 0},
		{fabric.StatusPending, 1},
		{fabric.StatusFulfilled, 2},
		{"unknown", 3},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			got := statusPriority(tt.status)
			if got != tt.expected {
				t.Errorf("statusPriority(%q) = %d, want %d", tt.status, got, tt.expected)
			}
		})
	}
}

func TestStatusColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   string
		expected string
	}{
		{fabric.StatusDisputed, string(colorDanger)},
		{fabric.StatusPending, string(colorStarYellow)},
		{fabric.StatusFulfilled, string(colorSuccess)},
		{"unknown", string(colorMuted)},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			got := string(statusColor(tt.status))
			if got != tt.expected {
				t.Errorf("statusColor(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

func TestFlatCount(t *testing.T) {
	t.Parallel()

	groups := []entanglementGroup{
		{Producer: "a", Entanglements: make([]fabric.Entanglement, 3)},
		{Producer: "b", Entanglements: make([]fabric.Entanglement, 2)},
	}

	got := flatCount(groups)
	if got != 5 {
		t.Errorf("flatCount = %d, want 5", got)
	}
}

func TestFlatCount_Empty(t *testing.T) {
	t.Parallel()
	got := flatCount(nil)
	if got != 0 {
		t.Errorf("flatCount(nil) = %d, want 0", got)
	}
}

func TestEntanglementView_MultilineSignature(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{
			ID:       1,
			Name:     "Multi",
			Producer: "p",
			Status:   fabric.StatusPending,
			Signature: "type Handler interface {\n" +
				"\tHandle(ctx context.Context) error\n" +
				"}",
		},
	}
	ev.SetSize(120, 40)

	view := ev.View()

	if !strings.Contains(view, "Handler") {
		t.Errorf("expected multiline signature in view")
	}
	if !strings.Contains(view, "Handle") {
		t.Errorf("expected method from multiline signature in view")
	}
}

func TestEntanglementView_EmptySignatureFallback(t *testing.T) {
	t.Parallel()
	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Name: "X", Producer: "p", Status: fabric.StatusPending, Signature: ""},
	}
	ev.SetSize(80, 40)

	view := ev.View()

	if !strings.Contains(view, "(no signature)") {
		t.Errorf("expected fallback text for empty signature, got: %q", view)
	}
}
