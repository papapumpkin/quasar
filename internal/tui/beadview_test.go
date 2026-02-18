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

	// Check header elements.
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

	// Check tree connectors and inline titles.
	if !strings.Contains(view, "├─") {
		t.Error("Expected mid-tree connector '├─'")
	}
	if !strings.Contains(view, "└─") {
		t.Error("Expected last-tree connector '└─'")
	}
	if !strings.Contains(view, "SQL injection") {
		t.Error("Expected child title 'SQL injection' inline")
	}
	if !strings.Contains(view, "Missing tests") {
		t.Error("Expected child title 'Missing tests' inline")
	}
	if !strings.Contains(view, "Error handling") {
		t.Error("Expected child title 'Error handling' inline")
	}

	// Check status icons are present.
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
			{ID: "c1", Title: "First task", Status: "closed", Cycle: 1},
			{ID: "c2", Title: "Second task", Status: "closed", Cycle: 1},
		},
	})
	view := bv.View()
	if !strings.Contains(view, "[2/2 resolved]") {
		t.Error("Expected '[2/2 resolved]' for fully resolved task")
	}
	// Last child should use └─ connector.
	if !strings.Contains(view, "└─") {
		t.Error("Expected last-tree connector for final child")
	}
}

func TestBeadViewCycleOrdering(t *testing.T) {
	bv := NewBeadView()
	bv.Width = 80
	bv.SetRoot(BeadInfo{
		ID:     "root",
		Title:  "Ordered task",
		Status: "open",
		Children: []BeadInfo{
			{ID: "c3", Title: "Cycle2 first", Status: "open", Cycle: 2},
			{ID: "c1", Title: "Cycle1 first", Status: "closed", Cycle: 1},
			{ID: "c4", Title: "Cycle2 second", Status: "open", Cycle: 2},
			{ID: "c2", Title: "Cycle1 second", Status: "closed", Cycle: 1},
		},
	})

	view := bv.View()

	// Cycle 1 children should appear before cycle 2 children.
	c1First := strings.Index(view, "Cycle1 first")
	c1Second := strings.Index(view, "Cycle1 second")
	c2First := strings.Index(view, "Cycle2 first")
	c2Second := strings.Index(view, "Cycle2 second")

	if c1First == -1 || c1Second == -1 || c2First == -1 || c2Second == -1 {
		t.Fatal("Expected all child titles to appear in view")
	}
	if c1First > c2First {
		t.Error("Expected cycle 1 children before cycle 2 children")
	}
	if c1First > c1Second {
		t.Error("Expected cycle 1 first child before cycle 1 second child (stable order)")
	}
	if c2First > c2Second {
		t.Error("Expected cycle 2 first child before cycle 2 second child (stable order)")
	}
}

func TestBeadViewTitleTruncation(t *testing.T) {
	bv := NewBeadView()
	bv.Width = 25 // Very narrow — prefix is 9 chars, leaving 16 for title.
	bv.SetRoot(BeadInfo{
		ID:     "root",
		Title:  "Narrow task",
		Status: "open",
		Children: []BeadInfo{
			{ID: "c1", Title: "This is a very long title that should be truncated", Status: "open", Cycle: 1},
		},
	})

	view := bv.View()

	// The full title should NOT appear.
	if strings.Contains(view, "This is a very long title that should be truncated") {
		t.Error("Expected title to be truncated at narrow width")
	}
	// But ellipsis should appear.
	if !strings.Contains(view, "...") {
		t.Error("Expected ellipsis in truncated title")
	}
}

func TestSortChildrenByCycle(t *testing.T) {
	children := []BeadInfo{
		{ID: "a", Title: "A", Cycle: 2},
		{ID: "b", Title: "B", Cycle: 1},
		{ID: "c", Title: "C", Cycle: 2},
		{ID: "d", Title: "D", Cycle: 0}, // 0 maps to cycle 1
	}

	sorted := sortChildrenByCycle(children)
	if len(sorted) != 4 {
		t.Fatalf("Expected 4 children, got %d", len(sorted))
	}

	// Cycle 1 (and 0→1) should come first, then cycle 2.
	if sorted[0].ID != "b" {
		t.Errorf("sorted[0] = %q, want b (cycle 1)", sorted[0].ID)
	}
	if sorted[1].ID != "d" {
		t.Errorf("sorted[1] = %q, want d (cycle 0→1)", sorted[1].ID)
	}
	if sorted[2].ID != "a" {
		t.Errorf("sorted[2] = %q, want a (cycle 2)", sorted[2].ID)
	}
	if sorted[3].ID != "c" {
		t.Errorf("sorted[3] = %q, want c (cycle 2)", sorted[3].ID)
	}
}

func TestSortChildrenByCycleEmpty(t *testing.T) {
	sorted := sortChildrenByCycle(nil)
	if len(sorted) != 0 {
		t.Errorf("Expected 0 children for nil input, got %d", len(sorted))
	}
}

func TestSortChildrenByCyclePreservesOrder(t *testing.T) {
	children := []BeadInfo{
		{ID: "a", Cycle: 3},
		{ID: "b", Cycle: 1},
		{ID: "c", Cycle: 3},
	}

	sorted := sortChildrenByCycle(children)
	if len(sorted) != 3 {
		t.Fatalf("Expected 3 children, got %d", len(sorted))
	}
	// Cycle 1 first, then cycle 3 in original order.
	if sorted[0].ID != "b" {
		t.Errorf("sorted[0] = %q, want b (cycle 1)", sorted[0].ID)
	}
	if sorted[1].ID != "a" {
		t.Errorf("sorted[1] = %q, want a (cycle 3)", sorted[1].ID)
	}
	if sorted[2].ID != "c" {
		t.Errorf("sorted[2] = %q, want c (cycle 3)", sorted[2].ID)
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
