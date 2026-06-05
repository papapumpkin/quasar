package integrations

import (
	"context"
	"testing"
)

// fakeSource is a minimal TicketSource used to assert interface satisfaction
// and exercise the registry.
type fakeSource struct {
	name   string
	ticket *Ticket
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Fetch(_ context.Context, sourceID string) (*Ticket, error) {
	if f.ticket != nil {
		return f.ticket, nil
	}
	return &Ticket{SourceName: f.name, SourceID: sourceID}, nil
}

// fakeForge is a minimal Forge used to assert interface satisfaction.
type fakeForge struct{ name string }

func (f *fakeForge) Name() string { return f.name }

// Compile-time interface satisfaction checks.
var (
	_ TicketSource = (*fakeSource)(nil)
	_ Forge        = (*fakeForge)(nil)
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

func TestTicketRoundTripsThroughFetch(t *testing.T) {
	t.Parallel()

	src := &fakeSource{name: "fake"}
	got, err := src.Fetch(context.Background(), "fake#1")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if got.SourceName != "fake" || got.SourceID != "fake#1" {
		t.Errorf("Fetch returned %+v, want SourceName=fake SourceID=fake#1", got)
	}
}
