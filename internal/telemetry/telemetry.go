// Package telemetry provides a JSONL event stream for recording state transitions
// during nebula executions. Every agent invocation, state change, discovery, and
// completion is recorded as a structured JSON event, making runs auditable,
// replayable, and analyzable.
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event kinds identify the type of telemetry event.
const (
	KindEpochStart         = "epoch_start"
	KindEpochDone          = "epoch_done"
	KindTaskState          = "task_state"
	KindAgentStart         = "agent_start"
	KindAgentDone          = "agent_done"
	KindEntanglementPosted = "entanglement_posted"
	KindClaimAcquired      = "claim_acquired"
	KindClaimReleased      = "claim_released"
	KindDiscoveryPosted    = "discovery_posted"
	KindDiscoveryResolved  = "discovery_resolved"
	KindFilterResult       = "filter_result"
	KindCycleStart         = "cycle_start"
	KindCycleDone          = "cycle_done"
)

// Event represents a single telemetry record. Each event carries a timestamp,
// a kind tag, and optional context identifiers (epoch, task) along with
// arbitrary structured data.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Kind      string    `json:"kind"`
	EpochID   string    `json:"epoch,omitempty"`
	TaskID    string    `json:"task,omitempty"`
	Data      any       `json:"data,omitempty"`
}

// Emitter writes telemetry events to a JSONL file. It is safe for concurrent
// use by multiple goroutines. A nil *Emitter is a valid no-op emitter.
type Emitter struct {
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

// NewEmitter creates a new Emitter that writes JSONL events to the file at
// path. The file is created if it does not exist, or appended to if it does.
func NewEmitter(path string) (*Emitter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open %s: %w", path, err)
	}
	return &Emitter{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Emit writes a single event to the JSONL file. It is safe for concurrent use.
// Calling Emit on a nil Emitter is a no-op.
func (e *Emitter) Emit(evt Event) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enc.Encode(evt); err != nil {
		return fmt.Errorf("telemetry: encode event: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file. Calling Close on a nil
// Emitter is a no-op.
func (e *Emitter) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.file.Close(); err != nil {
		return fmt.Errorf("telemetry: close: %w", err)
	}
	return nil
}
