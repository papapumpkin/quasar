package tui

import (
	"strings"
	"testing"
)

func TestWorkerCardView_BasicContent(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:    "implement-auth",
		QuasarID:   "q-1",
		Cycle:      2,
		MaxCycles:  5,
		TokensUsed: 12000,
		Claims:     []string{"internal/auth/login.go", "internal/auth/token.go"},
		Activity:   "coding...",
		AgentRole:  "coder",
	}

	out := wc.View(40)

	checks := []string{
		"implement-auth",
		"q-1",
		"cycle 2/5",
		"tokens 12000",
		"login.go",
		"token.go",
		"coding...",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestWorkerCardView_ReviewerColor(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:   "lint-phase",
		QuasarID:  "q-2",
		Cycle:     1,
		MaxCycles: 3,
		AgentRole: "reviewer",
	}

	out := wc.View(40)
	if !strings.Contains(out, "reviewing...") {
		t.Errorf("expected reviewing... activity, got:\n%s", out)
	}
}

func TestWorkerCardView_DefaultActivity(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:   "some-phase",
		QuasarID:  "q-3",
		AgentRole: "",
	}

	out := wc.View(40)
	if !strings.Contains(out, "working...") {
		t.Errorf("expected working... fallback activity, got:\n%s", out)
	}
}

func TestWorkerCardView_ClaimsTruncated(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:  "many-files",
		QuasarID: "q-1",
		Claims: []string{
			"file1.go",
			"file2.go",
			"file3.go",
			"file4.go",
			"file5.go",
		},
		AgentRole: "coder",
	}

	out := wc.View(40)
	if !strings.Contains(out, "file1.go") {
		t.Errorf("expected file1.go in claims, got:\n%s", out)
	}
	if !strings.Contains(out, "file3.go") {
		t.Errorf("expected file3.go in claims (3rd entry), got:\n%s", out)
	}
	if !strings.Contains(out, "+2 more") {
		t.Errorf("expected +2 more for truncated claims, got:\n%s", out)
	}
	if strings.Contains(out, "file4.go") {
		t.Errorf("should not show file4.go (beyond max 3), got:\n%s", out)
	}
}

func TestWorkerCardView_NoClaims(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:   "no-claims",
		QuasarID:  "q-1",
		AgentRole: "coder",
	}

	out := wc.View(40)
	// Should not contain claim-like artifacts.
	if strings.Contains(out, "+") && strings.Contains(out, "more") {
		t.Errorf("unexpected claim truncation marker in no-claims card:\n%s", out)
	}
}

func TestWorkerCardView_MinWidth(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:   "narrow",
		QuasarID:  "q-1",
		AgentRole: "coder",
	}

	// Very small width — should not panic.
	out := wc.View(10)
	if out == "" {
		t.Error("View() returned empty string for small width")
	}
}

func TestRenderWorkerCards_Empty(t *testing.T) {
	t.Parallel()
	out := RenderWorkerCards(nil, 120)
	if out != "" {
		t.Errorf("expected empty string for nil cards, got: %q", out)
	}
}

func TestRenderWorkerCards_SingleCard(t *testing.T) {
	t.Parallel()
	cards := []*WorkerCard{
		{PhaseID: "phase-a", QuasarID: "q-1", Cycle: 1, MaxCycles: 3, AgentRole: "coder"},
	}
	out := RenderWorkerCards(cards, 120)
	if !strings.Contains(out, "phase-a") {
		t.Errorf("expected phase-a in output:\n%s", out)
	}
	if !strings.Contains(out, "q-1") {
		t.Errorf("expected q-1 in output:\n%s", out)
	}
}

func TestRenderWorkerCards_MultipleCards(t *testing.T) {
	t.Parallel()
	cards := []*WorkerCard{
		{PhaseID: "phase-a", QuasarID: "q-1", AgentRole: "coder"},
		{PhaseID: "phase-b", QuasarID: "q-2", AgentRole: "reviewer"},
	}
	out := RenderWorkerCards(cards, 120)
	if !strings.Contains(out, "phase-a") {
		t.Errorf("expected phase-a in output:\n%s", out)
	}
	if !strings.Contains(out, "phase-b") {
		t.Errorf("expected phase-b in output:\n%s", out)
	}
}

func TestRenderWorkerCards_NarrowTerminal(t *testing.T) {
	t.Parallel()
	cards := []*WorkerCard{
		{PhaseID: "a", QuasarID: "q-1", AgentRole: "coder"},
		{PhaseID: "b", QuasarID: "q-2", AgentRole: "reviewer"},
	}
	// Narrow terminal should trigger vertical stacking.
	out := RenderWorkerCards(cards, 40)
	if !strings.Contains(out, "q-1") || !strings.Contains(out, "q-2") {
		t.Errorf("expected both cards in narrow output:\n%s", out)
	}
}

func TestActiveWorkerCards_Sorted(t *testing.T) {
	t.Parallel()
	cards := map[string]*WorkerCard{
		"phase-b": {PhaseID: "phase-b", QuasarID: "q-3"},
		"phase-a": {PhaseID: "phase-a", QuasarID: "q-1"},
		"phase-c": {PhaseID: "phase-c", QuasarID: "q-2"},
	}
	active := ActiveWorkerCards(cards)
	if len(active) != 3 {
		t.Fatalf("expected 3 active cards, got %d", len(active))
	}
	// Should be sorted by QuasarID.
	if active[0].QuasarID != "q-1" {
		t.Errorf("first card QuasarID = %q, want q-1", active[0].QuasarID)
	}
	if active[1].QuasarID != "q-2" {
		t.Errorf("second card QuasarID = %q, want q-2", active[1].QuasarID)
	}
	if active[2].QuasarID != "q-3" {
		t.Errorf("third card QuasarID = %q, want q-3", active[2].QuasarID)
	}
}

func TestActiveWorkerCards_Empty(t *testing.T) {
	t.Parallel()
	active := ActiveWorkerCards(nil)
	if len(active) != 0 {
		t.Errorf("expected 0 active cards for nil map, got %d", len(active))
	}
}

func TestCardWidth_Clamping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		numCards  int
		termWidth int
		wantMin   int
		wantMax   int
	}{
		{"single card wide terminal", 1, 200, workerCardMinWidth, workerCardMaxWidth},
		{"two cards moderate terminal", 2, 80, workerCardMinWidth, workerCardMaxWidth},
		{"many cards narrow terminal", 5, 100, workerCardMinWidth, workerCardMaxWidth},
		{"zero cards", 0, 100, workerCardMinWidth, workerCardMinWidth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := cardWidth(tc.numCards, tc.termWidth)
			if w < tc.wantMin {
				t.Errorf("cardWidth(%d, %d) = %d, want >= %d", tc.numCards, tc.termWidth, w, tc.wantMin)
			}
			if w > tc.wantMax {
				t.Errorf("cardWidth(%d, %d) = %d, want <= %d", tc.numCards, tc.termWidth, w, tc.wantMax)
			}
		})
	}
}

func TestActivityFromRole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role string
		want string
	}{
		{"coder", "coding..."},
		{"reviewer", "reviewing..."},
		{"", "working..."},
		{"unknown", "working..."},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			got := activityFromRole(tc.role)
			if got != tc.want {
				t.Errorf("activityFromRole(%q) = %q, want %q", tc.role, got, tc.want)
			}
		})
	}
}

func TestSortWorkerCards(t *testing.T) {
	t.Parallel()
	cards := []*WorkerCard{
		{QuasarID: "q-3"},
		{QuasarID: "q-1"},
		{QuasarID: "q-2"},
	}
	sortWorkerCards(cards)
	if cards[0].QuasarID != "q-1" || cards[1].QuasarID != "q-2" || cards[2].QuasarID != "q-3" {
		t.Errorf("sort order wrong: %s, %s, %s", cards[0].QuasarID, cards[1].QuasarID, cards[2].QuasarID)
	}
}

func TestSortWorkerCards_DoubleDigits(t *testing.T) {
	t.Parallel()
	cards := []*WorkerCard{
		{QuasarID: "q-10"},
		{QuasarID: "q-2"},
		{QuasarID: "q-9"},
		{QuasarID: "q-1"},
		{QuasarID: "q-11"},
	}
	sortWorkerCards(cards)
	want := []string{"q-1", "q-2", "q-9", "q-10", "q-11"}
	for i, w := range want {
		if cards[i].QuasarID != w {
			t.Errorf("cards[%d].QuasarID = %q, want %q", i, cards[i].QuasarID, w)
		}
	}
}

func TestQuasarNum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id   string
		want int
	}{
		{"q-1", 1},
		{"q-10", 10},
		{"q-99", 99},
		{"q-0", 0},
		{"invalid", 0},
		{"q-", 0},
		{"q-abc", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			got := quasarNum(tc.id)
			if got != tc.want {
				t.Errorf("quasarNum(%q) = %d, want %d", tc.id, got, tc.want)
			}
		})
	}
}

func TestWorkerCardView_LongPhaseName(t *testing.T) {
	t.Parallel()
	wc := &WorkerCard{
		PhaseID:   "this-is-a-very-long-phase-name-that-should-be-truncated",
		QuasarID:  "q-1",
		AgentRole: "coder",
	}
	out := wc.View(35)
	// Should contain ellipsis for truncation.
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncated phase name with ellipsis:\n%s", out)
	}
}

func TestEnsureWorkerCard_ModelIntegration(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)

	// First call creates a new card.
	wc := m.ensureWorkerCard("phase-x")
	if wc.PhaseID != "phase-x" {
		t.Errorf("PhaseID = %q, want phase-x", wc.PhaseID)
	}
	if wc.QuasarID != "q-1" {
		t.Errorf("QuasarID = %q, want q-1", wc.QuasarID)
	}

	// Second call returns the same card.
	wc2 := m.ensureWorkerCard("phase-x")
	if wc2.QuasarID != "q-1" {
		t.Errorf("repeated call should return same card, got QuasarID = %q", wc2.QuasarID)
	}

	// Third call with different phase gets next ID.
	wc3 := m.ensureWorkerCard("phase-y")
	if wc3.QuasarID != "q-2" {
		t.Errorf("new phase QuasarID = %q, want q-2", wc3.QuasarID)
	}
}

func TestWorkerCardLifecycle(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.NebulaView.Phases = []PhaseEntry{
		{ID: "alpha", Status: PhaseWaiting},
	}

	// Simulate phase start.
	m.ensureWorkerCard("alpha")
	if len(m.WorkerCards) != 1 {
		t.Fatalf("expected 1 worker card, got %d", len(m.WorkerCards))
	}

	// Simulate phase completion.
	delete(m.WorkerCards, "alpha")
	if len(m.WorkerCards) != 0 {
		t.Fatalf("expected 0 worker cards after delete, got %d", len(m.WorkerCards))
	}
}
