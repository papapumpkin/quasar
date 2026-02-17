package nebula

import (
	"bytes"
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

func TestHandlePhaseAdded(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	wg := &WorkerGroup{Logger: &buf}
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)

	wg.handlePhaseAdded(Change{
		Kind:    ChangeAdded,
		PhaseID: "new-phase",
		File:    "/tmp/new-phase.md",
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

func TestConsumeChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wg := newTestWorkerGroup(t)

	// Create a change channel we control.
	ch := make(chan Change, 3)
	path := writeTestPhaseFile(t, dir, "phase-c", "Consumed body")

	ch <- Change{Kind: ChangeModified, PhaseID: "phase-c", File: path}
	ch <- Change{Kind: ChangeAdded, PhaseID: "phase-new", File: "/tmp/phase-new.md"}
	ch <- Change{Kind: ChangeRemoved, PhaseID: "", File: "/tmp/removed.md"}
	close(ch)

	// Override watcher with a fake one that has our channel.
	wg.Watcher = &Watcher{Changes: ch}

	done := make(chan struct{})
	go func() {
		wg.consumeChanges()
		close(done)
	}()
	<-done

	wg.mu.Lock()
	_, hasC := wg.pendingRefactors["phase-c"]
	_, hasNew := wg.pendingRefactors["phase-new"]
	wg.mu.Unlock()

	if !hasC {
		t.Error("expected pending refactor for phase-c")
	}
	if !hasNew {
		t.Error("expected pending entry for phase-new")
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
