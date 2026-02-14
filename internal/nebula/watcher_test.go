package nebula

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()

	// Create a task file.
	taskContent := "+++\nid = \"test-task\"\ntitle = \"Test task\"\n+++\nTask body here.\n"
	taskFile := filepath.Join(dir, "test-task.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatalf("failed to create task file: %v", err)
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
	updatedContent := "+++\nid = \"test-task\"\ntitle = \"Updated task\"\n+++\nUpdated body.\n"
	if err := os.WriteFile(taskFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("failed to update task file: %v", err)
	}

	// Wait for change with timeout.
	select {
	case change := <-w.Changes:
		if change.TaskID != "test-task" {
			t.Errorf("expected task ID 'test-task', got %q", change.TaskID)
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

	// Create a task file.
	taskFile := filepath.Join(dir, "removable.md")
	if err := os.WriteFile(taskFile, []byte("+++\nid = \"removable\"\ntitle = \"Remove me\"\n+++\nBody.\n"), 0644); err != nil {
		t.Fatalf("failed to create task file: %v", err)
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
	if err := os.Remove(taskFile); err != nil {
		t.Fatalf("failed to remove task file: %v", err)
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
