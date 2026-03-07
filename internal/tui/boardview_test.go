package tui

import (
	"strings"
	"testing"
)

func TestBoardViewPartition_CorrectColumns(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150 // Full width: all 6 columns visible, no remapping.
	bv.Phases = []PhaseEntry{
		{ID: "queued", Status: PhaseWaiting},
		{ID: "running", Status: PhaseWorking},
		{ID: "done", Status: PhaseDone},
		{ID: "failed", Status: PhaseFailed},
		{ID: "gate", Status: PhaseGate},
		{ID: "skipped", Status: PhaseSkipped},
		{ID: "blocked", Status: PhaseWaiting, BlockedBy: "running"},
	}

	buckets := bv.partition()

	tests := []struct {
		col    BoardColumn
		expect []string
	}{
		{ColQueued, []string{"queued"}},
		{ColRunning, []string{"running"}},
		{ColDone, []string{"done", "skipped"}},
		{ColFailed, []string{"failed"}},
		{ColReview, []string{"gate"}},
		{ColBlocked, []string{"blocked"}},
	}

	for _, tc := range tests {
		t.Run(columnDefs[tc.col].Label, func(t *testing.T) {
			got := buckets[tc.col]
			if len(tc.expect) == 0 {
				if len(got) != 0 {
					t.Errorf("expected empty %s column, got %d entries", columnDefs[tc.col].Label, len(got))
				}
				return
			}
			if len(got) != len(tc.expect) {
				t.Errorf("expected %d entries in %s, got %d", len(tc.expect), columnDefs[tc.col].Label, len(got))
				return
			}
			for i, idx := range got {
				if bv.Phases[idx].ID != tc.expect[i] {
					t.Errorf("expected %s at position %d in %s, got %s",
						tc.expect[i], i, columnDefs[tc.col].Label, bv.Phases[idx].ID)
				}
			}
		})
	}
}

func TestBoardViewPartition_MediumWidthRemapping(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 120 // Medium width: Blocked column merges into Queued.
	bv.Phases = []PhaseEntry{
		{ID: "queued", Status: PhaseWaiting},
		{ID: "blocked", Status: PhaseWaiting, BlockedBy: "other"},
		{ID: "running", Status: PhaseWorking},
	}

	buckets := bv.partition()

	// At medium width, blocked phases should be remapped into Queued.
	if len(buckets[ColQueued]) != 2 {
		t.Errorf("expected 2 entries in Queued (original + remapped blocked), got %d", len(buckets[ColQueued]))
	}
	// Blocked column should be empty at medium width.
	if len(buckets[ColBlocked]) != 0 {
		t.Errorf("expected 0 entries in Blocked at medium width, got %d", len(buckets[ColBlocked]))
	}
	// Running should be unaffected.
	if len(buckets[ColRunning]) != 1 {
		t.Errorf("expected 1 entry in Running, got %d", len(buckets[ColRunning]))
	}
}

func TestBoardViewCursorNavigation_UpDown(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
		{ID: "b", Status: PhaseWaiting},
		{ID: "c", Status: PhaseWorking},
	}
	bv.Cursor = 0

	bv.MoveDown()
	if bv.Cursor != 1 {
		t.Errorf("expected cursor 1 after MoveDown, got %d", bv.Cursor)
	}

	bv.MoveDown()
	if bv.Cursor != 2 {
		t.Errorf("expected cursor 2 after second MoveDown, got %d", bv.Cursor)
	}

	// Should not go past end.
	bv.MoveDown()
	if bv.Cursor != 2 {
		t.Errorf("expected cursor to stay at 2, got %d", bv.Cursor)
	}

	bv.MoveUp()
	if bv.Cursor != 1 {
		t.Errorf("expected cursor 1 after MoveUp, got %d", bv.Cursor)
	}

	bv.MoveUp()
	if bv.Cursor != 0 {
		t.Errorf("expected cursor 0 after second MoveUp, got %d", bv.Cursor)
	}

	// Should not go below 0.
	bv.MoveUp()
	if bv.Cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", bv.Cursor)
	}
}

func TestBoardViewCursorNavigation_LeftRight(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "queued1", Status: PhaseWaiting},
		{ID: "running1", Status: PhaseWorking},
		{ID: "done1", Status: PhaseDone},
	}
	bv.Cursor = 0 // starts at queued1 (ColQueued)

	bv.MoveRight()
	sel := bv.SelectedPhase()
	if sel == nil || sel.ID != "running1" {
		id := ""
		if sel != nil {
			id = sel.ID
		}
		t.Errorf("expected running1 after MoveRight, got %s", id)
	}

	bv.MoveRight()
	sel = bv.SelectedPhase()
	if sel == nil || sel.ID != "done1" {
		id := ""
		if sel != nil {
			id = sel.ID
		}
		t.Errorf("expected done1 after second MoveRight, got %s", id)
	}

	// MoveRight past last column should stay.
	prevCursor := bv.Cursor
	bv.MoveRight()
	if bv.Cursor != prevCursor {
		t.Errorf("expected cursor to stay after MoveRight at end, got %d", bv.Cursor)
	}

	bv.MoveLeft()
	sel = bv.SelectedPhase()
	if sel == nil || sel.ID != "running1" {
		id := ""
		if sel != nil {
			id = sel.ID
		}
		t.Errorf("expected running1 after MoveLeft, got %s", id)
	}
}

func TestBoardViewView_ColumnHeaders(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
	}

	view := bv.View()

	// All 6 columns should have headers at full width.
	for col := BoardColumn(0); col < colCount; col++ {
		label := columnDefs[col].Label
		if !strings.Contains(view, label) {
			t.Errorf("expected column header %q in board view", label)
		}
	}
}

func TestBoardViewView_MediumWidth(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 120 // Medium: Blocked should be hidden.
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
		{ID: "b", Status: PhaseWorking},
	}

	view := bv.View()

	if strings.Contains(view, "Blocked") {
		t.Error("Blocked column should not appear at medium width")
	}
	// Queued and Running should still appear.
	if !strings.Contains(view, "Queued") {
		t.Error("expected Queued column at medium width")
	}
	if !strings.Contains(view, "Running") {
		t.Error("expected Running column at medium width")
	}
}

func TestBoardViewView_ShouldFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		width    int
		fallback bool
	}{
		{"narrow", 80, true},
		{"medium", 120, false},
		{"wide", 150, false},
		{"zero", 0, false}, // Zero width means not yet measured.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bv := BoardView{Width: tc.width}
			if bv.ShouldFallback() != tc.fallback {
				t.Errorf("ShouldFallback() = %v for width %d, want %v",
					bv.ShouldFallback(), tc.width, tc.fallback)
			}
		})
	}
}

func TestBoardViewView_PhaseEntriesShowIcons(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "done-phase", Title: "Setup", Status: PhaseDone},
		{ID: "work-phase", Title: "Auth", Status: PhaseWorking},
		{ID: "fail-phase", Title: "Tests", Status: PhaseFailed},
		{ID: "gate-phase", Title: "Review", Status: PhaseGate},
	}

	view := bv.View()

	icons := []string{iconDone, iconWorking, iconFailed, iconGate}
	for _, icon := range icons {
		if !strings.Contains(view, icon) {
			t.Errorf("expected icon %q in board view", icon)
		}
	}
}

func TestBoardViewView_PhaseEntriesShowTitle(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "setup-id", Title: "Setup Env", Status: PhaseDone},
		{ID: "auth-id", Title: "Auth Flow", Status: PhaseWorking},
	}

	view := bv.View()

	if !strings.Contains(view, "Setup Env") {
		t.Error("expected title 'Setup Env' in board view")
	}
	if !strings.Contains(view, "Auth Flow") {
		t.Error("expected title 'Auth Flow' in board view")
	}
}

func TestBoardViewView_FallsBackToIDWhenNoTitle(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "my-phase-id", Status: PhaseDone},
	}

	view := bv.View()

	if !strings.Contains(view, "my-phase-id") {
		t.Error("expected phase ID when title is empty")
	}
}

func TestBoardViewView_Empty(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	view := bv.View()

	if view != "" {
		t.Errorf("expected empty view for no phases, got: %q", view)
	}
}

func TestBoardViewView_BlockedDistinct(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "blocked-phase", Title: "Blocked Thing", Status: PhaseWaiting, BlockedBy: "other"},
	}

	view := bv.View()

	// Blocked phases should appear under the "Blocked" column header.
	if !strings.Contains(view, "Blocked") {
		t.Error("expected Blocked column header for blocked phase")
	}
	if !strings.Contains(view, "Blocked Thing") {
		t.Error("expected blocked phase title in view")
	}
}

func TestBoardViewView_EmptyColumnsDegrade(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	// Only queued phases — all other columns empty.
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
	}

	view := bv.View()

	// Should still render all column headers without panic.
	for col := BoardColumn(0); col < colCount; col++ {
		label := columnDefs[col].Label
		if !strings.Contains(view, label) {
			t.Errorf("expected column header %q even when empty", label)
		}
	}
	// Empty columns should show the dot placeholder.
	if strings.Count(view, "·") < 1 {
		t.Error("expected empty column dot placeholder")
	}
}

func TestBoardViewView_SelectionIndicator(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "a", Title: "First", Status: PhaseWaiting},
		{ID: "b", Title: "Second", Status: PhaseWorking},
	}
	bv.Cursor = 0

	view := bv.View()

	if !strings.Contains(view, selectionIndicator) {
		t.Error("expected selection indicator in board view")
	}
}

func TestBoardViewSelectedPhase_ValidCursor(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
		{ID: "b", Status: PhaseWorking},
	}
	bv.Cursor = 0

	sel := bv.SelectedPhase()
	if sel == nil {
		t.Fatal("expected non-nil selected phase")
	}
	if sel.ID != "a" {
		t.Errorf("expected phase 'a', got %q", sel.ID)
	}
}

func TestBoardViewSelectedPhase_InvalidCursor(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting},
	}
	bv.Cursor = 5

	sel := bv.SelectedPhase()
	if sel != nil {
		t.Error("expected nil for out-of-range cursor")
	}
}

func TestBoardViewSelectedPhase_Empty(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	sel := bv.SelectedPhase()
	if sel != nil {
		t.Error("expected nil for empty phases")
	}
}

func TestBoardViewVisibleColumns_FullWidth(t *testing.T) {
	t.Parallel()
	bv := BoardView{Width: 150}
	cols := bv.visibleColumns()

	if len(cols) != 6 {
		t.Errorf("expected 6 columns at full width, got %d", len(cols))
	}
}

func TestBoardViewVisibleColumns_MediumWidth(t *testing.T) {
	t.Parallel()
	bv := BoardView{Width: 120}
	cols := bv.visibleColumns()

	if len(cols) != 5 {
		t.Errorf("expected 5 columns at medium width, got %d", len(cols))
	}

	// Blocked should not be in the list at medium width.
	for _, col := range cols {
		if col == ColBlocked {
			t.Error("Blocked should not appear at medium width")
		}
	}
}

func TestBoardViewView_ProgressIndicator(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Phase One", Status: PhaseWorking, Cycles: 2, MaxCycles: 5, CostUSD: 0.42},
	}

	view := bv.View()

	if !strings.Contains(view, "[2/5]") {
		t.Errorf("expected progress indicator [2/5] in board view, got:\n%s", view)
	}
}

func TestBoardViewView_ProgressColorGreen(t *testing.T) {
	t.Parallel()
	// Early cycle: 1/5 = 20% should be green.
	result := renderProgress(1, 5)
	if !strings.Contains(result, "[1/5]") {
		t.Errorf("expected [1/5] in progress, got: %s", result)
	}
}

func TestBoardViewView_ProgressColorYellow(t *testing.T) {
	t.Parallel()
	// >60%: 4/5 = 80% should be yellow.
	result := renderProgress(4, 5)
	if !strings.Contains(result, "[4/5]") {
		t.Errorf("expected [4/5] in progress, got: %s", result)
	}
}

func TestBoardViewView_ProgressColorRed(t *testing.T) {
	t.Parallel()
	// Final cycle: 5/5 = 100% should be red.
	result := renderProgress(5, 5)
	if !strings.Contains(result, "[5/5]") {
		t.Errorf("expected [5/5] in progress, got: %s", result)
	}
}

func TestBoardViewView_HealthDot(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Healthy", Status: PhaseWorking, Cycles: 1, MaxCycles: 5},
	}

	view := bv.View()

	if !strings.Contains(view, healthDot) {
		t.Error("expected health dot in board view for working phase")
	}
}

func TestBoardViewView_CostBadge(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Costly", Status: PhaseWorking, Cycles: 1, MaxCycles: 5, CostUSD: 1.23},
	}

	view := bv.View()

	if !strings.Contains(view, "$1.23") {
		t.Errorf("expected cost badge $1.23 in board view, got:\n%s", view)
	}
}

func TestBoardViewView_ActivitySubtitle(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Active", Status: PhaseWorking, Cycles: 1, MaxCycles: 5, LastActivity: "coding..."},
	}

	view := bv.View()

	if !strings.Contains(view, "coding...") {
		t.Errorf("expected activity subtitle 'coding...' in board view, got:\n%s", view)
	}
}

func TestBoardViewView_AttentionMarkerHail(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Needs Help", Status: PhaseWorking, HasPendingHails: true, Cycles: 1, MaxCycles: 5},
	}

	view := bv.View()

	if !strings.Contains(view, "!") {
		t.Error("expected attention marker '!' for phase with pending hails")
	}
}

func TestBoardViewView_AttentionMarkerGate(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Gate Phase", Status: PhaseGate},
	}

	view := bv.View()

	if !strings.Contains(view, "!") {
		t.Error("expected attention marker '!' for gate phase")
	}
}

func TestBoardViewView_NoActivityForNonRunning(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Done Phase", Status: PhaseDone, CostUSD: 0.50, Cycles: 3, MaxCycles: 5, LastActivity: "coding..."},
	}

	view := bv.View()

	// Activity subtitle should only show for running phases.
	if strings.Contains(view, "coding...") {
		t.Error("activity subtitle should not appear on done phases")
	}
}

func TestBoardViewView_CardCompactness(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150
	bv.Phases = []PhaseEntry{
		{ID: "p1", Title: "Full Card", Status: PhaseWorking, Cycles: 2, MaxCycles: 5,
			CostUSD: 0.42, LastActivity: "reviewing...", HasPendingHails: true},
	}

	view := bv.View()

	// Count lines for the running column entries (should be ≤5 per card).
	// The card has: title line, meta line (health+progress+cost), activity line = 3 lines.
	lines := strings.Split(view, "\n")
	cardLines := 0
	for _, l := range lines {
		if strings.Contains(l, "Full Card") || strings.Contains(l, "[2/5]") ||
			strings.Contains(l, "reviewing...") || strings.Contains(l, "$0.42") {
			cardLines++
		}
	}
	if cardLines > 5 {
		t.Errorf("card should be compact (≤5 lines), found %d card-related lines", cardLines)
	}
}

func TestPhaseHealthSignal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phase  PhaseEntry
		expect PhaseHealth
	}{
		{"early cycle green", PhaseEntry{Status: PhaseWorking, Cycles: 1, MaxCycles: 5}, HealthGreen},
		{"60pct still green", PhaseEntry{Status: PhaseWorking, Cycles: 3, MaxCycles: 5}, HealthGreen},
		{"over 60pct yellow", PhaseEntry{Status: PhaseWorking, Cycles: 4, MaxCycles: 5}, HealthYellow},
		{"over 60pct satisfied green", PhaseEntry{Status: PhaseWorking, Cycles: 4, MaxCycles: 5, ReviewerSatisfied: true}, HealthGreen},
		{"final cycle red", PhaseEntry{Status: PhaseWorking, Cycles: 5, MaxCycles: 5}, HealthRed},
		{"final cycle satisfied green", PhaseEntry{Status: PhaseWorking, Cycles: 5, MaxCycles: 5, ReviewerSatisfied: true}, HealthGreen},
		{"pending hails red", PhaseEntry{Status: PhaseWorking, Cycles: 1, MaxCycles: 5, HasPendingHails: true}, HealthRed},
		{"gate yellow", PhaseEntry{Status: PhaseGate}, HealthYellow},
		{"done green", PhaseEntry{Status: PhaseDone}, HealthGreen},
		{"waiting green", PhaseEntry{Status: PhaseWaiting}, HealthGreen},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.phase.Health()
			if got != tc.expect {
				t.Errorf("Health() = %d, want %d", got, tc.expect)
			}
		})
	}
}

func TestRenderProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cycles    int
		maxCycles int
		contains  string
	}{
		{"early", 1, 5, "[1/5]"},
		{"mid", 3, 5, "[3/5]"},
		{"late", 4, 5, "[4/5]"},
		{"final", 5, 5, "[5/5]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := renderProgress(tc.cycles, tc.maxCycles)
			if !strings.Contains(result, tc.contains) {
				t.Errorf("renderProgress(%d, %d) = %q, want containing %q",
					tc.cycles, tc.maxCycles, result, tc.contains)
			}
		})
	}
}

func TestRenderCostBadge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cost     float64
		budget   float64
		contains string
	}{
		{"normal cost", 0.42, 5.0, "$0.42"},
		{"near budget", 4.50, 5.0, "$4.50"},
		{"no budget", 1.23, 0, "$1.23"},
		{"zero cost", 0, 5.0, "$0.00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := renderCostBadge(tc.cost, tc.budget)
			if !strings.Contains(result, tc.contains) {
				t.Errorf("renderCostBadge(%f, %f) = %q, want containing %q",
					tc.cost, tc.budget, result, tc.contains)
			}
		})
	}
}

func TestRenderHealthDot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		health PhaseHealth
	}{
		{"green", HealthGreen},
		{"yellow", HealthYellow},
		{"red", HealthRed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := renderHealthDot(tc.health)
			if !strings.Contains(result, healthDot) {
				t.Errorf("renderHealthDot(%d) should contain health dot character", tc.health)
			}
		})
	}
}

func TestNebulaViewSetPhaseActivity(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "p1", Status: PhaseWorking},
	}

	nv.SetPhaseActivity("p1", "reviewing...")

	if nv.Phases[0].LastActivity != "reviewing..." {
		t.Errorf("expected LastActivity %q, got %q", "reviewing...", nv.Phases[0].LastActivity)
	}
}

func TestNebulaViewSetPhaseHails(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "p1", Status: PhaseWorking},
	}

	nv.SetPhaseHails("p1", true)
	if !nv.Phases[0].HasPendingHails {
		t.Error("expected HasPendingHails to be true")
	}

	nv.SetPhaseHails("p1", false)
	if nv.Phases[0].HasPendingHails {
		t.Error("expected HasPendingHails to be false after clearing")
	}
}

func TestNebulaViewSetPhaseReviewerSatisfied(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "p1", Status: PhaseWorking},
	}

	nv.SetPhaseReviewerSatisfied("p1", true)
	if !nv.Phases[0].ReviewerSatisfied {
		t.Error("expected ReviewerSatisfied to be true")
	}
}

func TestNebulaViewSetPhaseMaxBudget(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "p1", Status: PhaseWorking},
	}

	nv.SetPhaseMaxBudget("p1", 5.0)
	if nv.Phases[0].MaxBudgetUSD != 5.0 {
		t.Errorf("expected MaxBudgetUSD 5.0, got %f", nv.Phases[0].MaxBudgetUSD)
	}
}

func TestStatusToColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		phase  PhaseEntry
		expect BoardColumn
	}{
		{"waiting", PhaseEntry{Status: PhaseWaiting}, ColQueued},
		{"waiting-blocked", PhaseEntry{Status: PhaseWaiting, BlockedBy: "x"}, ColBlocked},
		{"working", PhaseEntry{Status: PhaseWorking}, ColRunning},
		{"done", PhaseEntry{Status: PhaseDone}, ColDone},
		{"skipped", PhaseEntry{Status: PhaseSkipped}, ColDone},
		{"failed", PhaseEntry{Status: PhaseFailed}, ColFailed},
		{"gate", PhaseEntry{Status: PhaseGate}, ColReview},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statusToColumn(tc.phase)
			if got != tc.expect {
				t.Errorf("statusToColumn(%s) = %d, want %d", tc.name, got, tc.expect)
			}
		})
	}
}
