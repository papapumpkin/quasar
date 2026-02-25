package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// mockFabric implements fabric.Fabric for testing prompt injection.
type mockFabric struct {
	entanglements []fabric.Entanglement
	claims        []fabric.Claim
	phaseStates   map[string]string
	discoveries   []fabric.Discovery
	pulses        []fabric.Pulse
}

func (m *mockFabric) SetPhaseState(context.Context, string, string) error   { return nil }
func (m *mockFabric) GetPhaseState(context.Context, string) (string, error) { return "", nil }
func (m *mockFabric) AllPhaseStates(context.Context) (map[string]string, error) {
	return m.phaseStates, nil
}
func (m *mockFabric) PublishEntanglement(context.Context, fabric.Entanglement) error { return nil }
func (m *mockFabric) PublishEntanglements(context.Context, []fabric.Entanglement) error {
	return nil
}
func (m *mockFabric) EntanglementsFor(context.Context, string) ([]fabric.Entanglement, error) {
	return nil, nil
}
func (m *mockFabric) AllEntanglements(context.Context) ([]fabric.Entanglement, error) {
	return m.entanglements, nil
}
func (m *mockFabric) ClaimFile(context.Context, string, string) error        { return nil }
func (m *mockFabric) ReleaseClaims(context.Context, string) error            { return nil }
func (m *mockFabric) ReleaseFileClaim(context.Context, string, string) error { return nil }
func (m *mockFabric) FileOwner(context.Context, string) (string, error)      { return "", nil }
func (m *mockFabric) ClaimsFor(context.Context, string) ([]string, error)    { return nil, nil }
func (m *mockFabric) AllClaims(context.Context) ([]fabric.Claim, error) {
	return m.claims, nil
}
func (m *mockFabric) PostDiscovery(context.Context, fabric.Discovery) (int64, error) { return 0, nil }
func (m *mockFabric) Discoveries(context.Context, string) ([]fabric.Discovery, error) {
	return nil, nil
}
func (m *mockFabric) AllDiscoveries(context.Context) ([]fabric.Discovery, error) { return nil, nil }
func (m *mockFabric) ResolveDiscovery(context.Context, int64) error              { return nil }
func (m *mockFabric) UnresolvedDiscoveries(context.Context) ([]fabric.Discovery, error) {
	return m.discoveries, nil
}
func (m *mockFabric) EmitPulse(context.Context, fabric.Pulse) error { return nil }
func (m *mockFabric) PulsesFor(context.Context, string) ([]fabric.Pulse, error) {
	return nil, nil
}
func (m *mockFabric) AllPulses(context.Context) ([]fabric.Pulse, error) {
	return m.pulses, nil
}
func (m *mockFabric) PurgeAll(context.Context) error                   { return nil }
func (m *mockFabric) PurgeFulfilledEntanglements(context.Context) error { return nil }
func (m *mockFabric) Close() error                                      { return nil }

func TestPrependFabricContext(t *testing.T) {
	t.Parallel()

	t.Run("prepends snapshot before description", func(t *testing.T) {
		t.Parallel()
		desc := "Implement feature X."
		snap := fabric.Snapshot{
			Entanglements: []fabric.Entanglement{
				{Producer: "phase-a", Kind: "interface", Name: "Foo", Package: "pkg/foo"},
			},
			FileClaims: map[string]string{
				"internal/bar.go": "phase-b",
			},
			Completed:  []string{"phase-c"},
			InProgress: []string{"phase-d"},
		}
		got := PrependFabricContext(desc, snap)

		if !strings.HasPrefix(got, "## Current Fabric State") {
			t.Error("expected result to start with fabric state header")
		}
		if !strings.Contains(got, "Implement feature X.") {
			t.Error("expected original description to be preserved")
		}
		if !strings.Contains(got, "phase-a") {
			t.Error("expected entanglement producer in output")
		}
		if !strings.Contains(got, "internal/bar.go") {
			t.Error("expected file claim in output")
		}
		if !strings.Contains(got, "phase-c") {
			t.Error("expected completed phase in output")
		}
		if !strings.Contains(got, "---") {
			t.Error("expected separator between fabric state and description")
		}
		// Description must come after the separator.
		sepIdx := strings.Index(got, "---")
		descIdx := strings.Index(got, "Implement feature X.")
		if descIdx < sepIdx {
			t.Error("description should appear after the separator")
		}
	})

	t.Run("empty snapshot still prepends header", func(t *testing.T) {
		t.Parallel()
		desc := "Do something."
		snap := fabric.Snapshot{}
		got := PrependFabricContext(desc, snap)

		if !strings.HasPrefix(got, "## Current Fabric State") {
			t.Error("expected fabric state header even with empty snapshot")
		}
		if !strings.Contains(got, "Do something.") {
			t.Error("expected original description to be preserved")
		}
	})
}

func TestBuildCoderPromptFabricIntegration(t *testing.T) {
	t.Parallel()

	t.Run("fabric disabled does not inject protocol", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:   "Base coder prompt.",
			FabricEnabled: false,
		}
		state := &CycleState{
			TaskBeadID: "test-1",
			TaskTitle:  "test task",
			Cycle:      1,
		}
		got := l.buildCoderPrompt(state)
		if strings.Contains(got, "Fabric Protocol") {
			t.Error("fabric protocol should not appear when FabricEnabled is false")
		}
	})

	t.Run("coderAgent includes protocol when fabric enabled", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:   "Base coder prompt.",
			FabricEnabled: true,
			TaskID:        "phase-x",
		}
		ag := l.coderAgent(5.0)
		if !strings.Contains(ag.SystemPrompt, "## Fabric Protocol") {
			t.Error("expected fabric protocol in system prompt when enabled")
		}
		if !strings.Contains(ag.SystemPrompt, "Base coder prompt.") {
			t.Error("expected base prompt to be preserved")
		}
	})

	t.Run("coderAgent omits protocol when fabric disabled", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:   "Base coder prompt.",
			FabricEnabled: false,
		}
		ag := l.coderAgent(5.0)
		if strings.Contains(ag.SystemPrompt, "Fabric Protocol") {
			t.Error("fabric protocol should not appear when FabricEnabled is false")
		}
		if ag.SystemPrompt != "Base coder prompt." {
			t.Errorf("expected base prompt only, got: %s", ag.SystemPrompt)
		}
	})
}

func TestBuildFabricSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("builds snapshot from fabric store", func(t *testing.T) {
		t.Parallel()
		mf := &mockFabric{
			entanglements: []fabric.Entanglement{
				{Producer: "phase-a", Kind: "interface", Name: "Foo", Package: "pkg/foo"},
			},
			claims: []fabric.Claim{
				{Filepath: "internal/bar.go", OwnerTask: "phase-b"},
			},
			phaseStates: map[string]string{
				"phase-a": fabric.StateDone,
				"phase-b": fabric.StateRunning,
				"phase-c": fabric.StateQueued,
			},
			discoveries: []fabric.Discovery{
				{SourceTask: "phase-a", Kind: fabric.DiscoveryFileConflict, Detail: "conflict in foo.go"},
			},
			pulses: []fabric.Pulse{
				{TaskID: "phase-a", Kind: fabric.PulseNote, Content: "hello"},
			},
		}
		l := &Loop{Fabric: mf, FabricEnabled: true}
		snap := l.buildFabricSnapshot(context.Background())

		if len(snap.Entanglements) != 1 {
			t.Errorf("expected 1 entanglement, got %d", len(snap.Entanglements))
		}
		if snap.FileClaims["internal/bar.go"] != "phase-b" {
			t.Error("expected file claim for internal/bar.go owned by phase-b")
		}
		if len(snap.Completed) != 1 || snap.Completed[0] != "phase-a" {
			t.Errorf("expected completed=[phase-a], got %v", snap.Completed)
		}
		if len(snap.InProgress) != 1 || snap.InProgress[0] != "phase-b" {
			t.Errorf("expected inProgress=[phase-b], got %v", snap.InProgress)
		}
		if len(snap.UnresolvedDiscoveries) != 1 {
			t.Errorf("expected 1 discovery, got %d", len(snap.UnresolvedDiscoveries))
		}
		if len(snap.Pulses) != 1 {
			t.Errorf("expected 1 pulse, got %d", len(snap.Pulses))
		}
	})

	t.Run("completed and in-progress are sorted", func(t *testing.T) {
		t.Parallel()
		mf := &mockFabric{
			phaseStates: map[string]string{
				"phase-z": fabric.StateDone,
				"phase-a": fabric.StateDone,
				"phase-m": fabric.StateRunning,
				"phase-b": fabric.StateRunning,
			},
		}
		l := &Loop{Fabric: mf, FabricEnabled: true}
		snap := l.buildFabricSnapshot(context.Background())

		if len(snap.Completed) != 2 || snap.Completed[0] != "phase-a" || snap.Completed[1] != "phase-z" {
			t.Errorf("expected completed sorted [phase-a, phase-z], got %v", snap.Completed)
		}
		if len(snap.InProgress) != 2 || snap.InProgress[0] != "phase-b" || snap.InProgress[1] != "phase-m" {
			t.Errorf("expected inProgress sorted [phase-b, phase-m], got %v", snap.InProgress)
		}
	})

	t.Run("empty fabric returns empty snapshot", func(t *testing.T) {
		t.Parallel()
		mf := &mockFabric{}
		l := &Loop{Fabric: mf, FabricEnabled: true}
		snap := l.buildFabricSnapshot(context.Background())

		if len(snap.Entanglements) != 0 {
			t.Error("expected no entanglements")
		}
		if len(snap.FileClaims) != 0 {
			t.Error("expected no file claims")
		}
		if len(snap.Completed) != 0 {
			t.Error("expected no completed phases")
		}
		if len(snap.InProgress) != 0 {
			t.Error("expected no in-progress phases")
		}
	})
}

func TestFabricContextInjectionInPrompts(t *testing.T) {
	t.Parallel()

	mf := &mockFabric{
		entanglements: []fabric.Entanglement{
			{Producer: "phase-a", Kind: "interface", Name: "Foo", Package: "pkg/foo"},
		},
		claims: []fabric.Claim{
			{Filepath: "internal/bar.go", OwnerTask: "phase-b"},
		},
		phaseStates: map[string]string{
			"phase-a": fabric.StateDone,
		},
	}

	t.Run("coder prompt does not inject fabric directly", func(t *testing.T) {
		t.Parallel()
		l := &Loop{Fabric: mf, FabricEnabled: true}
		state := &CycleState{
			TaskBeadID: "test-1",
			TaskTitle:  "implement X",
			Cycle:      1,
		}
		prompt := l.buildCoderPrompt(state)
		// buildCoderPrompt itself does NOT inject fabric — that happens in
		// runCoderPhase. Verify the base prompt is fabric-free.
		if strings.Contains(prompt, "Current Fabric State") {
			t.Error("buildCoderPrompt should not inject fabric directly; that is done in runCoderPhase")
		}
	})

	t.Run("PrependFabricContext wraps coder prompt correctly", func(t *testing.T) {
		t.Parallel()
		l := &Loop{Fabric: mf, FabricEnabled: true}
		state := &CycleState{
			TaskBeadID: "test-1",
			TaskTitle:  "implement X",
			Cycle:      1,
		}
		prompt := l.buildCoderPrompt(state)
		snap := l.buildFabricSnapshot(context.Background())
		wrapped := PrependFabricContext(prompt, snap)

		if !strings.HasPrefix(wrapped, "## Current Fabric State") {
			t.Error("expected wrapped prompt to start with fabric state header")
		}
		if !strings.Contains(wrapped, "implement X") {
			t.Error("expected original task description to be preserved")
		}
		if !strings.Contains(wrapped, "phase-a") {
			t.Error("expected entanglement producer in fabric state")
		}
	})

	t.Run("PrependFabricContext wraps reviewer prompt correctly", func(t *testing.T) {
		t.Parallel()
		l := &Loop{Fabric: mf, FabricEnabled: true}
		state := &CycleState{
			TaskBeadID:  "test-1",
			TaskTitle:   "implement X",
			Cycle:       1,
			CoderOutput: "I made changes to foo.go",
		}
		prompt := l.buildReviewerPrompt(state)
		snap := l.buildFabricSnapshot(context.Background())
		wrapped := PrependFabricContext(prompt, snap)

		if !strings.Contains(wrapped, "Current Fabric State") {
			t.Error("expected fabric state in wrapped reviewer prompt")
		}
		if !strings.Contains(wrapped, "REVIEW INSTRUCTIONS") {
			t.Error("expected review instructions preserved in wrapped prompt")
		}
	})

	t.Run("no injection when fabric disabled", func(t *testing.T) {
		t.Parallel()
		l := &Loop{FabricEnabled: false}
		state := &CycleState{
			TaskBeadID: "test-1",
			TaskTitle:  "implement X",
			Cycle:      1,
		}
		prompt := l.buildCoderPrompt(state)
		if strings.Contains(prompt, "Fabric State") {
			t.Error("fabric state should not appear when fabric is disabled")
		}
	})

	t.Run("no injection when Fabric is nil", func(t *testing.T) {
		t.Parallel()
		l := &Loop{FabricEnabled: true, Fabric: nil}
		state := &CycleState{
			TaskBeadID: "test-1",
			TaskTitle:  "implement X",
			Cycle:      1,
		}
		prompt := l.buildCoderPrompt(state)
		if strings.Contains(prompt, "Fabric State") {
			t.Error("fabric state should not appear when Fabric is nil")
		}
	})
}
