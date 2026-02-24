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

	if len(cols) != 7 {
		t.Errorf("expected 7 columns at full width, got %d", len(cols))
	}
}

func TestBoardViewVisibleColumns_MediumWidth(t *testing.T) {
	t.Parallel()
	bv := BoardView{Width: 120}
	cols := bv.visibleColumns()

	if len(cols) != 5 {
		t.Errorf("expected 5 columns at medium width, got %d", len(cols))
	}

	// Scanning and Blocked should not be in the list.
	for _, col := range cols {
		if col == ColScanning {
			t.Error("Scanning should not appear at medium width")
		}
		if col == ColBlocked {
			t.Error("Blocked should not appear at medium width")
		}
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
