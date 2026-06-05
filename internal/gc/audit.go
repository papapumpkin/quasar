// Package gc implements Quasar's garbage collector: the only path that
// hard-deletes lifecycle state from the canonical SQLite store. Completed
// nebulas, constellation runs, sensor events, and consumed trigger rows are
// first soft-deleted (deleted_at stamped) once their per-category TTL expires,
// then hard-deleted after a grace window. Unreferenced blobs are reclaimed by a
// separate mark-and-sweep, and stale git worktrees by a conservative reaper.
// Every action is appended to a JSONL audit log for post-mortems.
package gc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// Action labels the kind of GC operation an audit entry records.
const (
	ActionMark  = "mark"
	ActionSweep = "sweep"
	ActionReap  = "reap"
)

// AuditEntry is one line of the JSONL audit log. Fields with omitempty are
// populated only when relevant to the action so each line stays compact.
type AuditEntry struct {
	TS             string `json:"ts"`
	Category       string `json:"category"`
	Action         string `json:"action"`
	NebulaID       string `json:"nebula_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	Hash           string `json:"hash,omitempty"`
	RepoPath       string `json:"repo_path,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Count          int    `json:"count,omitempty"`
	CascadedPhases int    `json:"cascaded_phases,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	ReclaimedBytes int64  `json:"reclaimed_bytes,omitempty"`
	DryRun         bool   `json:"dry_run,omitempty"`
}

// AuditLog appends GC actions as JSON lines to an underlying writer. It is
// safe for concurrent use; the GC sweeps each category sequentially but the
// blob sweep runs on its own goroutine schedule.
type AuditLog struct {
	mu  sync.Mutex
	w   io.Writer
	now func() time.Time
}

// NewAuditLog returns an AuditLog writing to w. The now function stamps each
// entry's timestamp and is injectable so tests get deterministic output.
func NewAuditLog(w io.Writer, now func() time.Time) *AuditLog {
	if now == nil {
		now = time.Now
	}
	return &AuditLog{w: w, now: now}
}

// Append writes one entry as a JSON line. The TS field is set from the clock
// when the caller left it empty. A nil AuditLog is a no-op so callers need not
// guard every call site.
func (a *AuditLog) Append(e AuditEntry) error {
	if a == nil {
		return nil
	}
	if e.TS == "" {
		e.TS = a.now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("gc: marshal audit entry: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("gc: write audit entry: %w", err)
	}
	return nil
}

// ReadAuditSince parses JSONL audit entries from r and returns those whose
// timestamp is at or after since. Malformed lines are skipped so a partially
// written final line (from a crash mid-append) never aborts a tail. `quasar gc
// audit --since` uses this.
func ReadAuditSince(r io.Reader, since time.Time) ([]AuditEntry, error) {
	var out []AuditEntry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerate a torn trailing line
		}
		ts, err := time.Parse(time.RFC3339, e.TS)
		if err != nil {
			continue
		}
		if !ts.Before(since) {
			out = append(out, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("gc: read audit log: %w", err)
	}
	return out, nil
}
