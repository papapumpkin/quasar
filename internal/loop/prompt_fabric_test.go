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
func (m *mockFabric) PurgeAll(context.Context) error { return nil }
func (m *mockFabric) Close() error                   { return nil }

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
