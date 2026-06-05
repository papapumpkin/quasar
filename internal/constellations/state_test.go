package constellations

import "testing"

func TestStateRoundTrip(t *testing.T) {
	st := NewState(NebulaSnapshot{
		ID:     "neb-1",
		Name:   "demo",
		Status: "running",
		Phases: []PhaseSnapshot{{ID: "p1", Seq: 0, Title: "First", Status: "pending"}},
	}, 42, 1000)
	st.RecordNode("review", map[string]any{"approved": true, "risk": "low"})
	st.Meta.TotalCostUSD = 1.25
	st.Cycle = 2

	encoded, err := MarshalState(st)
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}
	got, err := UnmarshalState(encoded)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	if got.Nebula.ID != "neb-1" || got.Cycle != 2 || got.Meta.TotalCostUSD != 1.25 {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if got.Nodes["review"]["approved"] != true {
		t.Errorf("node output not round-tripped: %+v", got.Nodes["review"])
	}
	if len(got.Nebula.Phases) != 1 || got.Nebula.Phases[0].ID != "p1" {
		t.Errorf("phases not round-tripped: %+v", got.Nebula.Phases)
	}
}

func TestExprStateLookups(t *testing.T) {
	st := NewState(NebulaSnapshot{ID: "n", Name: "demo", Status: "awaiting_approval"}, 1, 1)
	st.RecordNode("review", map[string]any{"approved": false})

	es := st.ExprState()
	if got := es.Get("nebula.status"); got != "awaiting_approval" {
		t.Errorf("nebula.status = %v", got)
	}
	if got := es.Get("nodes.review.approved"); got != false {
		t.Errorf("nodes.review.approved = %v", got)
	}
	if got := es.Get("nodes.missing.field"); got != nil {
		t.Errorf("missing lookup = %v, want nil", got)
	}
}
