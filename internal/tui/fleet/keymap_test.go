package fleet

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// keymapTimeout bounds teatest output waits and DB-side-effect polls so a hung
// model fails fast rather than blocking the suite.
const keymapTimeout = 5 * time.Second

// newKeymapModel seeds a single-repo fleet and returns a Model wired to it plus
// the backing DB for side-effect assertions. The state file lives in a temp dir
// so quit-time persistence never touches the developer's real config.
func newKeymapModel(t *testing.T, db *sql.DB) Model {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "tui-state.json")
	return NewModel(context.Background(), NewStore(db), statePath)
}

// seedRun inserts a running constellation_run plus one done star_invocation so
// the in-flight lane and detail trace have content.
func seedRun(t *testing.T, db *sql.DB, runID, nebulaID string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, step_count, created_at, updated_at)
		VALUES (?, ?, 'coder-reviewer', 'running', 'reviewer', 3, 5, ?, ?)`, runID, nebulaID, now, now); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO star_invocations (run_id, seq, node, star_name, state)
		VALUES (?, 1, 'coder', 'implement', 'done')`, runID); err != nil {
		t.Fatalf("seed invocation: %v", err)
	}
}

// keyRunes builds a printable-key message (e.g. "a", "d", "k").
func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// waitForOutput blocks until the program's rendered output contains substr.
func waitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	want := []byte(substr)
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, want)
	}, teatest.WithDuration(keymapTimeout), teatest.WithCheckInterval(20*time.Millisecond))
}

// waitForScalar polls a single-string-column query until it equals want.
func waitForScalar(t *testing.T, db *sql.DB, query, want string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(keymapTimeout)
	var got string
	for time.Now().Before(deadline) {
		if err := db.QueryRow(query, args...).Scan(&got); err == nil && got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("query %q = %q, want %q (timed out)", query, got, want)
}

// quit sends 'q' and waits for the program to finish so persisted state is
// flushed before the temp dir is removed.
func quit(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(keyRunes("q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(keymapTimeout))
}

func TestKeymapApprove(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "awaiting_approval")

	tm := teatest.NewTestModel(t, newKeymapModel(t, db), teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "retry") // fleet loaded, card on screen

	tm.Send(keyRunes("a")) // approve selected awaiting card
	waitForOutput(t, tm, "approved")

	waitForScalar(t, db, "SELECT status FROM nebulas WHERE id='neb-1'", "approved")
	waitForScalar(t, db,
		"SELECT constellation_name FROM trigger_queue WHERE nebula_id='neb-1' AND state='pending'",
		"nebula-lifecycle")
	quit(t, tm)
}

func TestKeymapReject(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "awaiting_approval")

	tm := teatest.NewTestModel(t, newKeymapModel(t, db), teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "retry")

	tm.Send(keyRunes("r")) // open reject-reason prompt
	waitForOutput(t, tm, "reject reason:")
	tm.Send(keyRunes("not now")) // type a reason
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForScalar(t, db, "SELECT status FROM nebulas WHERE id='neb-1'", "rejected")
	quit(t, tm)
}

func TestKeymapDetailOpen(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "draft")
	seedRun(t, db, "run-1", "neb-1")

	tm := teatest.NewTestModel(t, newKeymapModel(t, db), teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "coder-reviewer") // in-flight card rendered

	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // awaiting -> in-flight lane
	tm.Send(keyRunes("d"))                // open run detail
	waitForOutput(t, tm, "[p] pause")     // detail view opened (its control row)

	// The detail view's step trace is sourced from star_invocations. teatest's
	// Output() is a single consumable reader, so the async trace-load re-render
	// is unreliable to assert through it; assert the trace content directly.
	trace, err := NewStore(db).Trace(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}
	if rendered := RenderTrace(trace); !bytes.Contains([]byte(rendered), []byte("implement")) {
		t.Errorf("detail trace = %q, want it to include the star invocation", rendered)
	}
	quit(t, tm)
}

func TestKeymapKill(t *testing.T) {
	db := newTestDB(t)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "retry", "draft")
	seedRun(t, db, "run-1", "neb-1")

	tm := teatest.NewTestModel(t, newKeymapModel(t, db), teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "coder-reviewer")

	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // in-flight lane
	tm.Send(keyRunes("d"))                // open detail
	waitForOutput(t, tm, "[p] pause")
	tm.Send(keyRunes("k")) // kill the run

	waitForScalar(t, db, "SELECT state FROM constellation_runs WHERE id='run-1'", "killed")
	quit(t, tm)
}
