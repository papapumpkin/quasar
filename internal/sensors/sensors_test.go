package sensors

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeSensor is a minimal Sensor used to assert interface satisfaction and
// exercise the registry. It is intentionally inert: Configure/Poll/SeedNebula
// return zero values so registry tests can focus on registration semantics.
type fakeSensor struct{ name string }

func (f *fakeSensor) Name() string { return f.name }

func (f *fakeSensor) Configure(map[string]any, SecretResolver) error { return nil }

func (f *fakeSensor) Poll(context.Context, json.RawMessage) ([]Event, json.RawMessage, error) {
	return nil, nil, nil
}

func (f *fakeSensor) SeedNebula(Event) (*SeedNebulaContent, error) { return nil, nil }

// fakeForge is a minimal Forge used to assert interface satisfaction.
type fakeForge struct{ name string }

func (f *fakeForge) Name() string { return f.name }

// Compile-time interface satisfaction checks.
var (
	_ Sensor = (*fakeSensor)(nil)
	_ Forge  = (*fakeForge)(nil)
)

func TestTicketZeroValue(t *testing.T) {
	t.Parallel()

	var tk Ticket
	if tk.SourceName != "" || tk.SourceID != "" || tk.Title != "" {
		t.Errorf("zero Ticket should have empty string fields, got %+v", tk)
	}
	if tk.Number != 0 {
		t.Errorf("zero Ticket.Number = %d, want 0", tk.Number)
	}
	if tk.Labels != nil || tk.Comments != nil || tk.LinkedWork != nil || tk.SourceMetadata != nil {
		t.Errorf("zero Ticket slices/maps should be nil, got %+v", tk)
	}
}
