package nebula

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aaronsalm/quasar/internal/beads"
)

// --- Parse tests ---

func TestLoad_ValidNebula(t *testing.T) {
	n, err := Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if n.Manifest.Nebula.Name != "test-nebula" {
		t.Errorf("expected name 'test-nebula', got %q", n.Manifest.Nebula.Name)
	}
	if n.Manifest.Nebula.Description != "A test nebula for unit tests" {
		t.Errorf("unexpected description: %q", n.Manifest.Nebula.Description)
	}

	if len(n.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(n.Phases))
	}

	// Phases should be in directory order.
	phaseByID := make(map[string]PhaseSpec)
	for _, phase := range n.Phases {
		phaseByID[phase.ID] = phase
	}

	// Check first phase inherits defaults.
	first := phaseByID["first-task"]
	if first.Title != "First test task" {
		t.Errorf("first phase title: %q", first.Title)
	}
	if first.Type != "task" {
		t.Errorf("first phase type should inherit default 'task', got %q", first.Type)
	}
	if first.Priority != 2 {
		t.Errorf("first phase priority should inherit default 2, got %d", first.Priority)
	}
	if len(first.Labels) != 1 || first.Labels[0] != "test" {
		t.Errorf("first phase labels should inherit default, got %v", first.Labels)
	}
	if first.Body == "" {
		t.Error("first phase body should not be empty")
	}

	// Check second phase overrides defaults.
	second := phaseByID["second-task"]
	if second.Type != "feature" {
		t.Errorf("second phase type: %q", second.Type)
	}
	if second.Priority != 1 {
		t.Errorf("second phase priority: %d", second.Priority)
	}
	if len(second.DependsOn) != 1 || second.DependsOn[0] != "first-task" {
		t.Errorf("second phase depends_on: %v", second.DependsOn)
	}
	if len(second.Labels) != 1 || second.Labels[0] != "custom-label" {
		t.Errorf("second phase labels: %v", second.Labels)
	}
}

func TestLoad_NoManifest(t *testing.T) {
	_, err := Load("testdata/nonexistent")
	if !errors.Is(err, ErrNoManifest) {
		t.Errorf("expected ErrNoManifest, got %v", err)
	}
}

// --- Validate tests ---

func TestValidate_Valid(t *testing.T) {
	n, err := Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	errs := Validate(n)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got %d:", len(errs))
		for _, e := range errs {
			t.Errorf("  %s", e.Error())
		}
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	n, err := Load("testdata/invalid-dup")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	errs := Validate(n)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for duplicate IDs")
	}

	found := false
	for _, e := range errs {
		if errors.Is(e.Err, ErrDuplicateID) {
			found = true
		}
	}
	if !found {
		t.Error("expected ErrDuplicateID in validation errors")
	}
}

func TestValidate_Cycle(t *testing.T) {
	n, err := Load("testdata/invalid-cycle")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	errs := Validate(n)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for cycle")
	}

	found := false
	for _, e := range errs {
		if errors.Is(e.Err, ErrDependencyCycle) {
			found = true
		}
	}
	if !found {
		t.Error("expected ErrDependencyCycle in validation errors")
	}
}

func TestValidate_MissingTitle(t *testing.T) {
	n, err := Load("testdata/invalid-missing")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	errs := Validate(n)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for missing fields")
	}

	found := false
	for _, e := range errs {
		if errors.Is(e.Err, ErrMissingField) {
			found = true
		}
	}
	if !found {
		t.Error("expected ErrMissingField in validation errors")
	}
}

// --- Graph tests ---

func TestGraph_Sort(t *testing.T) {
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}

	g := NewGraph(phases)
	sorted, err := g.Sort()
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	// a must come before b, and b before c.
	pos := make(map[string]int)
	for i, id := range sorted {
		pos[id] = i
	}

	if pos["a"] >= pos["b"] {
		t.Errorf("a should come before b, got a=%d b=%d", pos["a"], pos["b"])
	}
	if pos["b"] >= pos["c"] {
		t.Errorf("b should come before c, got b=%d c=%d", pos["b"], pos["c"])
	}
}

func TestGraph_SortCycleDetection(t *testing.T) {
	phases := []PhaseSpec{
		{ID: "x", DependsOn: []string{"y"}},
		{ID: "y", DependsOn: []string{"x"}},
	}

	g := NewGraph(phases)
	_, err := g.Sort()
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("expected ErrDependencyCycle, got %v", err)
	}
}

func TestGraph_Ready(t *testing.T) {
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c"},
	}

	g := NewGraph(phases)

	// Initially, a and c should be ready.
	ready := g.Ready(map[string]bool{})
	sort.Strings(ready)
	if len(ready) != 2 || ready[0] != "a" || ready[1] != "c" {
		t.Errorf("expected [a, c] ready, got %v", ready)
	}

	// After a is done, b should become ready.
	ready = g.Ready(map[string]bool{"a": true})
	sort.Strings(ready)
	if len(ready) != 2 || ready[0] != "b" || ready[1] != "c" {
		t.Errorf("expected [b, c] ready after a done, got %v", ready)
	}
}

// --- State tests ---

func TestState_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	state := &State{
		Version:    1,
		NebulaName: "test",
		Phases:     make(map[string]*PhaseState),
	}
	state.SetPhaseState("phase-1", "bead-abc", PhaseStatusCreated)

	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Fatalf("state file not found: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.NebulaName != "test" {
		t.Errorf("expected name 'test', got %q", loaded.NebulaName)
	}
	ps, ok := loaded.Phases["phase-1"]
	if !ok {
		t.Fatal("phase-1 not found in loaded state")
	}
	if ps.BeadID != "bead-abc" {
		t.Errorf("expected bead ID 'bead-abc', got %q", ps.BeadID)
	}
	if ps.Status != PhaseStatusCreated {
		t.Errorf("expected status 'created', got %q", ps.Status)
	}
}

func TestState_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()

	state, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState on empty dir failed: %v", err)
	}
	if state.Version != 1 {
		t.Errorf("expected version 1, got %d", state.Version)
	}
	if len(state.Phases) != 0 {
		t.Errorf("expected empty phases, got %d", len(state.Phases))
	}
}

func TestState_LoadLegacyTasks(t *testing.T) {
	dir := t.TempDir()

	// Write a state file using the deprecated [tasks] section.
	legacyTOML := `version = 1
nebula_name = "legacy-test"

[tasks.phase-1]
bead_id = "bead-legacy"
status = "created"
created_at = 2025-01-01T00:00:00Z
updated_at = 2025-01-01T00:00:00Z

[tasks.phase-2]
bead_id = "bead-legacy-2"
status = "done"
created_at = 2025-01-01T00:00:00Z
updated_at = 2025-01-01T00:00:00Z
`
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(legacyTOML), 0644); err != nil {
		t.Fatalf("failed to write legacy state file: %v", err)
	}

	state, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState with legacy [tasks] failed: %v", err)
	}

	if state.NebulaName != "legacy-test" {
		t.Errorf("expected nebula name 'legacy-test', got %q", state.NebulaName)
	}
	if len(state.Phases) != 2 {
		t.Fatalf("expected 2 phases from legacy [tasks], got %d", len(state.Phases))
	}

	ps1, ok := state.Phases["phase-1"]
	if !ok {
		t.Fatal("phase-1 not found in loaded legacy state")
	}
	if ps1.BeadID != "bead-legacy" {
		t.Errorf("expected bead ID 'bead-legacy', got %q", ps1.BeadID)
	}
	if ps1.Status != PhaseStatusCreated {
		t.Errorf("expected status 'created', got %q", ps1.Status)
	}

	ps2, ok := state.Phases["phase-2"]
	if !ok {
		t.Fatal("phase-2 not found in loaded legacy state")
	}
	if ps2.BeadID != "bead-legacy-2" {
		t.Errorf("expected bead ID 'bead-legacy-2', got %q", ps2.BeadID)
	}
	if ps2.Status != PhaseStatusDone {
		t.Errorf("expected status 'done', got %q", ps2.Status)
	}
}

// --- Mock beads client for plan/apply tests ---

type mockBeadsClient struct {
	created   map[string]string // title → id
	shown     map[string]*beads.Bead
	closed    map[string]string
	nextID    int
	createErr error
}

func newMockBeadsClient() *mockBeadsClient {
	return &mockBeadsClient{
		created: make(map[string]string),
		shown:   make(map[string]*beads.Bead),
		closed:  make(map[string]string),
	}
}

func (m *mockBeadsClient) Create(_ context.Context, title string, opts beads.CreateOpts) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	m.nextID++
	id := "bead-" + title
	m.created[title] = id
	m.shown[id] = &beads.Bead{ID: id, Title: title}
	return id, nil
}

func (m *mockBeadsClient) Show(_ context.Context, id string) (*beads.Bead, error) {
	b, ok := m.shown[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return b, nil
}

func (m *mockBeadsClient) Update(_ context.Context, id string, opts beads.UpdateOpts) error {
	return nil
}

func (m *mockBeadsClient) Close(_ context.Context, id string, reason string) error {
	m.closed[id] = reason
	return nil
}

func (m *mockBeadsClient) AddComment(_ context.Context, id string, body string) error {
	return nil
}

func (m *mockBeadsClient) Validate() error {
	return nil
}

// --- Plan tests ---

func TestBuildPlan_NewNebula(t *testing.T) {
	n, err := Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
	client := newMockBeadsClient()

	plan, err := BuildPlan(context.Background(), n, state, client)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if plan.NebulaName != "test-nebula" {
		t.Errorf("expected plan name 'test-nebula', got %q", plan.NebulaName)
	}

	// All 3 phases should be creates.
	creates := 0
	for _, a := range plan.Actions {
		if a.Type == ActionCreate {
			creates++
		}
	}
	if creates != 3 {
		t.Errorf("expected 3 create actions, got %d", creates)
	}
}

func TestBuildPlan_LockedPhase(t *testing.T) {
	n := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases:   []PhaseSpec{{ID: "locked", Title: "A locked phase"}},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"locked": {BeadID: "bead-123", Status: PhaseStatusInProgress},
		},
	}
	client := newMockBeadsClient()
	client.shown["bead-123"] = &beads.Bead{ID: "bead-123"}

	plan, err := BuildPlan(context.Background(), n, state, client)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Type != ActionSkip {
		t.Errorf("expected skip action for locked phase, got %s", plan.Actions[0].Type)
	}
}

func TestBuildPlan_FailedPhase(t *testing.T) {
	n := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases:   []PhaseSpec{{ID: "fail-phase", Title: "A failed phase"}},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"fail-phase": {BeadID: "bead-old", Status: PhaseStatusFailed},
		},
	}
	client := newMockBeadsClient()

	plan, err := BuildPlan(context.Background(), n, state, client)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Type != ActionRetry {
		t.Errorf("expected retry action for failed phase, got %s", plan.Actions[0].Type)
	}
	if plan.Actions[0].PhaseID != "fail-phase" {
		t.Errorf("expected phase ID 'fail-phase', got %q", plan.Actions[0].PhaseID)
	}
}

// --- Apply tests ---

func TestApply_CreatesBeads(t *testing.T) {
	n, err := Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Use a temp dir so we don't write state into testdata.
	tmpDir := t.TempDir()
	n.Dir = tmpDir

	state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
	client := newMockBeadsClient()

	plan := &Plan{
		NebulaName: "test-nebula",
		Actions: []Action{
			{PhaseID: "first-task", Type: ActionCreate, Reason: "new"},
			{PhaseID: "second-task", Type: ActionCreate, Reason: "new"},
			{PhaseID: "independent", Type: ActionCreate, Reason: "new"},
		},
	}

	if err := Apply(context.Background(), plan, n, state, client); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if len(client.created) != 3 {
		t.Errorf("expected 3 beads created, got %d", len(client.created))
	}

	// State should have all 3 phases.
	if len(state.Phases) != 3 {
		t.Errorf("expected 3 phases in state, got %d", len(state.Phases))
	}

	for _, id := range []string{"first-task", "second-task", "independent"} {
		ps, ok := state.Phases[id]
		if !ok {
			t.Errorf("phase %q not in state", id)
			continue
		}
		if ps.Status != PhaseStatusCreated {
			t.Errorf("phase %q status: %s, expected created", id, ps.Status)
		}
		if ps.BeadID == "" {
			t.Errorf("phase %q has empty bead ID", id)
		}
	}
}

func TestApply_RetriesFailedPhase(t *testing.T) {
	n, err := Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Use a temp dir so we don't write state into testdata.
	tmpDir := t.TempDir()
	n.Dir = tmpDir

	oldBeadID := "bead-old-failed"
	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"first-task": {BeadID: oldBeadID, Status: PhaseStatusFailed},
		},
	}
	client := newMockBeadsClient()

	plan := &Plan{
		NebulaName: "test-nebula",
		Actions: []Action{
			{PhaseID: "first-task", Type: ActionRetry, Reason: "retrying failed phase"},
		},
	}

	if err := Apply(context.Background(), plan, n, state, client); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// A new bead should have been created.
	if len(client.created) != 1 {
		t.Errorf("expected 1 bead created for retry, got %d", len(client.created))
	}

	ps, ok := state.Phases["first-task"]
	if !ok {
		t.Fatal("phase 'first-task' not in state after retry")
	}

	// The bead ID should be different from the old failed bead.
	if ps.BeadID == oldBeadID {
		t.Errorf("expected new bead ID after retry, but still has old ID %q", oldBeadID)
	}
	if ps.BeadID == "" {
		t.Error("expected non-empty bead ID after retry")
	}

	// Status should be reset to created (not failed).
	if ps.Status != PhaseStatusCreated {
		t.Errorf("expected status %q after retry, got %q", PhaseStatusCreated, ps.Status)
	}
}

// --- Worker tests ---

type mockRunner struct {
	calls      []string
	err        error
	result     *PhaseRunnerResult
	resultFunc func(beadID string) *PhaseRunnerResult // optional per-call result
}

func (m *mockRunner) RunExistingPhase(ctx context.Context, beadID, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error) {
	m.calls = append(m.calls, beadID)
	if m.resultFunc != nil {
		return m.resultFunc(beadID), m.err
	}
	return m.result, m.err
}

func (m *mockRunner) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	return "checkpoint summary", nil
}

func TestWorkerGroup_ExecutesDependencyOrder(t *testing.T) {
	n := &Nebula{
		Dir:      t.TempDir(),
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
			{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
		},
	}

	runner := &mockRunner{}
	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
	}

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// With max 1 worker and b depending on a, a must execute first.
	if runner.calls[0] != "bead-a" {
		t.Errorf("expected bead-a to run first, got %s", runner.calls[0])
	}
	if runner.calls[1] != "bead-b" {
		t.Errorf("expected bead-b to run second, got %s", runner.calls[1])
	}

	// State should reflect both done.
	if state.Phases["a"].Status != PhaseStatusDone {
		t.Errorf("phase a status: %s, expected done", state.Phases["a"].Status)
	}
	if state.Phases["b"].Status != PhaseStatusDone {
		t.Errorf("phase b status: %s, expected done", state.Phases["b"].Status)
	}
}

func TestWorkerGroup_FailureBlocksDependents(t *testing.T) {
	n := &Nebula{
		Dir:      t.TempDir(),
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
			{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
		},
	}

	runner := &mockRunner{err: errors.New("simulated failure")}
	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
	}

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	// Only a should have been attempted (b blocked by a's failure).
	if len(results) != 1 {
		t.Fatalf("expected 1 result (only a attempted), got %d", len(results))
	}
	if results[0].PhaseID != "a" {
		t.Errorf("expected result for phase a, got %s", results[0].PhaseID)
	}
	if results[0].Err == nil {
		t.Error("expected error in result for phase a")
	}

	if state.Phases["a"].Status != PhaseStatusFailed {
		t.Errorf("phase a status: %s, expected failed", state.Phases["a"].Status)
	}
	// b should remain created (never touched).
	if state.Phases["b"].Status != PhaseStatusCreated {
		t.Errorf("phase b status: %s, expected created (untouched)", state.Phases["b"].Status)
	}
}

func TestWorkerGroup_AccumulatesCostAcrossPhases(t *testing.T) {
	n := &Nebula{
		Dir:      t.TempDir(),
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
			{ID: "b", Body: "phase b"},
			{ID: "c", Body: "phase c", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
			"c": {BeadID: "bead-c", Status: PhaseStatusCreated},
		},
	}

	costs := map[string]float64{
		"bead-a": 0.50,
		"bead-b": 1.25,
		"bead-c": 0.75,
	}

	runner := &mockRunner{
		resultFunc: func(beadID string) *PhaseRunnerResult {
			return &PhaseRunnerResult{TotalCostUSD: costs[beadID]}
		},
	}

	var progressCosts []float64
	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
		OnProgress: func(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
			progressCosts = append(progressCosts, totalCostUSD)
		},
	}

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Total cost should be sum of all phases.
	expectedTotal := 0.50 + 1.25 + 0.75
	if state.TotalCostUSD != expectedTotal {
		t.Errorf("expected total cost $%.2f, got $%.2f", expectedTotal, state.TotalCostUSD)
	}

	// Progress callback should have been called with increasing costs.
	// Each phase triggers two progress calls (in_progress + done), so we check the final one.
	if len(progressCosts) == 0 {
		t.Fatal("expected progress callbacks, got none")
	}
	lastCost := progressCosts[len(progressCosts)-1]
	if lastCost != expectedTotal {
		t.Errorf("expected final progress cost $%.2f, got $%.2f", expectedTotal, lastCost)
	}
}

// --- Intervention tests ---

// newTestWatcher creates a Watcher with pre-built channels for unit testing
// (no fsnotify needed).
func newTestWatcher(dir string) *Watcher {
	ch := make(chan Change, 16)
	iv := make(chan InterventionKind, 4)
	return &Watcher{
		Dir:           dir,
		Changes:       ch,
		Interventions: iv,
		changes:       ch,
		interventions: iv,
		done:          make(chan struct{}),
	}
}

func TestWorkerGroup_StopIntervention(t *testing.T) {
	dir := t.TempDir()

	n := &Nebula{
		Dir:      dir,
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
			{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
		},
	}

	w := newTestWatcher(dir)
	runner := &mockRunner{}

	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
		Watcher:    w,
	}

	// Create a STOP file so it can be cleaned up.
	stopFile := filepath.Join(dir, "STOP")
	if err := os.WriteFile(stopFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create STOP file: %v", err)
	}

	// Pre-load a stop intervention before Run starts.
	w.interventions <- InterventionStop

	results, err := wg.Run(context.Background())
	if !errors.Is(err, ErrManualStop) {
		t.Fatalf("expected ErrManualStop, got %v", err)
	}

	// No phases should have been executed (stop came before first batch).
	if len(results) != 0 {
		t.Errorf("expected 0 results (stopped before execution), got %d", len(results))
	}

	// STOP file should be cleaned up.
	if _, err := os.Stat(stopFile); !os.IsNotExist(err) {
		t.Error("expected STOP file to be removed after stop")
	}
}

func TestWorkerGroup_PauseIntervention(t *testing.T) {
	dir := t.TempDir()

	n := &Nebula{
		Dir:      dir,
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
			{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
		},
	}

	w := newTestWatcher(dir)
	runner := &mockRunner{}

	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
		Watcher:    w,
	}

	// Send pause, but the PAUSE file doesn't exist on disk so handlePause
	// returns immediately (the stat check finds no file).
	w.interventions <- InterventionPause

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	// After resume, all phases should complete.
	if len(results) != 2 {
		t.Errorf("expected 2 results after resume, got %d", len(results))
	}
}

func TestWorkerGroup_PauseBlocksUntilResume(t *testing.T) {
	dir := t.TempDir()

	n := &Nebula{
		Dir:      dir,
		Manifest: Manifest{Nebula: Info{Name: "test"}},
		Phases: []PhaseSpec{
			{ID: "a", Body: "phase a"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	w := newTestWatcher(dir)
	runner := &mockRunner{}

	wg := &WorkerGroup{
		Runner:     runner,
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
		Watcher:    w,
	}

	// Create the PAUSE file so handlePause actually blocks.
	pauseFile := filepath.Join(dir, "PAUSE")
	if err := os.WriteFile(pauseFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create PAUSE file: %v", err)
	}

	w.interventions <- InterventionPause

	done := make(chan struct{})
	go func() {
		_, _ = wg.Run(context.Background())
		close(done)
	}()

	// Give the worker time to start and block on pause.
	time.Sleep(100 * time.Millisecond)

	// Send resume to unblock.
	w.interventions <- InterventionResume

	select {
	case <-done:
		// Success: Run completed after resume.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Run to complete after resume")
	}
}

func TestIsInterventionFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"PAUSE", true},
		{"STOP", true},
		{"pause", false},
		{"stop", false},
		{"README.md", false},
		{"PAUSING", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsInterventionFile(tt.name)
			if got != tt.want {
				t.Errorf("IsInterventionFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestInterventionFileNames(t *testing.T) {
	names := InterventionFileNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 intervention file names, got %d", len(names))
	}

	sort.Strings(names)
	if names[0] != "PAUSE" || names[1] != "STOP" {
		t.Errorf("expected [PAUSE, STOP], got %v", names)
	}
}

func TestGitExcludePatterns(t *testing.T) {
	patterns := GitExcludePatterns()
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}

	joined := strings.Join(patterns, ",")
	if !strings.Contains(joined, "PAUSE") {
		t.Error("expected PAUSE in exclude patterns")
	}
	if !strings.Contains(joined, "STOP") {
		t.Error("expected STOP in exclude patterns")
	}
}
