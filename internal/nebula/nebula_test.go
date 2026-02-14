package nebula

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

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

	if len(n.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(n.Tasks))
	}

	// Tasks should be in directory order.
	taskByID := make(map[string]TaskSpec)
	for _, task := range n.Tasks {
		taskByID[task.ID] = task
	}

	// Check first task inherits defaults.
	first := taskByID["first-task"]
	if first.Title != "First test task" {
		t.Errorf("first task title: %q", first.Title)
	}
	if first.Type != "task" {
		t.Errorf("first task type should inherit default 'task', got %q", first.Type)
	}
	if first.Priority != 2 {
		t.Errorf("first task priority should inherit default 2, got %d", first.Priority)
	}
	if len(first.Labels) != 1 || first.Labels[0] != "test" {
		t.Errorf("first task labels should inherit default, got %v", first.Labels)
	}
	if first.Body == "" {
		t.Error("first task body should not be empty")
	}

	// Check second task overrides defaults.
	second := taskByID["second-task"]
	if second.Type != "feature" {
		t.Errorf("second task type: %q", second.Type)
	}
	if second.Priority != 1 {
		t.Errorf("second task priority: %d", second.Priority)
	}
	if len(second.DependsOn) != 1 || second.DependsOn[0] != "first-task" {
		t.Errorf("second task depends_on: %v", second.DependsOn)
	}
	if len(second.Labels) != 1 || second.Labels[0] != "custom-label" {
		t.Errorf("second task labels: %v", second.Labels)
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
	tasks := []TaskSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}

	g := NewGraph(tasks)
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
	tasks := []TaskSpec{
		{ID: "x", DependsOn: []string{"y"}},
		{ID: "y", DependsOn: []string{"x"}},
	}

	g := NewGraph(tasks)
	_, err := g.Sort()
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("expected ErrDependencyCycle, got %v", err)
	}
}

func TestGraph_Ready(t *testing.T) {
	tasks := []TaskSpec{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c"},
	}

	g := NewGraph(tasks)

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
		Tasks:      make(map[string]*TaskState),
	}
	state.SetTaskState("task-1", "bead-abc", TaskStatusCreated)

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
	ts, ok := loaded.Tasks["task-1"]
	if !ok {
		t.Fatal("task-1 not found in loaded state")
	}
	if ts.BeadID != "bead-abc" {
		t.Errorf("expected bead ID 'bead-abc', got %q", ts.BeadID)
	}
	if ts.Status != TaskStatusCreated {
		t.Errorf("expected status 'created', got %q", ts.Status)
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
	if len(state.Tasks) != 0 {
		t.Errorf("expected empty tasks, got %d", len(state.Tasks))
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

func (m *mockBeadsClient) Create(title string, opts beads.CreateOpts) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	m.nextID++
	id := "bead-" + title
	m.created[title] = id
	m.shown[id] = &beads.Bead{ID: id, Title: title}
	return id, nil
}

func (m *mockBeadsClient) Show(id string) (*beads.Bead, error) {
	b, ok := m.shown[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return b, nil
}

func (m *mockBeadsClient) Update(id string, opts beads.UpdateOpts) error {
	return nil
}

func (m *mockBeadsClient) Close(id string, reason string) error {
	m.closed[id] = reason
	return nil
}

func (m *mockBeadsClient) AddComment(id string, body string) error {
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

	state := &State{Version: 1, Tasks: make(map[string]*TaskState)}
	client := newMockBeadsClient()

	plan, err := BuildPlan(n, state, client)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if plan.NebulaName != "test-nebula" {
		t.Errorf("expected plan name 'test-nebula', got %q", plan.NebulaName)
	}

	// All 3 tasks should be creates.
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

func TestBuildPlan_LockedTask(t *testing.T) {
	n := &Nebula{
		Manifest: Manifest{Nebula: NebulaInfo{Name: "test"}},
		Tasks:    []TaskSpec{{ID: "locked", Title: "A locked task"}},
	}

	state := &State{
		Version: 1,
		Tasks: map[string]*TaskState{
			"locked": {BeadID: "bead-123", Status: TaskStatusInProgress},
		},
	}
	client := newMockBeadsClient()
	client.shown["bead-123"] = &beads.Bead{ID: "bead-123"}

	plan, err := BuildPlan(n, state, client)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Type != ActionSkip {
		t.Errorf("expected skip action for locked task, got %s", plan.Actions[0].Type)
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

	state := &State{Version: 1, Tasks: make(map[string]*TaskState)}
	client := newMockBeadsClient()

	plan := &Plan{
		NebulaName: "test-nebula",
		Actions: []Action{
			{TaskID: "first-task", Type: ActionCreate, Reason: "new"},
			{TaskID: "second-task", Type: ActionCreate, Reason: "new"},
			{TaskID: "independent", Type: ActionCreate, Reason: "new"},
		},
	}

	if err := Apply(context.Background(), plan, n, state, client); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if len(client.created) != 3 {
		t.Errorf("expected 3 beads created, got %d", len(client.created))
	}

	// State should have all 3 tasks.
	if len(state.Tasks) != 3 {
		t.Errorf("expected 3 tasks in state, got %d", len(state.Tasks))
	}

	for _, id := range []string{"first-task", "second-task", "independent"} {
		ts, ok := state.Tasks[id]
		if !ok {
			t.Errorf("task %q not in state", id)
			continue
		}
		if ts.Status != TaskStatusCreated {
			t.Errorf("task %q status: %s, expected created", id, ts.Status)
		}
		if ts.BeadID == "" {
			t.Errorf("task %q has empty bead ID", id)
		}
	}
}

// --- Worker tests ---

type mockRunner struct {
	calls  []string
	err    error
	result *TaskRunnerResult
}

func (m *mockRunner) RunExistingTask(ctx context.Context, beadID, taskDescription string) (*TaskRunnerResult, error) {
	m.calls = append(m.calls, beadID)
	return m.result, m.err
}

func (m *mockRunner) GenerateCheckpoint(ctx context.Context, beadID, taskDescription string) (string, error) {
	return "checkpoint summary", nil
}

func TestWorkerGroup_ExecutesDependencyOrder(t *testing.T) {
	n := &Nebula{
		Dir: t.TempDir(),
		Manifest: Manifest{Nebula: NebulaInfo{Name: "test"}},
		Tasks: []TaskSpec{
			{ID: "a", Body: "task a"},
			{ID: "b", Body: "task b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Tasks: map[string]*TaskState{
			"a": {BeadID: "bead-a", Status: TaskStatusCreated},
			"b": {BeadID: "bead-b", Status: TaskStatusCreated},
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
	if state.Tasks["a"].Status != TaskStatusDone {
		t.Errorf("task a status: %s, expected done", state.Tasks["a"].Status)
	}
	if state.Tasks["b"].Status != TaskStatusDone {
		t.Errorf("task b status: %s, expected done", state.Tasks["b"].Status)
	}
}

func TestWorkerGroup_FailureBlocksDependents(t *testing.T) {
	n := &Nebula{
		Dir: t.TempDir(),
		Manifest: Manifest{Nebula: NebulaInfo{Name: "test"}},
		Tasks: []TaskSpec{
			{ID: "a", Body: "task a"},
			{ID: "b", Body: "task b", DependsOn: []string{"a"}},
		},
	}

	state := &State{
		Version: 1,
		Tasks: map[string]*TaskState{
			"a": {BeadID: "bead-a", Status: TaskStatusCreated},
			"b": {BeadID: "bead-b", Status: TaskStatusCreated},
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
	if results[0].TaskID != "a" {
		t.Errorf("expected result for task a, got %s", results[0].TaskID)
	}
	if results[0].Err == nil {
		t.Error("expected error in result for task a")
	}

	if state.Tasks["a"].Status != TaskStatusFailed {
		t.Errorf("task a status: %s, expected failed", state.Tasks["a"].Status)
	}
	// b should remain created (never touched).
	if state.Tasks["b"].Status != TaskStatusCreated {
		t.Errorf("task b status: %s, expected created (untouched)", state.Tasks["b"].Status)
	}
}
