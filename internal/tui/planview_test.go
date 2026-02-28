package tui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
)

func testPlan() *nebula.ExecutionPlan {
	return &nebula.ExecutionPlan{
		Name: "test-nebula",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"phase-a"}},
			{Number: 2, NodeIDs: []string{"phase-b", "phase-c"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"phase-a", "phase-b"}},
			{ID: 1, NodeIDs: []string{"phase-c"}},
		},
		Contracts: []fabric.PhaseContract{
			{PhaseID: "phase-a", Produces: []fabric.Entanglement{{Name: "FooType"}}},
			{PhaseID: "phase-b", Consumes: []fabric.Entanglement{{Name: "FooType"}}},
		},
		Report: &fabric.ContractReport{
			Fulfilled: []fabric.ContractEntry{
				{Consumer: "phase-b", Producer: "phase-a", Entanglement: fabric.Entanglement{Name: "FooType"}},
			},
		},
		Risks: []nebula.PlanRisk{
			{Severity: "warning", PhaseID: "phase-c", Message: "no downstream consumers"},
			{Severity: "info", Message: "single track — parallelism limited"},
		},
		Stats: nebula.PlanStats{
			TotalPhases:        3,
			TotalWaves:         2,
			TotalTracks:        2,
			FulfilledContracts: 1,
			EstimatedCost:      50.0,
		},
	}
}

func TestPlanView_NewPlanView(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()
	if !pv.loading {
		t.Error("new plan view should be loading")
	}
	if pv.Plan != nil {
		t.Error("new plan view should have nil plan")
	}
	if pv.selected != PlanActionApply {
		t.Errorf("default selected action should be Apply, got %d", pv.selected)
	}
}

func TestPlanView_SetPlan(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()
	pv.SetSize(80, 40)

	plan := testPlan()
	pv.SetPlan(plan, nil, "/tmp/test")

	if pv.loading {
		t.Error("plan view should not be loading after SetPlan")
	}
	if pv.Plan != plan {
		t.Error("plan not set correctly")
	}
	if pv.NebulaDir != "/tmp/test" {
		t.Errorf("NebulaDir = %q, want /tmp/test", pv.NebulaDir)
	}
}

func TestPlanView_ViewLoading(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()
	view := pv.View()

	if !strings.Contains(view, "Analyzing") {
		t.Errorf("loading view should contain 'Analyzing', got %q", view)
	}
}

func TestPlanView_ViewRendered(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()
	pv.SetSize(100, 40)
	pv.SetPlan(testPlan(), nil, "/tmp/test")

	view := pv.View()

	// Check that key sections are present.
	if !strings.Contains(view, "test-nebula") {
		t.Error("view should contain nebula name")
	}
	if !strings.Contains(view, "Contracts") {
		t.Error("view should contain Contracts section")
	}
	if !strings.Contains(view, "Risks") {
		t.Error("view should contain Risks section")
	}
	if !strings.Contains(view, "Stats") {
		t.Error("view should contain Stats section")
	}
}

func TestPlanView_ViewWithDiff(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()
	pv.SetSize(100, 60)

	changes := []nebula.PlanChange{
		{Kind: "added", Subject: "phase-d", Detail: "phase added to plan"},
		{Kind: "changed", Subject: "waves", Detail: "wave count changed from 1 to 2"},
	}
	pv.SetPlan(testPlan(), changes, "/tmp/test")

	view := pv.View()
	if !strings.Contains(view, "Changes since last plan") {
		t.Error("view should contain diff section when changes exist")
	}
}

func TestPlanView_ActionCycling(t *testing.T) {
	t.Parallel()

	pv := NewPlanView()

	// Default is Apply.
	if pv.SelectedAction() != PlanActionApply {
		t.Error("default should be Apply")
	}

	// Move right: Apply -> Cancel.
	pv.MoveRight()
	if pv.SelectedAction() != PlanActionCancel {
		t.Errorf("after MoveRight, expected Cancel, got %d", pv.SelectedAction())
	}

	// Move right: Cancel -> Save.
	pv.MoveRight()
	if pv.SelectedAction() != PlanActionSave {
		t.Errorf("after second MoveRight, expected Save, got %d", pv.SelectedAction())
	}

	// Move right wraps: Save -> Apply.
	pv.MoveRight()
	if pv.SelectedAction() != PlanActionApply {
		t.Errorf("after wrap MoveRight, expected Apply, got %d", pv.SelectedAction())
	}

	// Move left wraps: Apply -> Save.
	pv.MoveLeft()
	if pv.SelectedAction() != PlanActionSave {
		t.Errorf("after MoveLeft from Apply, expected Save, got %d", pv.SelectedAction())
	}
}

func TestPlanView_ErrorRiskCount(t *testing.T) {
	t.Parallel()

	t.Run("no errors", func(t *testing.T) {
		t.Parallel()
		pv := NewPlanView()
		pv.Plan = testPlan() // has only warning and info risks
		if n := pv.ErrorRiskCount(); n != 0 {
			t.Errorf("expected 0 error risks, got %d", n)
		}
	})

	t.Run("with errors", func(t *testing.T) {
		t.Parallel()
		plan := testPlan()
		plan.Risks = append(plan.Risks, nebula.PlanRisk{
			Severity: "error",
			Message:  "missing contract",
		})
		pv := NewPlanView()
		pv.Plan = plan
		if n := pv.ErrorRiskCount(); n != 1 {
			t.Errorf("expected 1 error risk, got %d", n)
		}
	})

	t.Run("nil plan", func(t *testing.T) {
		t.Parallel()
		pv := NewPlanView()
		if n := pv.ErrorRiskCount(); n != 0 {
			t.Errorf("expected 0 for nil plan, got %d", n)
		}
	})
}

func TestPlanView_RenderErrorRiskBadge(t *testing.T) {
	t.Parallel()

	plan := testPlan()
	plan.Risks = append(plan.Risks, nebula.PlanRisk{
		Severity: "error",
		Message:  "scope conflict on cmd/",
	})

	pv := NewPlanView()
	pv.SetSize(100, 60)
	pv.SetPlan(plan, nil, "/tmp/test")

	view := pv.View()
	// The Apply button should show a risk badge.
	if !strings.Contains(view, "risk") {
		t.Error("Apply button should contain risk badge when errors exist")
	}
}

func TestMsgPlanAction_ApplySetsNebulaAndQuits(t *testing.T) {
	t.Parallel()

	m := newHomeModel([]NebulaChoice{
		{Name: "Alpha", Path: "/path/alpha", Status: "ready", Phases: 2},
	})
	m.ShowPlanPreview = true
	pv := NewPlanView()
	pv.Plan = testPlan()
	pv.NebulaDir = "/path/alpha"
	m.PlanPreview = &pv

	msg := MsgPlanAction{
		Action:    PlanActionApply,
		Plan:      pv.Plan,
		NebulaDir: "/path/alpha",
	}

	result, cmd := m.Update(msg)
	rm := result.(AppModel)

	if rm.SelectedNebula != "/path/alpha" {
		t.Errorf("expected SelectedNebula '/path/alpha', got %q", rm.SelectedNebula)
	}
	if rm.NextNebula != "/path/alpha" {
		t.Errorf("expected NextNebula '/path/alpha', got %q", rm.NextNebula)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd (tea.Quit)")
	}
}

func TestMsgPlanAction_CancelReturnsToHome(t *testing.T) {
	t.Parallel()

	m := newHomeModel([]NebulaChoice{
		{Name: "Alpha", Path: "/path/alpha", Status: "ready", Phases: 2},
	})
	m.ShowPlanPreview = true
	pv := NewPlanView()
	m.PlanPreview = &pv

	msg := MsgPlanAction{
		Action:    PlanActionCancel,
		NebulaDir: "/path/alpha",
	}

	result, _ := m.Update(msg)
	rm := result.(AppModel)

	if rm.ShowPlanPreview {
		t.Error("ShowPlanPreview should be false after cancel")
	}
	if rm.PlanPreview != nil {
		t.Error("PlanPreview should be nil after cancel")
	}
}

func TestMsgPlanError_ShowsToast(t *testing.T) {
	t.Parallel()

	m := newHomeModel([]NebulaChoice{
		{Name: "Alpha", Path: "/path/alpha", Status: "ready", Phases: 2},
	})
	m.ShowPlanPreview = true
	pv := NewPlanView()
	m.PlanPreview = &pv

	msg := MsgPlanError{Err: errTest}

	result, _ := m.Update(msg)
	rm := result.(AppModel)

	if rm.ShowPlanPreview {
		t.Error("ShowPlanPreview should be false after plan error")
	}
	if len(rm.Toasts) == 0 {
		t.Error("expected toast for plan error")
	}
}

// errTest is a test sentinel error.
var errTest = errSentinel("test error")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
