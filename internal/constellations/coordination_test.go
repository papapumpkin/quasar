package constellations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// fakeEntanglementReader returns a fixed set of active entanglements, recording
// whether it was consulted.
type fakeEntanglementReader struct {
	ents []fabric.Entanglement
	err  error
}

func (f *fakeEntanglementReader) ActiveAll(context.Context) ([]fabric.Entanglement, error) {
	return f.ents, f.err
}

// writeFile creates a file under dir with the given contents and returns its path.
func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestCheckNotesSymbolMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caller := writeFile(t, dir, "caller.go", "func use() { Poll(ctx, nil) }")

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "Poll", Status: fabric.StatusInFlight, RunID: "run-sibling", PhaseID: "p-sibling", CurrentSignature: "Poll(ctx) error", InFlightAt: 100},
		{Name: "Unrelated", Status: fabric.StatusInFlight, RunID: "run-sibling", PhaseID: "p-sibling", InFlightAt: 100},
	}}
	check := &Check{Store: store}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:   "run-self",
		PhaseID: "p-self",
		Files:   []string{caller},
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note (symbol match), got %d: %+v", len(notes), notes)
	}
	if notes[0].Name != "Poll" || notes[0].CurrentSignature != "Poll(ctx) error" {
		t.Errorf("unexpected note: %+v", notes[0])
	}
	if notes[0].Advice == "" {
		t.Error("expected an advice string for the note")
	}
}

func TestCheckNotesPackageMatch(t *testing.T) {
	t.Parallel()

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "Scheduler", Package: "internal/runtime", Status: fabric.StatusDeclared, RunID: "run-sibling", PhaseID: "p-sibling", DeclaredAt: 50},
		{Name: "Elsewhere", Package: "internal/other", Status: fabric.StatusDeclared, RunID: "run-sibling", PhaseID: "p-sibling", DeclaredAt: 50},
	}}
	check := &Check{Store: store}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:   "run-self",
		PhaseID: "p-self",
		Scope:   []string{"internal/runtime/**"},
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Name != "Scheduler" {
		t.Fatalf("expected 1 package-match note for Scheduler, got %+v", notes)
	}
}

func TestCheckNotesSelfExclusion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caller := writeFile(t, dir, "caller.go", "Foo Bar Baz")

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "Foo", Status: fabric.StatusInFlight, RunID: "run-self", PhaseID: "p-other", InFlightAt: 100}, // same run
		{Name: "Bar", Status: fabric.StatusInFlight, RunID: "", PhaseID: "p-self", InFlightAt: 100},          // same phase, NULL run
		{Name: "Baz", Status: fabric.StatusInFlight, RunID: "run-sibling", PhaseID: "p-sibling", InFlightAt: 100},
	}}
	check := &Check{Store: store}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:       "run-self",
		PhaseID:     "p-self",
		Files:       []string{caller},
		SelfSymbols: []string{"Baz"}, // declared by self → excluded even though run/phase differ
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected all notes self-excluded, got %+v", notes)
	}
}

func TestCheckNotesRecencyOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caller := writeFile(t, dir, "caller.go", "older newer")

	// ActiveAll is the recency source of truth; a real store orders by recency.
	// The Check must preserve whatever order ActiveAll returns.
	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "newer", Status: fabric.StatusInFlight, RunID: "r", PhaseID: "p1", InFlightAt: 300},
		{Name: "older", Status: fabric.StatusDeclared, RunID: "r2", PhaseID: "p2", DeclaredAt: 60},
	}}
	check := &Check{Store: store}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:   "run-self",
		PhaseID: "p-self",
		Files:   []string{caller},
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 2 || notes[0].Name != "newer" || notes[1].Name != "older" {
		t.Fatalf("expected recency order [newer, older], got %+v", notes)
	}
}

func TestCheckNotesOverrideSuppression(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caller := writeFile(t, dir, "caller.go", "FromTicket Poll")
	logPath := filepath.Join(dir, "coordination_log.jsonl")
	log := telemetry.NewCoordinationLog(logPath)

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "FromTicket", Status: fabric.StatusDeprecated, RunID: "r", PhaseID: "p1"},
		{Name: "Poll", Status: fabric.StatusInFlight, RunID: "r", PhaseID: "p1", CurrentSignature: "Poll(ctx)"},
	}}
	check := &Check{Store: store, Log: log}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:              "run-self",
		PhaseID:            "p-self",
		Files:              []string{caller},
		IgnoreDeprecations: []string{"FromTicket"},
		IgnoreSignatures:   []string{"Poll"},
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected both notes suppressed by override, got %+v", notes)
	}

	events, err := log.ReadSince(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	overrides := 0
	checks := 0
	for _, e := range events {
		switch e.Type {
		case telemetry.CoordinationEventOverride:
			overrides++
		case telemetry.CoordinationEventCheck:
			checks++
		}
	}
	if overrides != 2 {
		t.Errorf("expected 2 override rows, got %d", overrides)
	}
	if checks != 1 {
		t.Errorf("expected exactly 1 summary check row, got %d", checks)
	}
}

func TestCheckNotesOneSummaryRowPerCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caller := writeFile(t, dir, "caller.go", "Poll")
	logPath := filepath.Join(dir, "coordination_log.jsonl")
	log := telemetry.NewCoordinationLog(logPath)

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{Name: "Poll", Status: fabric.StatusInFlight, RunID: "r", PhaseID: "p1", InFlightAt: 1},
	}}
	check := &Check{Store: store, Log: log}

	for i := 0; i < 3; i++ {
		if _, err := check.Notes(context.Background(), PhaseContext{RunID: "self", PhaseID: "p-self", Files: []string{caller}}); err != nil {
			t.Fatalf("Notes: %v", err)
		}
	}

	events, err := log.ReadSince(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	checks := 0
	for _, e := range events {
		if e.Type == telemetry.CoordinationEventCheck {
			checks++
			if e.NotesCount != 1 || e.ByStatus["in_flight"] != 1 {
				t.Errorf("unexpected summary row: %+v", e)
			}
		}
	}
	if checks != 3 {
		t.Errorf("expected one summary row per check (3), got %d", checks)
	}
}

func TestCheckNotesIntegrationWithRenderer(t *testing.T) {
	t.Parallel()

	// Two fixture phases with overlapping scope: phase A's coder marked Poll
	// in_flight; phase B's coder file calls Poll. B's pre-flight must surface a
	// note about A's in-flight symbol, and the rendered prompt must carry it.
	dir := t.TempDir()
	phaseBFile := writeFile(t, dir, "consumer.go", "func run() { Poll(ctx, cursor) }")

	store := &fakeEntanglementReader{ents: []fabric.Entanglement{
		{
			Name:             "Poll",
			Status:           fabric.StatusInFlight,
			RunID:            "run-A",
			PhaseID:          "phase-A",
			CurrentSignature: "Poll(ctx, cursor) ([]Event, error)",
			InFlightAt:       1000,
		},
	}}
	check := &Check{Store: store}

	notes, err := check.Notes(context.Background(), PhaseContext{
		RunID:   "run-B",
		PhaseID: "phase-B",
		Files:   []string{phaseBFile},
	})
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected phase B to see phase A's in-flight Poll, got %+v", notes)
	}

	prompt := agent.AppendCoordinationNotes("# Phase B brief", notes)
	for _, want := range []string{
		"## Coordination notes",
		"**Poll** (in_flight, phase `phase-A`)",
		"Poll(ctx, cursor) ([]Event, error)",
		"Use the current signature shown above.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("rendered prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCheckNotesNilStoreAndError(t *testing.T) {
	t.Parallel()

	t.Run("nil check", func(t *testing.T) {
		t.Parallel()
		var c *Check
		notes, err := c.Notes(context.Background(), PhaseContext{})
		if err != nil || notes != nil {
			t.Errorf("nil check should yield (nil, nil), got %v, %v", notes, err)
		}
	})

	t.Run("nil store", func(t *testing.T) {
		t.Parallel()
		c := &Check{}
		notes, err := c.Notes(context.Background(), PhaseContext{})
		if err != nil || notes != nil {
			t.Errorf("nil store should yield (nil, nil), got %v, %v", notes, err)
		}
	})

	t.Run("store error propagates", func(t *testing.T) {
		t.Parallel()
		c := &Check{Store: &fakeEntanglementReader{err: context.Canceled}}
		_, err := c.Notes(context.Background(), PhaseContext{})
		if err == nil {
			t.Error("expected store error to propagate so caller can log it")
		}
	})
}
