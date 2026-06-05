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
