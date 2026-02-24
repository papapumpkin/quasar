package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tycho"
)

func TestAppModelHandlesMsgEntanglementUpdate(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	ents := []fabric.Entanglement{
		{ID: 1, Producer: "phase-1", Kind: "function", Name: "Foo"},
		{ID: 2, Producer: "phase-2", Kind: "interface", Name: "Bar"},
	}

	var tm tea.Model = m
	tm, _ = tm.Update(MsgEntanglementUpdate{Entanglements: ents})
	am := tm.(AppModel)

	if len(am.Entanglements) != 2 {
		t.Errorf("Entanglements = %d, want 2", len(am.Entanglements))
	}
	if am.Entanglements[0].Name != "Foo" {
		t.Errorf("Entanglements[0].Name = %q, want %q", am.Entanglements[0].Name, "Foo")
	}
}

func TestAppModelHandlesMsgDiscoveryPosted(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	disc := fabric.Discovery{
		ID:         1,
		SourceTask: "phase-1",
		Kind:       fabric.DiscoveryFileConflict,
		Detail:     "conflicting file ownership",
	}

	var tm tea.Model = m
	tm, _ = tm.Update(MsgDiscoveryPosted{Discovery: disc})
	am := tm.(AppModel)

	if len(am.Discoveries) != 1 {
		t.Fatalf("Discoveries = %d, want 1", len(am.Discoveries))
	}
	if am.Discoveries[0].Kind != fabric.DiscoveryFileConflict {
		t.Errorf("Discoveries[0].Kind = %q, want %q", am.Discoveries[0].Kind, fabric.DiscoveryFileConflict)
	}
	// Should also create a toast notification.
	if len(am.Toasts) == 0 {
		t.Error("expected at least one toast notification")
	}
}

func TestAppModelHandlesMsgHail(t *testing.T) {
	t.Parallel()

	disc := fabric.Discovery{
		ID:         1,
		SourceTask: "phase-3",
		Kind:       fabric.DiscoveryRequirementsAmbiguity,
		Detail:     "unclear API contract",
	}

	t.Run("shows overlay when board view is active", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 120
		m.Height = 24
		m.ActiveTab = TabBoard
		m.BoardActive = true
		m.Depth = DepthPhases

		var tm tea.Model = m
		tm, _ = tm.Update(MsgHail{PhaseID: "phase-3", Discovery: disc})
		am := tm.(AppModel)

		if am.Hail == nil {
			t.Error("expected hail overlay to be set in board mode")
		}
		if am.Hail != nil && am.Hail.PhaseID != "phase-3" {
			t.Errorf("expected hail phase %q, got %q", "phase-3", am.Hail.PhaseID)
		}
	})

	t.Run("falls back to toast outside board view", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.ActiveTab = TabEntanglements

		var tm tea.Model = m
		tm, _ = tm.Update(MsgHail{PhaseID: "phase-3", Discovery: disc})
		am := tm.(AppModel)

		if am.Hail != nil {
			t.Error("expected hail overlay to be nil outside board view")
		}
		if len(am.Toasts) == 0 {
			t.Error("expected at least one toast notification for hail")
		}
	})
}

func TestAppModelHandlesMsgScratchpadEntry(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	entry := MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "phase-1",
		Text:      "discovery: missing API endpoint",
	}

	var tm tea.Model = m
	tm, _ = tm.Update(entry)
	am := tm.(AppModel)

	if len(am.Scratchpad) != 1 {
		t.Fatalf("Scratchpad = %d, want 1", len(am.Scratchpad))
	}
	if am.Scratchpad[0].Text != "discovery: missing API endpoint" {
		t.Errorf("Scratchpad[0].Text = %q, want %q", am.Scratchpad[0].Text, "discovery: missing API endpoint")
	}
}

func TestAppModelHandlesMsgStaleWarning(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	items := []tycho.StaleItem{
		{Kind: "claim", ID: "path/to/file.go", Age: 10 * time.Minute, Details: "old claim"},
		{Kind: "task", ID: "phase-stuck", Age: 30 * time.Minute, Details: "not progressing"},
	}

	var tm tea.Model = m
	tm, _ = tm.Update(MsgStaleWarning{Items: items})
	am := tm.(AppModel)

	if len(am.StaleItems) != 2 {
		t.Errorf("StaleItems = %d, want 2", len(am.StaleItems))
	}
	// Should produce a toast notification.
	if len(am.Toasts) == 0 {
		t.Error("expected at least one toast notification for stale warning")
	}
}

func TestAppModelHandlesMsgStaleWarningEmpty(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m
	tm, _ = tm.Update(MsgStaleWarning{Items: nil})
	am := tm.(AppModel)

	if len(am.StaleItems) != 0 {
		t.Errorf("StaleItems = %d, want 0", len(am.StaleItems))
	}
	// Empty stale warning should not produce a toast.
	if len(am.Toasts) != 0 {
		t.Errorf("expected no toasts for empty stale warning, got %d", len(am.Toasts))
	}
}

func TestAppModelMultipleDiscoveriesAccumulate(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m
	tm, _ = tm.Update(MsgDiscoveryPosted{Discovery: fabric.Discovery{ID: 1, Kind: "file_conflict"}})
	tm, _ = tm.Update(MsgDiscoveryPosted{Discovery: fabric.Discovery{ID: 2, Kind: "budget_alert"}})
	tm, _ = tm.Update(MsgDiscoveryPosted{Discovery: fabric.Discovery{ID: 3, Kind: "missing_dependency"}})
	am := tm.(AppModel)

	if len(am.Discoveries) != 3 {
		t.Errorf("Discoveries = %d, want 3", len(am.Discoveries))
	}
}

func TestAppModelEntanglementUpdateReplaces(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m
	tm, _ = tm.Update(MsgEntanglementUpdate{Entanglements: []fabric.Entanglement{
		{ID: 1, Name: "Foo"},
	}})
	tm, _ = tm.Update(MsgEntanglementUpdate{Entanglements: []fabric.Entanglement{
		{ID: 1, Name: "Foo"},
		{ID: 2, Name: "Bar"},
	}})
	am := tm.(AppModel)

	// Second update should replace, not accumulate.
	if len(am.Entanglements) != 2 {
		t.Errorf("Entanglements = %d, want 2 (should replace)", len(am.Entanglements))
	}
}

func TestAppModelEntanglementUpdateSyncsView(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	ents := []fabric.Entanglement{
		{ID: 1, Producer: "phase-1", Name: "Foo", Status: "pending"},
		{ID: 2, Producer: "phase-2", Name: "Bar", Status: "fulfilled"},
	}

	var tm tea.Model = m
	tm, _ = tm.Update(MsgEntanglementUpdate{Entanglements: ents})
	am := tm.(AppModel)

	// The EntanglementView field should be synced with Entanglements.
	if len(am.EntanglementView.Entanglements) != 2 {
		t.Errorf("EntanglementView.Entanglements = %d, want 2", len(am.EntanglementView.Entanglements))
	}
}

func TestAppModelEntanglementCursorClampedOnUpdate(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.EntanglementView.Cursor = 5 // out-of-bounds cursor

	ents := []fabric.Entanglement{
		{ID: 1, Producer: "p", Name: "X", Status: "pending"},
		{ID: 2, Producer: "p", Name: "Y", Status: "fulfilled"},
	}

	var tm tea.Model = m
	tm, _ = tm.Update(MsgEntanglementUpdate{Entanglements: ents})
	am := tm.(AppModel)

	// Cursor should be clamped to last valid index.
	if am.EntanglementView.Cursor != 1 {
		t.Errorf("EntanglementView.Cursor = %d, want 1 (clamped)", am.EntanglementView.Cursor)
	}
}

func TestAppModelEntanglementCursorNavigatesOnTab(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.ActiveTab = TabEntanglements
	m.EntanglementView.Entanglements = []fabric.Entanglement{
		{ID: 1, Producer: "p", Name: "A", Status: "pending"},
		{ID: 2, Producer: "p", Name: "B", Status: "fulfilled"},
		{ID: 3, Producer: "q", Name: "C", Status: "disputed"},
	}

	// Move down should increment entanglement cursor, not nebula cursor.
	m.moveDown()
	if m.EntanglementView.Cursor != 1 {
		t.Errorf("after moveDown: EntanglementView.Cursor = %d, want 1", m.EntanglementView.Cursor)
	}
	if m.NebulaView.Cursor != 0 {
		t.Errorf("after moveDown: NebulaView.Cursor = %d, want 0 (should not move)", m.NebulaView.Cursor)
	}

	// Move down again.
	m.moveDown()
	if m.EntanglementView.Cursor != 2 {
		t.Errorf("after second moveDown: EntanglementView.Cursor = %d, want 2", m.EntanglementView.Cursor)
	}

	// Move up should decrement entanglement cursor.
	m.moveUp()
	if m.EntanglementView.Cursor != 1 {
		t.Errorf("after moveUp: EntanglementView.Cursor = %d, want 1", m.EntanglementView.Cursor)
	}
}

func TestPhaseUIBridgeFabricMethodsDoNotPanic(t *testing.T) {
	t.Parallel()

	model := NewAppModel(ModeNebula)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.AfterFunc(200*time.Millisecond, func() { p.Quit() })
		_, _ = p.Run()
	}()
	time.Sleep(50 * time.Millisecond)

	b := NewPhaseUIBridge(p, "phase-test", "")

	// None of these should panic.
	b.EntanglementPublished([]fabric.Entanglement{
		{ID: 1, Producer: "phase-test", Kind: "function", Name: "TestFunc"},
	})
	b.DiscoveryPosted(fabric.Discovery{
		ID:         1,
		SourceTask: "phase-test",
		Kind:       fabric.DiscoveryFileConflict,
		Detail:     "test conflict",
	})
	b.Hail("phase-test", fabric.Discovery{
		ID:         2,
		SourceTask: "phase-test",
		Kind:       fabric.DiscoveryRequirementsAmbiguity,
		Detail:     "test hail",
	})
	b.ScratchpadNote("phase-test", "test note")

	<-done
}
