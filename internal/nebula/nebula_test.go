package nebula

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
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
	mu         sync.Mutex
	calls      []string
	err        error
	result     *PhaseRunnerResult
	resultFunc func(beadID string) *PhaseRunnerResult // optional per-call result
}

func (m *mockRunner) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, beadID)
	m.mu.Unlock()
	if m.resultFunc != nil {
		return m.resultFunc(beadID), m.err
	}
	return m.result, m.err
}

// getCalls returns a snapshot of the calls slice for safe reading in assertions.
func (m *mockRunner) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.calls))
	copy(cp, m.calls)
	return cp
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
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
	)

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// With max 1 worker and b depending on a, a must execute first.
	calls := runner.getCalls()
	if calls[0] != "bead-a" {
		t.Errorf("expected bead-a to run first, got %s", calls[0])
	}
	if calls[1] != "bead-b" {
		t.Errorf("expected bead-b to run second, got %s", calls[1])
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
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
	)

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
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithOnProgress(func(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
			progressCosts = append(progressCosts, totalCostUSD)
		}),
	)

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

	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithWatcher(w),
	)

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

	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithWatcher(w),
	)

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

	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithWatcher(w),
	)

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
		{"RETRY", true},
		{"pause", false},
		{"stop", false},
		{"retry", false},
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
	if len(names) != 3 {
		t.Fatalf("expected 3 intervention file names, got %d", len(names))
	}

	sort.Strings(names)
	if names[0] != "PAUSE" || names[1] != "RETRY" || names[2] != "STOP" {
		t.Errorf("expected [PAUSE, RETRY, STOP], got %v", names)
	}
}

func TestGitExcludePatterns(t *testing.T) {
	patterns := GitExcludePatterns()
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}

	joined := strings.Join(patterns, ",")
	if !strings.Contains(joined, "PAUSE") {
		t.Error("expected PAUSE in exclude patterns")
	}
	if !strings.Contains(joined, "STOP") {
		t.Error("expected STOP in exclude patterns")
	}
	if !strings.Contains(joined, "RETRY") {
		t.Error("expected RETRY in exclude patterns")
	}
}

// --- Gate mode tests ---

func TestResolveGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest Execution
		phase    PhaseSpec
		want     GateMode
	}{
		{
			name:     "both empty defaults to trust",
			manifest: Execution{},
			phase:    PhaseSpec{},
			want:     GateModeTrust,
		},
		{
			name:     "manifest gate used when phase empty",
			manifest: Execution{Gate: GateModeReview},
			phase:    PhaseSpec{},
			want:     GateModeReview,
		},
		{
			name:     "phase gate overrides manifest",
			manifest: Execution{Gate: GateModeReview},
			phase:    PhaseSpec{Gate: GateModeApprove},
			want:     GateModeApprove,
		},
		{
			name:     "phase gate used when manifest empty",
			manifest: Execution{},
			phase:    PhaseSpec{Gate: GateModeWatch},
			want:     GateModeWatch,
		},
		{
			name:     "trust can be explicitly set on phase",
			manifest: Execution{Gate: GateModeApprove},
			phase:    PhaseSpec{Gate: GateModeTrust},
			want:     GateModeTrust,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveGate(tt.manifest, tt.phase)
			if got != tt.want {
				t.Errorf("ResolveGate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate_InvalidManifestGate(t *testing.T) {
	t.Parallel()

	n := &Nebula{
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: "yolo"},
		},
		Phases: []PhaseSpec{
			{ID: "a", Title: "Phase A", Body: "do stuff", SourceFile: "a.md"},
		},
	}

	errs := Validate(n)
	found := false
	for _, e := range errs {
		if e.Field == "execution.gate" && strings.Contains(e.Err.Error(), "yolo") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected validation error for invalid manifest gate, got %v", errs)
	}
}

func TestValidate_InvalidPhaseGate(t *testing.T) {
	t.Parallel()

	n := &Nebula{
		Manifest: Manifest{
			Nebula: Info{Name: "test"},
		},
		Phases: []PhaseSpec{
			{ID: "a", Title: "Phase A", Gate: "nope", Body: "do stuff", SourceFile: "a.md"},
		},
	}

	errs := Validate(n)
	found := false
	for _, e := range errs {
		if e.Field == "gate" && e.PhaseID == "a" && strings.Contains(e.Err.Error(), "nope") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected validation error for invalid phase gate, got %v", errs)
	}
}

func TestValidate_ValidGateModes(t *testing.T) {
	t.Parallel()

	modes := []GateMode{GateModeTrust, GateModeReview, GateModeApprove, GateModeWatch, ""}
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			n := &Nebula{
				Manifest: Manifest{
					Nebula:    Info{Name: "test"},
					Execution: Execution{Gate: mode},
				},
				Phases: []PhaseSpec{
					{ID: "a", Title: "Phase A", Gate: mode, Body: "do stuff", SourceFile: "a.md"},
				},
			}

			errs := Validate(n)
			for _, e := range errs {
				if e.Field == "execution.gate" || e.Field == "gate" {
					t.Errorf("unexpected gate validation error for mode %q: %v", mode, e.Err)
				}
			}
		})
	}
}

// --- ComputeWaves tests ---

func TestGraph_ComputeWaves_Linear(t *testing.T) {
	t.Parallel()
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	g := NewGraph(phases)
	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatalf("ComputeWaves failed: %v", err)
	}
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	if waves[0].PhaseIDs[0] != "a" {
		t.Errorf("wave 1: expected [a], got %v", waves[0].PhaseIDs)
	}
	if waves[1].PhaseIDs[0] != "b" {
		t.Errorf("wave 2: expected [b], got %v", waves[1].PhaseIDs)
	}
	if waves[2].PhaseIDs[0] != "c" {
		t.Errorf("wave 3: expected [c], got %v", waves[2].PhaseIDs)
	}
}

func TestGraph_ComputeWaves_Parallel(t *testing.T) {
	t.Parallel()
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d", DependsOn: []string{"a", "b", "c"}},
	}
	g := NewGraph(phases)
	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatalf("ComputeWaves failed: %v", err)
	}
	if len(waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(waves))
	}
	if len(waves[0].PhaseIDs) != 3 {
		t.Errorf("wave 1: expected 3 phases, got %d", len(waves[0].PhaseIDs))
	}
	// PhaseIDs should be sorted within each wave.
	if waves[0].PhaseIDs[0] != "a" || waves[0].PhaseIDs[1] != "b" || waves[0].PhaseIDs[2] != "c" {
		t.Errorf("wave 1: expected [a, b, c], got %v", waves[0].PhaseIDs)
	}
	if waves[1].PhaseIDs[0] != "d" {
		t.Errorf("wave 2: expected [d], got %v", waves[1].PhaseIDs)
	}
}

func TestGraph_ComputeWaves_Cycle(t *testing.T) {
	t.Parallel()
	phases := []PhaseSpec{
		{ID: "x", DependsOn: []string{"y"}},
		{ID: "y", DependsOn: []string{"x"}},
	}
	g := NewGraph(phases)
	_, err := g.ComputeWaves()
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("expected ErrDependencyCycle, got %v", err)
	}
}

func TestGraph_ComputeWaves_WaveNumbers(t *testing.T) {
	t.Parallel()
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
	}
	g := NewGraph(phases)
	waves, err := g.ComputeWaves()
	if err != nil {
		t.Fatalf("ComputeWaves failed: %v", err)
	}
	for i, w := range waves {
		if w.Number != i+1 {
			t.Errorf("wave %d: expected Number=%d, got %d", i, i+1, w.Number)
		}
	}
}

// --- RenderPlan tests ---

func TestRenderPlan_Output(t *testing.T) {
	t.Parallel()
	waves := []Wave{
		{Number: 1, PhaseIDs: []string{"test", "vet", "lint"}},
		{Number: 2, PhaseIDs: []string{"build"}},
		{Number: 3, PhaseIDs: []string{"deploy"}},
	}

	var buf strings.Builder
	RenderPlan(&buf, "CI Pipeline", waves, 5, 50.0, GateModeApprove)

	output := buf.String()
	if !strings.Contains(output, "CI Pipeline") {
		t.Error("expected nebula name in output")
	}
	if !strings.Contains(output, "approve mode") {
		t.Error("expected gate mode in output")
	}
	if !strings.Contains(output, "Wave 1 (parallel)") {
		t.Error("expected parallel label for wave with multiple phases")
	}
	if !strings.Contains(output, "test, vet, lint") {
		t.Error("expected phase IDs in wave 1")
	}
	if !strings.Contains(output, "Wave 2:") {
		t.Error("expected Wave 2 label without parallel")
	}
	if !strings.Contains(output, "Phases: 5") {
		t.Error("expected phase count in output")
	}
	if !strings.Contains(output, "Budget: $50.00") {
		t.Error("expected budget in output")
	}
	// RenderPlan should NOT include prompt options; those come from Gater.Prompt.
	if strings.Contains(output, "[a]pprove") {
		t.Error("RenderPlan should not include prompt options")
	}
}

func TestRenderPlan_NoBudget(t *testing.T) {
	t.Parallel()
	waves := []Wave{
		{Number: 1, PhaseIDs: []string{"a"}},
	}

	var buf strings.Builder
	RenderPlan(&buf, "test", waves, 1, 0, GateModeApprove)

	output := buf.String()
	if strings.Contains(output, "Budget") {
		t.Error("expected no budget line when budget is 0")
	}
}

// --- Plan gate tests ---

// mockGater is a GatePrompter that returns a predetermined action.
// It counts how many times Prompt is called.
type mockGater struct {
	action GateAction
	calls  int
}

func (g *mockGater) Prompt(_ context.Context, _ *Checkpoint) (GateAction, error) {
	g.calls++
	return g.action, nil
}

func TestWorkerGroup_ApproveMode_PlanAccepted(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeApprove},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	gater := &mockGater{action: GateActionAccept}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Gater should have been called at least once (for the plan gate).
	if gater.calls < 1 {
		t.Error("expected gater to be called for plan gate")
	}

	// Phase should have been executed.
	if len(runner.getCalls()) != 1 {
		t.Errorf("expected 1 phase execution, got %d", len(runner.getCalls()))
	}
}

func TestWorkerGroup_ApproveMode_PlanRejected(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeApprove},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	gater := &mockGater{action: GateActionSkip}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if !errors.Is(err, ErrPlanRejected) {
		t.Fatalf("expected ErrPlanRejected, got %v", err)
	}

	// No phases should have been executed.
	if len(runner.getCalls()) != 0 {
		t.Errorf("expected 0 phase executions after plan rejection, got %d", len(runner.getCalls()))
	}
}

func TestWorkerGroup_ReviewMode_NoPlanGate(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeReview},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	// Use a gater that accepts — but we want to verify the plan gate is NOT shown.
	gater := &mockGater{action: GateActionAccept}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Phase should have been executed.
	if len(runner.getCalls()) != 1 {
		t.Errorf("expected 1 phase execution, got %d", len(runner.getCalls()))
	}

	// Gater should have been called once for the phase gate (review mode),
	// but NOT for a plan gate.
	if gater.calls != 1 {
		t.Errorf("expected 1 gater call (phase only, no plan), got %d", gater.calls)
	}
}

func TestWorkerGroup_TrustMode_NoPlanGate(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeTrust},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	gater := &mockGater{action: GateActionAccept}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Trust mode: no gater calls at all (neither plan nor phase).
	if gater.calls != 0 {
		t.Errorf("expected 0 gater calls in trust mode, got %d", gater.calls)
	}
}

func TestWorkerGroup_WatchMode_NoPlanGate(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeWatch},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	gater := &mockGater{action: GateActionAccept}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Watch mode: no plan gate, but phase gate renders checkpoint without blocking.
	// Since watch mode doesn't call Prompt, gater.calls should be 0.
	if gater.calls != 0 {
		t.Errorf("expected 0 gater calls in watch mode, got %d", gater.calls)
	}
}

func TestWorkerGroup_WatchMode_RendersCheckpointWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "watch-render-test"},
			Execution: Execution{Gate: GateModeWatch},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff", Title: "Phase A"},
			{ID: "b", Body: "do more", Title: "Phase B"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
			"b": {BeadID: "bead-b", Status: PhaseStatusCreated},
		},
	}

	runner := &mockRunner{
		result: &PhaseRunnerResult{
			TotalCostUSD: 0.10,
			CyclesUsed:   1,
			Report:       &agent.ReviewReport{Summary: "looks good"},
		},
	}
	committer := &mockGitCommitter{
		diffLastCommit:     "diff --git a/main.go b/main.go\n",
		diffStatLastCommit: " main.go | 5 +++++\n 1 file changed, 5 insertions(+)\n",
	}
	gater := &mockGater{action: GateActionAccept}

	var dashBuf bytes.Buffer
	dashboard := NewDashboard(&dashBuf, n, state, 10.0, true)
	dashboard.AppendOnly = true

	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(2),
		WithPrompter(gater),
		WithDashboard(dashboard),
		WithCommitter(committer),
		WithOnProgress(dashboard.ProgressCallback()),
	)

	results, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Both phases should complete.
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Watch mode should not have called Prompt.
	if gater.calls != 0 {
		t.Errorf("expected 0 gater calls in watch mode, got %d", gater.calls)
	}

	// Both phases should be done.
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("phase %q failed: %v", r.PhaseID, r.Err)
		}
	}
}

func TestWorkerGroup_WatchMode_DashboardPausedDuringCheckpoint(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "dashboard-pause-test"},
			Execution: Execution{Gate: GateModeWatch},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	runner := &mockRunner{
		result: &PhaseRunnerResult{
			TotalCostUSD: 0.05,
			CyclesUsed:   1,
		},
	}
	committer := &mockGitCommitter{
		diffLastCommit:     "diff --git a/test.go b/test.go\n",
		diffStatLastCommit: " test.go | 3 +++\n 1 file changed, 3 insertions(+)\n",
	}

	var dashBuf bytes.Buffer
	dashboard := NewDashboard(&dashBuf, n, state, 5.0, true)
	dashboard.AppendOnly = true

	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(&mockGater{action: GateActionAccept}),
		WithDashboard(dashboard),
		WithCommitter(committer),
		WithOnProgress(dashboard.ProgressCallback()),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Dashboard should have produced output (append-only plain lines).
	if dashBuf.Len() == 0 {
		t.Error("expected dashboard output in watch mode")
	}
}

func TestWorkerGroup_ApproveMode_PlanRejectedWithReject(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeApprove},
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	gater := &mockGater{action: GateActionReject}
	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithPrompter(gater),
	)

	_, err := wg.Run(context.Background())
	if !errors.Is(err, ErrPlanRejected) {
		t.Fatalf("expected ErrPlanRejected, got %v", err)
	}

	// No phases should have been executed.
	if len(runner.getCalls()) != 0 {
		t.Errorf("expected 0 phase executions after plan rejection, got %d", len(runner.getCalls()))
	}
}

// --- Metrics instrumentation tests ---

func TestWorkerGroup_NilMetrics_NoPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phases     []PhaseSpec
		maxWorkers int
	}{
		{
			name: "single phase",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
			},
			maxWorkers: 1,
		},
		{
			name: "multiple phases with dependency",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
				{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
			},
			maxWorkers: 2,
		},
		{
			name: "parallel independent phases",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
				{ID: "b", Body: "phase b"},
				{ID: "c", Body: "phase c"},
			},
			maxWorkers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			n := &Nebula{
				Dir:      dir,
				Manifest: Manifest{Nebula: Info{Name: "nil-metrics-test"}},
				Phases:   tt.phases,
			}

			phases := make(map[string]*PhaseState)
			for _, p := range tt.phases {
				phases[p.ID] = &PhaseState{BeadID: "bead-" + p.ID, Status: PhaseStatusCreated}
			}
			state := &State{Version: 1, Phases: phases}

			runner := &mockRunner{}
			wg := NewWorkerGroup(n, state,
				WithRunner(runner),
				WithMaxWorkers(tt.maxWorkers),
				// Metrics intentionally omitted (nil).
			)

			results, err := wg.Run(context.Background())
			if err != nil {
				t.Fatalf("WorkerGroup.Run with nil Metrics failed: %v", err)
			}

			if len(results) != len(tt.phases) {
				t.Errorf("expected %d results, got %d", len(tt.phases), len(results))
			}

			// All phases should complete successfully.
			for _, r := range results {
				if r.Err != nil {
					t.Errorf("phase %q failed: %v", r.PhaseID, r.Err)
				}
			}
		})
	}
}

func TestWorkerGroup_WithMetrics_PhaseMetricsPopulated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phases     []PhaseSpec
		costs      map[string]float64
		cycles     map[string]int
		wantPhases int
		wantCost   float64
	}{
		{
			name: "single phase records metrics",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
			},
			costs:      map[string]float64{"bead-a": 0.50},
			cycles:     map[string]int{"bead-a": 3},
			wantPhases: 1,
			wantCost:   0.50,
		},
		{
			name: "multiple phases accumulate cost",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
				{ID: "b", Body: "phase b"},
			},
			costs:      map[string]float64{"bead-a": 0.25, "bead-b": 0.75},
			cycles:     map[string]int{"bead-a": 2, "bead-b": 4},
			wantPhases: 2,
			wantCost:   1.00,
		},
		{
			name: "dependent phases with metrics",
			phases: []PhaseSpec{
				{ID: "a", Body: "phase a"},
				{ID: "b", Body: "phase b", DependsOn: []string{"a"}},
			},
			costs:      map[string]float64{"bead-a": 0.25, "bead-b": 0.50},
			cycles:     map[string]int{"bead-a": 1, "bead-b": 2},
			wantPhases: 2,
			wantCost:   0.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			n := &Nebula{
				Dir:      dir,
				Manifest: Manifest{Nebula: Info{Name: "metrics-test"}},
				Phases:   tt.phases,
			}

			phases := make(map[string]*PhaseState)
			for _, p := range tt.phases {
				phases[p.ID] = &PhaseState{BeadID: "bead-" + p.ID, Status: PhaseStatusCreated}
			}
			state := &State{Version: 1, Phases: phases}

			runner := &mockRunner{
				resultFunc: func(beadID string) *PhaseRunnerResult {
					return &PhaseRunnerResult{
						TotalCostUSD: tt.costs[beadID],
						CyclesUsed:   tt.cycles[beadID],
					}
				},
			}

			metrics := NewMetrics("metrics-test")
			wg := NewWorkerGroup(n, state,
				WithRunner(runner),
				WithMaxWorkers(1),
				WithMetrics(metrics),
			)

			results, err := wg.Run(context.Background())
			if err != nil {
				t.Fatalf("WorkerGroup.Run failed: %v", err)
			}

			if len(results) != tt.wantPhases {
				t.Fatalf("expected %d results, got %d", tt.wantPhases, len(results))
			}

			snap := metrics.Snapshot()

			// Verify total phase count.
			if snap.TotalPhases != tt.wantPhases {
				t.Errorf("TotalPhases = %d, want %d", snap.TotalPhases, tt.wantPhases)
			}

			// Verify total cost.
			if snap.TotalCostUSD != tt.wantCost {
				t.Errorf("TotalCostUSD = %f, want %f", snap.TotalCostUSD, tt.wantCost)
			}

			// Verify per-phase metrics.
			if len(snap.Phases) != tt.wantPhases {
				t.Fatalf("len(Phases) = %d, want %d", len(snap.Phases), tt.wantPhases)
			}

			for _, pm := range snap.Phases {
				if pm.PhaseID == "" {
					t.Error("PhaseMetrics.PhaseID should not be empty")
				}
				if pm.Duration <= 0 {
					t.Errorf("PhaseMetrics[%s].Duration = %v, want > 0", pm.PhaseID, pm.Duration)
				}
				if pm.CompletedAt.IsZero() {
					t.Errorf("PhaseMetrics[%s].CompletedAt should be set", pm.PhaseID)
				}
				if pm.StartedAt.IsZero() {
					t.Errorf("PhaseMetrics[%s].StartedAt should be set", pm.PhaseID)
				}

				expectedCost := tt.costs["bead-"+pm.PhaseID]
				if pm.CostUSD != expectedCost {
					t.Errorf("PhaseMetrics[%s].CostUSD = %f, want %f", pm.PhaseID, pm.CostUSD, expectedCost)
				}

				expectedCycles := tt.cycles["bead-"+pm.PhaseID]
				if pm.CyclesUsed != expectedCycles {
					t.Errorf("PhaseMetrics[%s].CyclesUsed = %d, want %d", pm.PhaseID, pm.CyclesUsed, expectedCycles)
				}
			}

			// Verify at least one wave was recorded.
			if snap.TotalWaves == 0 {
				t.Error("TotalWaves should be > 0 after Run completes")
			}
			if len(snap.Waves) == 0 {
				t.Error("Waves should not be empty after Run completes")
			}
		})
	}
}

func TestWorkerGroup_WithMetrics_FailedPhaseRecorded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	n := &Nebula{
		Dir:      dir,
		Manifest: Manifest{Nebula: Info{Name: "fail-metrics-test"}},
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
	metrics := NewMetrics("fail-metrics-test")
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		WithMetrics(metrics),
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("WorkerGroup.Run failed: %v", err)
	}

	snap := metrics.Snapshot()

	// Phase a was started (should have a start record).
	if snap.TotalPhases == 0 {
		t.Error("TotalPhases should be > 0 even with failures")
	}

	// Phase a was started and should appear in Phases.
	foundA := false
	for _, pm := range snap.Phases {
		if pm.PhaseID == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Error("expected phase 'a' to appear in metrics even after failure")
	}
}

func TestWorkerGroup_NilGater_NoPlanGate(t *testing.T) {
	dir := t.TempDir()
	n := &Nebula{
		Dir: dir,
		Manifest: Manifest{
			Nebula:    Info{Name: "test"},
			Execution: Execution{Gate: GateModeApprove}, // approve mode but no gater
		},
		Phases: []PhaseSpec{
			{ID: "a", Body: "do stuff"},
		},
	}

	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {BeadID: "bead-a", Status: PhaseStatusCreated},
		},
	}

	runner := &mockRunner{}
	wg := NewWorkerGroup(n, state,
		WithRunner(runner),
		WithMaxWorkers(1),
		// Gater intentionally omitted — should fall back to trust mode.
	)

	_, err := wg.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error with nil gater, got %v", err)
	}

	// Phase should have been executed even with approve mode set,
	// because nil Gater falls back to trust.
	if len(runner.getCalls()) != 1 {
		t.Errorf("expected 1 phase execution with nil gater, got %d", len(runner.getCalls()))
	}
}
