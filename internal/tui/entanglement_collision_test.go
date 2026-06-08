package tui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func TestEntanglementViewRendersCollisions(t *testing.T) {
	t.Parallel()

	ev := NewEntanglementView()
	ev.Collisions = []EntanglementCollision{
		{Scope: "internal/runtime/**", PhaseID: "02-budget", OtherPhaseID: "01-cycle"},
	}
	ev.SetSize(80, 20)

	content := ev.renderContent()
	for _, want := range []string{"scope collision", "02-budget", "01-cycle", "internal/runtime/**"} {
		if !strings.Contains(content, want) {
			t.Errorf("rendered content missing %q\n---\n%s", want, content)
		}
	}
}

func TestEntanglementViewCollisionsWithoutEntanglements(t *testing.T) {
	t.Parallel()

	ev := NewEntanglementView()
	ev.Collisions = []EntanglementCollision{
		{Scope: "internal/runtime/engine.go", PhaseID: "b", OtherPhaseID: "a"},
	}

	// With collisions present, the view must not report "No entanglements".
	if got := ev.View(); strings.Contains(got, "No entanglements") {
		t.Errorf("View() reported no entanglements despite active collisions: %q", got)
	}

	ev.SetSize(60, 10)
	if !strings.Contains(ev.renderContent(), "scope collision") {
		t.Error("collision warning not rendered when no entanglements present")
	}
}

func TestEntanglementViewNoCollisionsRendersCleanly(t *testing.T) {
	t.Parallel()

	ev := NewEntanglementView()
	ev.Entanglements = []fabric.Entanglement{
		{ID: 1, Producer: "a", Kind: fabric.KindFunction, Name: "DoThing", Status: fabric.StatusFulfilled},
	}
	ev.SetSize(80, 20)

	if strings.Contains(ev.renderContent(), "scope collision") {
		t.Error("collision warning rendered when there are no collisions")
	}
}
