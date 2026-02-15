package nebula

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()

	// Create a phase file.
	phaseContent := "+++\nid = \"test-phase\"\ntitle = \"Test phase\"\n+++\nPhase body here.\n"
	phaseFile := filepath.Join(dir, "test-phase.md")
	if err := os.WriteFile(phaseFile, []byte(phaseContent), 0644); err != nil {
		t.Fatalf("failed to create phase file: %v", err)
	}

	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	// Modify the file.
	updatedContent := "+++\nid = \"test-phase\"\ntitle = \"Updated phase\"\n+++\nUpdated body.\n"
	if err := os.WriteFile(phaseFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("failed to update phase file: %v", err)
	}

	// Wait for change with timeout.
	select {
	case change := <-w.Changes:
		if change.PhaseID != "test-phase" {
			t.Errorf("expected phase ID 'test-phase', got %q", change.PhaseID)
		}
		if change.Kind != ChangeModified {
			t.Errorf("expected ChangeModified, got %d", change.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event")
	}
}

func TestWatcher_IgnoresNonMDFiles(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	// Write a non-md file.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Should not receive any change.
	select {
	case change := <-w.Changes:
		t.Errorf("unexpected change event: %+v", change)
	case <-time.After(300 * time.Millisecond):
		// Expected: no events for non-md files.
	}
}

func TestWatcher_DetectsRemoval(t *testing.T) {
	dir := t.TempDir()

	// Create a phase file.
	phaseFile := filepath.Join(dir, "removable.md")
	if err := os.WriteFile(phaseFile, []byte("+++\nid = \"removable\"\ntitle = \"Remove me\"\n+++\nBody.\n"), 0644); err != nil {
		t.Fatalf("failed to create phase file: %v", err)
	}

	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	// Remove the file.
	if err := os.Remove(phaseFile); err != nil {
		t.Fatalf("failed to remove phase file: %v", err)
	}

	select {
	case change := <-w.Changes:
		if change.Kind != ChangeRemoved {
			t.Errorf("expected ChangeRemoved, got %d", change.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for removal event")
	}
}
