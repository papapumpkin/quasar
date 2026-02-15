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
	ChangeModified ChangeKind = iota // Phase .md file edited
	ChangeRemoved                    // Phase .md file deleted
	ChangeAdded                      // New .md file appeared
)

// Change represents a detected change in the nebula directory.
type Change struct {
	Kind    ChangeKind
	PhaseID string // Derived from parsing the file (or empty on removal)
	File    string // Absolute path
}

// InterventionKind describes the type of human intervention detected.
type InterventionKind string

const (
	// InterventionPause indicates the user created a PAUSE file.
	InterventionPause InterventionKind = "pause"
	// InterventionStop indicates the user created a STOP file.
	InterventionStop InterventionKind = "stop"
	// InterventionResume indicates the user removed the PAUSE file.
	InterventionResume InterventionKind = "resume"
)

// interventionFiles maps filenames to their intervention kinds.
var interventionFiles = map[string]InterventionKind{
	"PAUSE": InterventionPause,
	"STOP":  InterventionStop,
}

// IsInterventionFile reports whether the given filename is an intervention file (PAUSE or STOP).
func IsInterventionFile(name string) bool {
	_, ok := interventionFiles[name]
	return ok
}

// Watcher monitors a nebula directory for phase file changes using fsnotify.
type Watcher struct {
	Dir           string
	Changes       <-chan Change            // Read-only external channel
	Interventions <-chan InterventionKind  // Read-only intervention channel

	changes       chan Change             // Internal write channel
	interventions chan InterventionKind   // Internal write channel
	done          chan struct{}
	watcher       *fsnotify.Watcher
}

// NewWatcher creates a new watcher for the given nebula directory.
func NewWatcher(dir string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ch := make(chan Change, 16)
	iv := make(chan InterventionKind, 4)
	w := &Watcher{
		Dir:           dir,
		Changes:       ch,
		Interventions: iv,
		changes:       ch,
		interventions: iv,
		done:          make(chan struct{}),
		watcher:       fw,
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
	close(w.interventions)
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

			// Check for intervention files (PAUSE, STOP).
			if w.handleIntervention(event) {
				continue
			}

			if !w.isPhaseFile(event.Name) {
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

// handleIntervention checks whether the event corresponds to an intervention file
// (PAUSE or STOP). If so, it emits the appropriate signal and returns true.
func (w *Watcher) handleIntervention(event fsnotify.Event) bool {
	base := filepath.Base(event.Name)
	kind, ok := interventionFiles[base]
	if !ok {
		return false
	}

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		w.interventions <- kind
		return true
	}

	if event.Has(fsnotify.Remove) && kind == InterventionPause {
		// Removing the PAUSE file signals resume.
		w.interventions <- InterventionResume
		return true
	}

	return false
}

func (w *Watcher) isPhaseFile(name string) bool {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ".md") {
		return false
	}
	// Ignore non-phase files.
	if base == "nebula.toml" || base == "nebula.state.toml" {
		return false
	}
	return true
}

func (w *Watcher) emitChange(file string) {
	// Try to parse the file to get the phase ID.
	phase, err := parsePhaseFile(file, Defaults{})
	if err != nil {
		// File may have been removed.
		w.changes <- Change{
			Kind: ChangeRemoved,
			File: file,
		}
		return
	}

	w.changes <- Change{
		Kind:    ChangeModified,
		PhaseID: phase.ID,
		File:    file,
	}
}
