+++
id = "feedback-types"
title = "Define feedback primitives: FeedbackItem, FeedbackAction, FeedbackQueue"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/feedback/**"]
+++

## Problem

Quasar currently has no mechanism for users to inject guidance into a running nebula execution. The `HotReloader` can modify phase files on disk, and gate prompts can pause/approve phases, but there is no structured way to express higher-level intent like "make that phase more conservative", "skip the auth phase", or "add a test phase for the API layer".

We need a typed vocabulary for mid-execution feedback that can originate from multiple sources (voice transcription, TUI text input, web chat) and target specific phases or the nebula as a whole. This vocabulary must be decoupled from both the input sources and the execution engine so that new sources and new action types can be added independently.

## Solution

Create `internal/feedback/` with the core types: `FeedbackItem` (what the user said), `FeedbackAction` (what the system should do), and `FeedbackQueue` (thread-safe conduit between producers and the worker loop).

### FeedbackItem

```go
// internal/feedback/feedback.go

package feedback

import "time"

// Source identifies where a feedback item originated.
type Source string

const (
    SourceVoice Source = "voice" // From STT pipeline.
    SourceText  Source = "text"  // From TUI text input.
    SourceWeb   Source = "web"   // From web UI input.
)

// FeedbackItem represents a piece of user feedback captured from
// any input source. It carries the raw content and optional targeting
// metadata. The advisor agent interprets items into FeedbackActions.
type FeedbackItem struct {
    // ID is a unique identifier for this feedback item (UUID).
    ID string

    // Source identifies the input channel (voice, text, web).
    Source Source

    // Content is the raw user input (transcribed speech or typed text).
    Content string

    // TargetPhaseID optionally scopes this feedback to a specific phase.
    // Empty string means nebula-wide feedback.
    TargetPhaseID string

    // TargetNebula optionally scopes this feedback to a specific nebula.
    // Empty string means the currently running nebula.
    TargetNebula string

    // Priority controls ordering in the queue. Lower is higher priority.
    // Default: 2 (normal).
    Priority int

    // Timestamp is when the feedback was captured.
    Timestamp time.Time
}
```

### FeedbackAction

```go
// ActionKind discriminates the type of feedback action.
type ActionKind string

const (
    // ActionRefactorPhase modifies a phase's description or constraints
    // by injecting new instructions via the HotReloader.
    ActionRefactorPhase ActionKind = "refactor-phase"

    // ActionAdjustPriority changes a phase's priority, affecting
    // scheduling order in the Tycho DAG.
    ActionAdjustPriority ActionKind = "adjust-priority"

    // ActionAdjustBudget modifies a phase's max budget or cycle limit.
    ActionAdjustBudget ActionKind = "adjust-budget"

    // ActionAddConstraint appends a constraint to a phase's description.
    ActionAddConstraint ActionKind = "add-constraint"

    // ActionPause pauses execution of a specific phase or the entire
    // nebula. Paused phases are skipped by the scheduler until resumed.
    ActionPause ActionKind = "pause"

    // ActionResume resumes a previously paused phase or nebula.
    ActionResume ActionKind = "resume"

    // ActionSkip marks a phase as skipped, removing it from the
    // execution queue without running it.
    ActionSkip ActionKind = "skip"

    // ActionAddPhase dynamically adds a new phase to the nebula
    // via the HotReloader's hot-add machinery.
    ActionAddPhase ActionKind = "add-phase"

    // ActionAnnotate adds a human note to a phase's bead for
    // context without changing execution.
    ActionAnnotate ActionKind = "annotate"
)

// FeedbackAction is a concrete, executable action derived from a
// FeedbackItem by the advisor agent. Workers consume actions from the
// FeedbackQueue and apply them between coder-reviewer cycles.
type FeedbackAction struct {
    // Kind identifies the action type.
    Kind ActionKind

    // SourceItemID links back to the originating FeedbackItem.
    SourceItemID string

    // PhaseID is the target phase. Empty for nebula-wide actions.
    PhaseID string

    // Payload carries action-specific data. The interpretation
    // depends on Kind:
    //   refactor-phase:   new/amended description text
    //   adjust-priority:  target priority as string ("1", "3")
    //   adjust-budget:    new budget as string ("25.0") or cycle limit ("3")
    //   add-constraint:   constraint text to append
    //   pause/resume/skip: empty (PhaseID is sufficient)
    //   add-phase:        full phase spec as TOML+markdown
    //   annotate:         annotation text
    Payload string

    // Confidence is the advisor agent's confidence in this
    // interpretation (0.0-1.0). Actions below a threshold can be
    // held for human confirmation.
    Confidence float64

    // Timestamp is when the action was created.
    Timestamp time.Time
}
```

### FeedbackQueue

```go
// FeedbackQueue is a thread-safe priority queue that connects feedback
// producers (STT pipeline, TUI input, web input) with consumers
// (worker loop). It supports blocking receive for workers and
// non-blocking send for producers.
type FeedbackQueue struct {
    mu      sync.Mutex
    cond    *sync.Cond
    items   []FeedbackAction
    closed  bool
    history []FeedbackItem // all items ever enqueued, for display
    histMu  sync.RWMutex
}

// NewFeedbackQueue creates an empty queue ready for use.
func NewFeedbackQueue() *FeedbackQueue {
    q := &FeedbackQueue{}
    q.cond = sync.NewCond(&q.mu)
    return q
}

// Enqueue adds an action to the queue. It wakes one blocked Dequeue
// caller. Returns ErrQueueClosed if the queue has been closed.
func (q *FeedbackQueue) Enqueue(action FeedbackAction) error {
    q.mu.Lock()
    defer q.mu.Unlock()
    if q.closed {
        return ErrQueueClosed
    }
    // Insert in priority order (lower priority value = higher priority).
    idx := sort.Search(len(q.items), func(i int) bool {
        return q.items[i].Confidence < action.Confidence
    })
    q.items = slices.Insert(q.items, idx, action)
    q.cond.Signal()
    return nil
}

// Dequeue blocks until an action is available or the queue is closed.
// Returns ErrQueueClosed when the queue is closed and empty.
func (q *FeedbackQueue) Dequeue() (FeedbackAction, error) {
    q.mu.Lock()
    defer q.mu.Unlock()
    for len(q.items) == 0 && !q.closed {
        q.cond.Wait()
    }
    if len(q.items) == 0 {
        return FeedbackAction{}, ErrQueueClosed
    }
    action := q.items[0]
    q.items = q.items[1:]
    return action, nil
}

// TryDequeue returns the next action without blocking. Returns
// false if the queue is empty.
func (q *FeedbackQueue) TryDequeue() (FeedbackAction, bool) {
    q.mu.Lock()
    defer q.mu.Unlock()
    if len(q.items) == 0 {
        return FeedbackAction{}, false
    }
    action := q.items[0]
    q.items = q.items[1:]
    return action, true
}

// Pending returns the number of actions waiting to be consumed.
func (q *FeedbackQueue) Pending() int {
    q.mu.Lock()
    defer q.mu.Unlock()
    return len(q.items)
}

// RecordItem stores a FeedbackItem in the history for display in
// the TUI/web feedback panels. This is called by producers before
// sending to the advisor agent.
func (q *FeedbackQueue) RecordItem(item FeedbackItem) {
    q.histMu.Lock()
    defer q.histMu.Unlock()
    q.history = append(q.history, item)
}

// History returns a copy of all recorded FeedbackItems for display.
func (q *FeedbackQueue) History() []FeedbackItem {
    q.histMu.RLock()
    defer q.histMu.RUnlock()
    out := make([]FeedbackItem, len(q.history))
    copy(out, q.history)
    return out
}

// Close shuts down the queue, waking all blocked Dequeue callers.
func (q *FeedbackQueue) Close() {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.closed = true
    q.cond.Broadcast()
}

// Sentinel errors.
var ErrQueueClosed = errors.New("feedback: queue closed")
```

## Files

- `internal/feedback/feedback.go` -- `Source` type, `FeedbackItem` struct, `ActionKind` type, `FeedbackAction` struct, `FeedbackQueue` with `Enqueue`/`Dequeue`/`TryDequeue`/`Close`, `ErrQueueClosed` sentinel
- `internal/feedback/feedback_test.go` -- tests for queue ordering, blocking dequeue, close semantics, concurrent enqueue/dequeue safety, history recording

## Acceptance Criteria

- [ ] `FeedbackItem` captures source, content, optional phase/nebula target, priority, and timestamp
- [ ] `FeedbackAction` covers all nine action kinds with kind-specific payload semantics
- [ ] `FeedbackQueue.Enqueue()` is non-blocking for producers
- [ ] `FeedbackQueue.Dequeue()` blocks until an action is available or the queue is closed
- [ ] `FeedbackQueue.TryDequeue()` returns immediately with a boolean
- [ ] `FeedbackQueue.Close()` wakes all blocked `Dequeue` callers and returns `ErrQueueClosed`
- [ ] `FeedbackQueue.History()` returns all recorded items for display
- [ ] Queue is safe for concurrent use from multiple goroutines
- [ ] `go build ./internal/feedback/...` passes
- [ ] `go vet ./internal/feedback/...` passes
- [ ] `go test ./internal/feedback/...` passes with no failures
- [ ] All exported types and functions have GoDoc comments
