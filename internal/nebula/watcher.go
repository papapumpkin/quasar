package nebula

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeKind describes the type of file change detected.
type ChangeKind int

const (
	ChangeModified ChangeKind = iota // Task .md file edited
	ChangeRemoved                    // Task .md file deleted
	ChangeAdded                      // New .md file appeared
)

// NebulaChange represents a detected change in the nebula directory.
type NebulaChange struct {
	Kind   ChangeKind
	TaskID string // Derived from parsing the file (or empty on removal)
	File   string // Absolute path
}

// Watcher monitors a nebula directory for task file changes using fsnotify.
type Watcher struct {
	Dir     string
	Changes <-chan NebulaChange // Read-only external channel

	changes chan NebulaChange // Internal write channel
	done    chan struct{}
	watcher *fsnotify.Watcher
}

// NewWatcher creates a new watcher for the given nebula directory.
func NewWatcher(dir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ch := make(chan NebulaChange, 16)
	w := &Watcher{
		Dir:     dir,
		Changes: ch,
		changes: ch,
		done:    make(chan struct{}),
		watcher: fw,
	}
	return w, nil
}

// Start begins watching the nebula directory for changes.
func (w *Watcher) Start() error {
	if err := w.watcher.Add(w.Dir); err != nil {
		return err
	}

	go w.loop()
	return nil
}

// Stop closes the watcher and channels.
func (w *Watcher) Stop() {
	w.watcher.Close()
	<-w.done // Wait for loop to exit
	close(w.changes)
}

func (w *Watcher) loop() {
	defer close(w.done)

	// Debounce: track last event time per file.
	const debounce = 100 * time.Millisecond
	pending := make(map[string]time.Time)
	ticker := time.NewTicker(debounce)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				// Drain pending on close.
				for file := range pending {
					w.emitChange(file)
				}
				return
			}

			if !w.isTaskFile(event.Name) {
				continue
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				pending[event.Name] = time.Now()
			}

		case _, ok := <-ticker.C:
			if !ok {
				return
			}
			now := time.Now()
			for file, t := range pending {
				if now.Sub(t) >= debounce {
					w.emitChange(file)
					delete(pending, file)
				}
			}

		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// Ignore watch errors; they're non-fatal.
		}
	}
}

func (w *Watcher) isTaskFile(name string) bool {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ".md") {
		return false
	}
	// Ignore non-task files.
	if base == "nebula.toml" || base == "nebula.state.toml" {
		return false
	}
	return true
}

func (w *Watcher) emitChange(file string) {
	// Try to parse the file to get the task ID.
	task, err := parseTaskFile(file, Defaults{})
	if err != nil {
		// File may have been removed.
		w.changes <- NebulaChange{
			Kind: ChangeRemoved,
			File: file,
		}
		return
	}

	w.changes <- NebulaChange{
		Kind:   ChangeModified,
		TaskID: task.ID,
		File:   file,
	}
}
