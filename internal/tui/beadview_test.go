package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBeadViewEmptyState(t *testing.T) {
	bv := NewBeadView()
	view := bv.View()
	if !strings.Contains(view, "no bead data yet") {
		t.Errorf("Empty BeadView should show placeholder, got: %q", view)
	}
}

func TestBeadViewRootOnly(t *testing.T) {
	bv := NewBeadView()
	bv.SetRoot(BeadInfo{
		ID:     "quasar-a1b",
		Title:  "Add JWT auth",
		Status: "open",
		Type:   "task",
	})

	view := bv.View()
	if !strings.Contains(view, "Add JWT auth") {
		t.Error("Expected root title in view")
	}
	if !strings.Contains(view, "no child issues") {
		t.Error("Expected 'no child issues' placeholder for root with no children")
	}
}

func TestBeadViewWithChildren(t *testing.T) {
	bv := NewBeadView()
	bv.Width = 60
	bv.SetRoot(BeadInfo{
		ID:     "quasar-a1b",
		Title:  "Add JWT auth",
		Status: "open",
		Type:   "task",
		Children: []BeadInfo{
			{ID: "quasar-a1b.1", Title: "SQL injection", Status: "closed", Cycle: 1},
			{ID: "quasar-a1b.2", Title: "Missing tests", Status: "open", Cycle: 1},
			{ID: "quasar-a1b.3", Title: "Error handling", Status: "in_progress", Cycle: 2},
		},
	})

	view := bv.View()

	// Check compact graph elements.
	if !strings.Contains(view, "Add JWT auth") {
		t.Error("Expected root title in view")
	}
	if !strings.Contains(view, "[1/3 resolved]") {
		t.Error("Expected progress fraction '[1/3 resolved]'")
	}

	// Check progress bar characters.
	if !strings.Contains(view, "█") {
		t.Error("Expected filled progress bar characters")
	}
	if !strings.Contains(view, "░") {
		t.Error("Expected empty progress bar characters")
	}

	// Check cycle labels and status icons.
	if !strings.Contains(view, "Cycle 1") {
		t.Error("Expected 'Cycle 1' label")
	}
	if !strings.Contains(view, "Cycle 2") {
		t.Error("Expected 'Cycle 2' label")
	}
	if !strings.Contains(view, beadIconClosed) {
		t.Error("Expected closed icon (✓) in view")
	}
	if !strings.Contains(view, beadIconOpen) {
		t.Error("Expected open icon (●) in view")
	}
	if !strings.Contains(view, beadIconInProgress) {
		t.Error("Expected in_progress icon (◎) in view")
	}
}

func TestBeadViewAllResolved(t *testing.T) {
	bv := NewBeadView()
	bv.Width = 60
	bv.SetRoot(BeadInfo{
		ID:     "root",
		Title:  "All done",
		Status: "closed",
		Children: []BeadInfo{
			{ID: "c1", Status: "closed", Cycle: 1},
			{ID: "c2", Status: "closed", Cycle: 1},
		},
	})
	view := bv.View()
	if !strings.Contains(view, "[2/2 resolved]") {
		t.Error("Expected '[2/2 resolved]' for fully resolved task")
	}
}

func TestGroupByCycle(t *testing.T) {
	children := []BeadInfo{
		{ID: "a", Cycle: 1},
		{ID: "b", Cycle: 1},
		{ID: "c", Cycle: 2},
		{ID: "d", Cycle: 0}, // 0 maps to cycle 1
	}

	groups := groupByCycle(children)
	if len(groups) != 2 {
		t.Fatalf("Expected 2 cycle groups, got %d", len(groups))
	}

	// Cycle 1 should have 3 children (a, b, d).
	if groups[0].Cycle != 1 {
		t.Errorf("First group cycle = %d, want 1", groups[0].Cycle)
	}
	if len(groups[0].Children) != 3 {
		t.Errorf("Cycle 1 children = %d, want 3", len(groups[0].Children))
	}

	// Cycle 2 should have 1 child (c).
	if groups[1].Cycle != 2 {
		t.Errorf("Second group cycle = %d, want 2", groups[1].Cycle)
	}
	if len(groups[1].Children) != 1 {
		t.Errorf("Cycle 2 children = %d, want 1", len(groups[1].Children))
	}
}

func TestGroupByCycleEmpty(t *testing.T) {
	groups := groupByCycle(nil)
	if len(groups) != 0 {
		t.Errorf("Expected 0 groups for nil children, got %d", len(groups))
	}
}

func TestGroupByCyclePreservesOrder(t *testing.T) {
	children := []BeadInfo{
		{ID: "a", Cycle: 3},
		{ID: "b", Cycle: 1},
		{ID: "c", Cycle: 3},
	}

	groups := groupByCycle(children)
	if len(groups) != 2 {
		t.Fatalf("Expected 2 groups, got %d", len(groups))
	}
	if groups[0].Cycle != 3 {
		t.Errorf("First group cycle = %d, want 3 (order of first appearance)", groups[0].Cycle)
	}
	if groups[1].Cycle != 1 {
		t.Errorf("Second group cycle = %d, want 1", groups[1].Cycle)
	}
}

func TestRenderCompactCycle(t *testing.T) {
	g := cycleGroup{
		Cycle: 1,
		Children: []BeadInfo{
			{ID: "a", Title: "First issue", Status: "closed"},
			{ID: "b", Title: "Second issue", Status: "open"},
		},
	}

	out := renderCompactCycle(g)
	if !strings.Contains(out, "Cycle 1") {
		t.Error("Expected 'Cycle 1' label")
	}
	if !strings.Contains(out, beadIconClosed) {
		t.Error("Expected closed icon")
	}
	if !strings.Contains(out, beadIconOpen) {
		t.Error("Expected open icon")
	}
}

func TestBeadStatusIcon(t *testing.T) {
	tests := []struct {
		status   string
		wantIcon string
	}{
		{"closed", beadIconClosed},
		{"in_progress", beadIconInProgress},
		{"open", beadIconOpen},
		{"unknown", beadIconOpen}, // default
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			icon, _ := beadStatusIcon(tt.status)
			if icon != tt.wantIcon {
				t.Errorf("beadStatusIcon(%q) = %q, want %q", tt.status, icon, tt.wantIcon)
			}
		})
	}
}

func TestBeadStatusLabel(t *testing.T) {
	tests := []struct {
		status  string
		wantSub string
	}{
		{"closed", "closed"},
		{"in_progress", "in_progress"},
		{"open", "open"},
		{"other", "open"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			label := beadStatusLabel(tt.status)
			if !strings.Contains(label, tt.wantSub) {
				t.Errorf("beadStatusLabel(%q) = %q, expected to contain %q", tt.status, label, tt.wantSub)
			}
		})
	}
}

func TestPluralIssue(t *testing.T) {
	if got := pluralIssue(0); got != "issues" {
		t.Errorf("pluralIssue(0) = %q, want %q", got, "issues")
	}
	if got := pluralIssue(1); got != "issue" {
		t.Errorf("pluralIssue(1) = %q, want %q", got, "issue")
	}
	if got := pluralIssue(5); got != "issues" {
		t.Errorf("pluralIssue(5) = %q, want %q", got, "issues")
	}
}

func TestBeadViewSetRootSetsHasData(t *testing.T) {
	bv := NewBeadView()
	if bv.HasData {
		t.Error("New BeadView should have HasData = false")
	}
	bv.SetRoot(BeadInfo{ID: "test", Title: "test"})
	if !bv.HasData {
		t.Error("After SetRoot, HasData should be true")
	}
}

func TestBeadSeverityTag(t *testing.T) {
	tests := []struct {
		severity string
		wantSub  string
	}{
		{"critical", "critical"},
		{"major", "major"},
		{"minor", "minor"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			tag := beadSeverityTag(tt.severity)
			if !strings.Contains(tag, tt.wantSub) {
				t.Errorf("beadSeverityTag(%q) = %q, expected to contain %q", tt.severity, tag, tt.wantSub)
			}
		})
	}
}

func TestMsgBeadUpdateAutoRefreshesBeadPanel(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.ShowBeads = true

	var tm tea.Model = m
	tm, _ = tm.Update(MsgBeadUpdate{
		TaskBeadID: "bead-1",
		Root: BeadInfo{
			ID:     "bead-1",
			Title:  "Test task",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "bead-1.1", Title: "Issue 1", Status: "open", Severity: "major", Cycle: 1},
			},
		},
	})
	am := tm.(AppModel)
	// The detail panel should have been refreshed with bead content.
	// Verify by checking the View output contains bead info.
	view := am.Detail.View()
	if !strings.Contains(view, "Test task") {
		t.Error("Expected detail panel to be refreshed with bead content when ShowBeads is true")
	}
}

func TestMsgBeadUpdatePopulatesModel(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m
	tm, _ = tm.Update(MsgBeadUpdate{
		TaskBeadID: "bead-1",
		Root: BeadInfo{
			ID:     "bead-1",
			Title:  "Test task",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "bead-1.1", Title: "Issue 1", Status: "open", Cycle: 1},
			},
		},
	})
	am := tm.(AppModel)
	if am.LoopBeads == nil {
		t.Fatal("Expected LoopBeads to be populated after MsgBeadUpdate")
	}
	if am.LoopBeads.ID != "bead-1" {
		t.Errorf("LoopBeads.ID = %q, want %q", am.LoopBeads.ID, "bead-1")
	}
	if len(am.LoopBeads.Children) != 1 {
		t.Errorf("LoopBeads.Children = %d, want 1", len(am.LoopBeads.Children))
	}
}

func TestMsgPhaseBeadUpdatePopulatesModel(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m
	tm, _ = tm.Update(MsgPhaseBeadUpdate{
		PhaseID:    "setup",
		TaskBeadID: "bead-2",
		Root: BeadInfo{
			ID:     "bead-2",
			Title:  "Setup task",
			Status: "open",
		},
	})
	am := tm.(AppModel)
	root, ok := am.PhaseBeads["setup"]
	if !ok || root == nil {
		t.Fatal("Expected PhaseBeads[\"setup\"] to be populated after MsgPhaseBeadUpdate")
	}
	if root.ID != "bead-2" {
		t.Errorf("PhaseBeads[\"setup\"].ID = %q, want %q", root.ID, "bead-2")
	}
}
