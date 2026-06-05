package fleet

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestModelFoldHidesCardsAndPersists exercises the full fold path through the
// model keymap (Issue #1): pressing "f" must fold the repo under the cursor so
// its cards vanish, record the fold in ui state, persist it to disk, and remain
// unfoldable via a second "f" on the now-selectable header slot.
func TestModelFoldHidesCardsAndPersists(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "awaiting_approval")

	statePath := filepath.Join(t.TempDir(), "tui-state.json")
	m := NewModel(context.Background(), NewStore(db), statePath)

	// Apply the initial load synchronously.
	loaded := m.loadCmd()()
	updated, _ := m.Update(loaded)
	m = updated.(Model)

	if !strings.Contains(m.View(), "retry") {
		t.Fatalf("card should be visible before fold:\n%s", m.View())
	}

	// Fold the repo under the cursor.
	updated, _ = m.Update(keyRunes("f"))
	m = updated.(Model)
	if strings.Contains(m.View(), "retry") {
		t.Errorf("card should be hidden after fold:\n%s", m.View())
	}
	if !m.ui.IsFolded("papapumpkin/quasar") {
		t.Errorf("ui state should record the fold, got %+v", m.ui)
	}

	// Persisting (as on quit) must write the fold to disk.
	m.persist()
	st, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !st.IsFolded("papapumpkin/quasar") {
		t.Errorf("fold not persisted to %s, got %+v", statePath, st)
	}

	// The folded repo keeps a selectable header slot, so a second "f" unfolds it.
	updated, _ = m.Update(keyRunes("f"))
	m = updated.(Model)
	if !strings.Contains(m.View(), "retry") {
		t.Errorf("card should reappear after unfold:\n%s", m.View())
	}
	if m.ui.IsFolded("papapumpkin/quasar") {
		t.Errorf("ui state should record the unfold, got %+v", m.ui)
	}
}

// TestRepoForCursorAccountsForFoldedRepos verifies the cursor-slot accounting
// (Issue #2): a folded repo occupies exactly one header slot, so a folded repo
// between unfolded repos must not be mis-attributed to a later repo's cards.
func TestRepoForCursorAccountsForFoldedRepos(t *testing.T) {
	m := Model{lane: 0}
	m.view = Fleet{Repos: []RepoLane{
		{DisplayName: "org/a", AwaitingApproval: []NebulaCard{{ID: "a1"}, {ID: "a2"}}},
		{DisplayName: "org/b", Folded: true, AwaitingApproval: []NebulaCard{{ID: "b1"}}},
		{DisplayName: "org/c", AwaitingApproval: []NebulaCard{{ID: "c1"}}},
	}}

	// Slots: a→0,1  b(header)→2  c→3.
	for cursor, want := range map[int]string{0: "org/a", 1: "org/a", 2: "org/b", 3: "org/c"} {
		m.cursor = cursor
		if got := m.repoForCursor(); got != want {
			t.Errorf("cursor %d: repoForCursor() = %q, want %q", cursor, got, want)
		}
	}

	if got := m.laneLen(); got != 4 {
		t.Errorf("laneLen() = %d, want 4 (2 cards + 1 folded header + 1 card)", got)
	}

	// The folded header slot carries no card.
	m.cursor = 2
	if c := m.selectedNebula(); c != nil {
		t.Errorf("folded header slot should select no nebula, got %+v", c)
	}

	// The card after the folded repo resolves to the correct repo's card.
	m.cursor = 3
	if c := m.selectedNebula(); c == nil || c.ID != "c1" {
		t.Errorf("cursor 3 should select c1, got %+v", c)
	}
}

// TestApproveJumpShowsStartingPlaceholder verifies that A (approve + jump)
// opens the detail view on a transient placeholder: the architect run has no
// RunID yet (the Phase-5 supervisor creates it), so the view shows an explicit
// "starting" message rather than a half-empty run header.
func TestApproveJumpShowsStartingPlaceholder(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "awaiting_approval")

	statePath := filepath.Join(t.TempDir(), "tui-state.json")
	m := NewModel(context.Background(), NewStore(db), statePath)
	loaded := m.loadCmd()()
	updated, _ := m.Update(loaded)
	m = updated.(Model)

	updated, _ = m.Update(keyRunes("A"))
	m = updated.(Model)

	if m.mode != modeDetail {
		t.Fatalf("A should open the detail view, mode = %v", m.mode)
	}
	if m.detail.RunID != "" {
		t.Errorf("placeholder run should have an empty RunID, got %q", m.detail.RunID)
	}
	if view := m.View(); !strings.Contains(view, "architect starting") {
		t.Errorf("detail view should show the starting placeholder:\n%s", view)
	}

	// The transient-placeholder tick reschedules itself without a trace poll.
	if _, cmd := m.Update(tickMsg{}); cmd == nil {
		t.Error("tick should still reschedule while on the placeholder")
	}
}

// TestNebulaDetailViewRendersProjection verifies the read-only nebula
// inspection view renders the title, status, and source, always shows the
// footer, and gates the optional SourceURL separator and Description block on
// their fields being non-empty (the only branching logic in the method).
func TestNebulaDetailViewRendersProjection(t *testing.T) {
	tests := []struct {
		name    string
		detail  NebulaDetail
		want    []string
		notWant []string
	}{
		{
			name: "populated source url and description",
			detail: NebulaDetail{
				Title:       "retry flaky uploads",
				Status:      "awaiting_approval",
				SourceLabel: "#142",
				SourceURL:   "https://github.com/papapumpkin/quasar/issues/142",
				Description: "Uploads intermittently fail under load.",
			},
			want: []string{
				"retry flaky uploads",
				"awaiting_approval",
				"#142",
				"https://github.com/papapumpkin/quasar/issues/142",
				"Uploads intermittently fail under load.",
				"[b] back  [q] quit",
			},
		},
		{
			name: "empty source url and description",
			detail: NebulaDetail{
				Title:       "manual draft",
				Status:      "draft",
				SourceLabel: "manual",
			},
			want: []string{
				"manual draft",
				"draft",
				"manual",
				"[b] back  [q] quit",
			},
			notWant: []string{"—", "https://"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := Model{mode: modeNebulaDetail, nebDetail: tt.detail}
			view := m.View()
			for _, want := range tt.want {
				if !strings.Contains(view, want) {
					t.Errorf("view missing %q:\n%s", want, view)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(view, notWant) {
					t.Errorf("view should omit %q:\n%s", notWant, view)
				}
			}
		})
	}
}

// TestSelectionMatchesCursor verifies selection() resolves the cursor to the
// same slot the navigation/action helpers act on (Issue #1): the render
// highlight must point at exactly the card approve/reject will target, and a
// folded repo's header resolves to the headerCard slot.
func TestSelectionMatchesCursor(t *testing.T) {
	m := Model{lane: 0}
	m.view = Fleet{Repos: []RepoLane{
		{DisplayName: "org/a", AwaitingApproval: []NebulaCard{{ID: "a1"}, {ID: "a2"}}},
		{DisplayName: "org/b", Folded: true, AwaitingApproval: []NebulaCard{{ID: "b1"}}},
		{DisplayName: "org/c", AwaitingApproval: []NebulaCard{{ID: "c1"}}},
	}}

	cases := []struct {
		cursor  int
		repoIdx int
		cardIdx int
	}{
		{0, 0, 0},          // org/a first card
		{1, 0, 1},          // org/a second card
		{2, 1, headerCard}, // org/b folded header
		{3, 2, 0},          // org/c first card
	}
	for _, c := range cases {
		m.cursor = c.cursor
		sel := m.selection()
		if !sel.Active || sel.Lane != 0 || sel.RepoIdx != c.repoIdx || sel.CardIdx != c.cardIdx {
			t.Errorf("cursor %d: selection = %+v, want repo %d card %d", c.cursor, sel, c.repoIdx, c.cardIdx)
		}
	}

	// Empty lane → inactive selection (no highlight drawn).
	empty := Model{lane: 0, view: Fleet{Repos: []RepoLane{{DisplayName: "org/x"}}}}
	if sel := empty.selection(); sel.Active {
		t.Errorf("empty lane should yield inactive selection, got %+v", sel)
	}
}
