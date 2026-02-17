package nebula

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestPhaseFile(t *testing.T, dir, id, body string) string {
	t.Helper()
	content := "+++\nid = \"" + id + "\"\ntitle = \"Test Phase\"\n+++\n" + body
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write phase file: %v", err)
	}
	return path
}

func newTestWorkerGroup(t *testing.T) *WorkerGroup {
	t.Helper()
	var buf bytes.Buffer
	wg := &WorkerGroup{
		Logger: &buf,
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)
	return wg
}

func TestHandlePhaseModified_StoresPending(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wg := newTestWorkerGroup(t)
	path := writeTestPhaseFile(t, dir, "phase-1", "Updated instructions")

	wg.handlePhaseModified(Change{
		Kind:    ChangeModified,
		PhaseID: "phase-1",
		File:    path,
	})

	wg.mu.Lock()
	body, ok := wg.pendingRefactors["phase-1"]
	wg.mu.Unlock()
	if !ok {
		t.Fatal("expected pending refactor for phase-1")
	}
	if body != "Updated instructions" {
		t.Errorf("pendingRefactors[phase-1] = %q, want %q", body, "Updated instructions")
	}
}

func TestHandlePhaseModified_SendsToRunningLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wg := newTestWorkerGroup(t)
	refactorCh := make(chan string, 1)
	wg.RegisterPhaseLoop("phase-1", refactorCh)

	path := writeTestPhaseFile(t, dir, "phase-1", "New body for running phase")

	wg.handlePhaseModified(Change{
		Kind:    ChangeModified,
		PhaseID: "phase-1",
		File:    path,
	})

	select {
	case got := <-refactorCh:
		if got != "New body for running phase" {
			t.Errorf("refactorCh received %q, want %q", got, "New body for running phase")
		}
	default:
		t.Error("expected value on refactorCh, got nothing")
	}
}

func TestHandlePhaseModified_BadFile(t *testing.T) {
	t.Parallel()
	wg := newTestWorkerGroup(t)

	wg.handlePhaseModified(Change{
		Kind:    ChangeModified,
		PhaseID: "phase-bad",
		File:    "/nonexistent/phase-bad.md",
	})

	wg.mu.Lock()
	_, ok := wg.pendingRefactors["phase-bad"]
	wg.mu.Unlock()
	if ok {
		t.Error("should not store pending refactor for unparseable file")
	}
}

func TestRegisterPhaseLoop_FlushPending(t *testing.T) {
	t.Parallel()
	wg := newTestWorkerGroup(t)

	// Store a pending refactor before registration.
	wg.mu.Lock()
	wg.pendingRefactors["phase-2"] = "pre-registered body"
	wg.mu.Unlock()

	ch := make(chan string, 1)
	wg.RegisterPhaseLoop("phase-2", ch)

	select {
	case got := <-ch:
		if got != "pre-registered body" {
			t.Errorf("flushed body = %q, want %q", got, "pre-registered body")
		}
	default:
		t.Error("expected pending body to be flushed on registration")
	}
}

func TestUnregisterPhaseLoop(t *testing.T) {
	t.Parallel()
	wg := newTestWorkerGroup(t)
	ch := make(chan string, 1)
	wg.RegisterPhaseLoop("phase-3", ch)

	wg.UnregisterPhaseLoop("phase-3")

	wg.mu.Lock()
	_, ok := wg.phaseLoops["phase-3"]
	wg.mu.Unlock()
	if ok {
		t.Error("phase-3 should be unregistered")
	}
}

func TestHandlePhaseAdded_NoLiveState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	wg := &WorkerGroup{Logger: &buf}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	path := writeTestPhaseFile(t, dir, "new-phase", "New phase body")

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "new-phase",
		File:    path,
	})

	wg.mu.Lock()
	_, ok := wg.pendingRefactors["new-phase"]
	wg.mu.Unlock()
	if !ok {
		t.Error("expected pending entry for new-phase")
	}

	if !strings.Contains(buf.String(), "new-phase") {
		t.Error("expected log message mentioning new-phase")
	}
}

func TestHandlePhaseAdded_BadFile(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	wg := &WorkerGroup{Logger: &buf}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "bad-phase",
		File:    "/nonexistent/bad-phase.md",
	})

	if !strings.Contains(buf.String(), "warning") {
		t.Error("expected warning log for unparseable file")
	}
}

func TestHandlePhaseAdded_WithLiveState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases:   []PhaseSpec{{ID: "existing", Title: "Existing"}},
	}
	state := &State{
		Version: 1,
		Phases:  map[string]*PhaseState{"existing": {Status: PhaseStatusDone}},
	}
	graph := NewGraph(neb.Phases)
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"existing": &neb.Phases[0]},
		liveDone:       map[string]bool{"existing": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	path := writeTestPhaseFile(t, dir, "hot-phase", "Hot phase body")

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "hot-phase",
		File:    path,
	})

	// Phase should be in live data structures.
	wg.mu.Lock()
	_, inLive := wg.livePhasesByID["hot-phase"]
	ps := wg.State.Phases["hot-phase"]
	wg.mu.Unlock()

	if !inLive {
		t.Error("expected hot-phase in livePhasesByID")
	}
	if ps == nil || ps.Status != PhaseStatusPending {
		t.Errorf("expected state pending for hot-phase, got %v", ps)
	}

	// Phase has no deps and all deps are satisfied, should be on hotAdded channel.
	select {
	case id := <-wg.hotAdded:
		if id != "hot-phase" {
			t.Errorf("expected hot-phase on hotAdded channel, got %q", id)
		}
	default:
		t.Error("expected hot-phase to be immediately ready on hotAdded channel")
	}

	if !strings.Contains(buf.String(), "hot-added") {
		t.Error("expected log message about hot-add")
	}
}

func TestHandlePhaseAdded_DuplicateID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases:   []PhaseSpec{{ID: "dup", Title: "Duplicate"}},
	}
	state := &State{
		Version: 1,
		Phases:  map[string]*PhaseState{"dup": {Status: PhaseStatusPending}},
	}
	graph := NewGraph(neb.Phases)
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"dup": &neb.Phases[0]},
		liveDone:       map[string]bool{},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	path := writeTestPhaseFile(t, dir, "dup", "Duplicate phase body")

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "dup",
		File:    path,
	})

	if !strings.Contains(buf.String(), "rejected") {
		t.Error("expected rejection warning for duplicate ID")
	}

	// Should not be on hotAdded channel.
	select {
	case id := <-wg.hotAdded:
		t.Errorf("unexpected phase on hotAdded: %q", id)
	default:
		// Expected: rejected phase is not queued.
	}
}

func TestHandlePhaseAdded_WithBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases: []PhaseSpec{
			{ID: "setup", Title: "Setup"},
			{ID: "tests", Title: "Tests", DependsOn: []string{"setup"}},
		},
	}
	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"setup": {Status: PhaseStatusDone},
			"tests": {Status: PhaseStatusPending},
		},
	}
	graph := NewGraph(neb.Phases)
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"setup": &neb.Phases[0], "tests": &neb.Phases[1]},
		liveDone:       map[string]bool{"setup": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	// Write a phase file with blocks field.
	content := "+++\nid = \"middleware\"\ntitle = \"Middleware\"\ndepends_on = [\"setup\"]\nblocks = [\"tests\"]\n+++\nMiddleware body"
	path := filepath.Join(dir, "middleware.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write phase file: %v", err)
	}

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "middleware",
		File:    path,
	})

	wg.mu.Lock()
	testsPhase := wg.livePhasesByID["tests"]
	wg.mu.Unlock()

	// The "tests" phase should now depend on "middleware".
	found := false
	for _, dep := range testsPhase.DependsOn {
		if dep == "middleware" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'tests' to depend on 'middleware' after blocks injection, got %v", testsPhase.DependsOn)
	}
}

func TestHandlePhaseAdded_BlocksRunningPhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases: []PhaseSpec{
			{ID: "setup", Title: "Setup"},
			{ID: "running", Title: "Running Phase"},
		},
	}
	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"setup":   {Status: PhaseStatusDone},
			"running": {Status: PhaseStatusInProgress},
		},
	}
	graph := NewGraph(neb.Phases)
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"setup": &neb.Phases[0], "running": &neb.Phases[1]},
		liveDone:       map[string]bool{"setup": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{"running": true},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	// Write a phase that tries to block a running phase.
	content := "+++\nid = \"blocker\"\ntitle = \"Blocker\"\ndepends_on = [\"setup\"]\nblocks = [\"running\"]\n+++\nBlocker body"
	path := filepath.Join(dir, "blocker.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write phase file: %v", err)
	}

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "blocker",
		File:    path,
	})

	// Should log a warning about the running phase.
	if !strings.Contains(buf.String(), "already started/done") {
		t.Error("expected warning about blocking a running phase")
	}

	// The running phase should NOT have blocker as a dependency.
	wg.mu.Lock()
	runningPhase := wg.livePhasesByID["running"]
	wg.mu.Unlock()
	for _, dep := range runningPhase.DependsOn {
		if dep == "blocker" {
			t.Error("running phase should not have blocker as dependency")
		}
	}
}

func TestHandlePhaseAdded_OnHotAddCallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases:   []PhaseSpec{{ID: "existing", Title: "Existing"}},
	}
	state := &State{
		Version: 1,
		Phases:  map[string]*PhaseState{"existing": {Status: PhaseStatusDone}},
	}
	graph := NewGraph(neb.Phases)

	var callbackPhaseID, callbackTitle string
	var callbackDeps []string

	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"existing": &neb.Phases[0]},
		liveDone:       map[string]bool{"existing": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
		OnHotAdd: func(phaseID, title string, dependsOn []string) {
			callbackPhaseID = phaseID
			callbackTitle = title
			callbackDeps = dependsOn
		},
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	content := "+++\nid = \"callback-phase\"\ntitle = \"Callback Phase\"\ndepends_on = [\"existing\"]\n+++\nBody"
	path := filepath.Join(dir, "callback-phase.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write phase file: %v", err)
	}

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "callback-phase",
		File:    path,
	})

	if callbackPhaseID != "callback-phase" {
		t.Errorf("callback phaseID = %q, want %q", callbackPhaseID, "callback-phase")
	}
	if callbackTitle != "Callback Phase" {
		t.Errorf("callback title = %q, want %q", callbackTitle, "Callback Phase")
	}
	if len(callbackDeps) != 1 || callbackDeps[0] != "existing" {
		t.Errorf("callback deps = %v, want [existing]", callbackDeps)
	}
}

func TestHandlePhaseAdded_CreatesBead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases:   []PhaseSpec{{ID: "existing", Title: "Existing"}},
	}
	state := &State{
		Version: 1,
		Phases:  map[string]*PhaseState{"existing": {Status: PhaseStatusDone}},
	}
	graph := NewGraph(neb.Phases)
	client := newMockBeadsClient()
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		BeadsClient:    client,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"existing": &neb.Phases[0]},
		liveDone:       map[string]bool{"existing": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	path := writeTestPhaseFile(t, dir, "bead-phase", "Bead phase body")

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "bead-phase",
		File:    path,
	})

	wg.mu.Lock()
	ps := wg.State.Phases["bead-phase"]
	wg.mu.Unlock()

	if ps == nil {
		t.Fatal("expected state entry for bead-phase")
	}
	if ps.BeadID == "" {
		t.Error("expected non-empty bead ID after hot-add with BeadsClient")
	}
	if ps.Status != PhaseStatusPending {
		t.Errorf("expected status pending, got %v", ps.Status)
	}
	if len(client.created) == 0 {
		t.Error("expected BeadsClient.Create to be called")
	}
}

func TestHandlePhaseAdded_BeadCreateFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases:   []PhaseSpec{{ID: "existing", Title: "Existing"}},
	}
	state := &State{
		Version: 1,
		Phases:  map[string]*PhaseState{"existing": {Status: PhaseStatusDone}},
	}
	graph := NewGraph(neb.Phases)
	client := newMockBeadsClient()
	client.createErr = fmt.Errorf("bead creation failed")
	wg := &WorkerGroup{
		Logger:         &buf,
		Nebula:         neb,
		State:          state,
		BeadsClient:    client,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"existing": &neb.Phases[0]},
		liveDone:       map[string]bool{"existing": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       make(chan string, 16),
	}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	path := writeTestPhaseFile(t, dir, "fail-bead", "Fail bead body")

	wg.handlePhaseAdded(context.Background(), Change{
		Kind:    ChangeAdded,
		PhaseID: "fail-bead",
		File:    path,
	})

	wg.mu.Lock()
	ps := wg.State.Phases["fail-bead"]
	wg.mu.Unlock()

	if ps == nil {
		t.Fatal("expected state entry for fail-bead")
	}
	if ps.Status != PhaseStatusFailed {
		t.Errorf("expected status failed when bead creation fails, got %v", ps.Status)
	}

	// Phase should NOT be signaled as ready.
	select {
	case id := <-wg.hotAdded:
		t.Errorf("phase %q should not be on hotAdded channel after bead creation failure", id)
	default:
	}

	if !strings.Contains(buf.String(), "failed to create bead") {
		t.Error("expected warning about bead creation failure")
	}
}

func TestConsumeChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wg := newTestWorkerGroup(t)

	// Create a change channel we control.
	ch := make(chan Change, 3)
	pathC := writeTestPhaseFile(t, dir, "phase-c", "Consumed body")
	pathNew := writeTestPhaseFile(t, dir, "phase-new", "New phase body")

	ch <- Change{Kind: ChangeModified, PhaseID: "phase-c", File: pathC}
	ch <- Change{Kind: ChangeAdded, PhaseID: "phase-new", File: pathNew}
	ch <- Change{Kind: ChangeRemoved, PhaseID: "", File: "/tmp/removed.md"}
	close(ch)

	// Override watcher with a fake one that has our channel.
	wg.Watcher = &Watcher{Changes: ch}

	done := make(chan struct{})
	go func() {
		wg.consumeChanges(context.Background())
		close(done)
	}()
	<-done

	wg.mu.Lock()
	_, hasC := wg.pendingRefactors["phase-c"]
	wg.mu.Unlock()

	if !hasC {
		t.Error("expected pending refactor for phase-c")
	}
}

func TestOnRefactorCallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wg := newTestWorkerGroup(t)

	var callbackPhaseID string
	var callbackPending bool
	wg.OnRefactor = func(phaseID string, pending bool) {
		callbackPhaseID = phaseID
		callbackPending = pending
	}

	path := writeTestPhaseFile(t, dir, "phase-cb", "Callback body")
	wg.handlePhaseModified(Change{
		Kind:    ChangeModified,
		PhaseID: "phase-cb",
		File:    path,
	})

	if callbackPhaseID != "phase-cb" {
		t.Errorf("callback phaseID = %q, want %q", callbackPhaseID, "phase-cb")
	}
	if !callbackPending {
		t.Error("callback pending = false, want true")
	}
}
