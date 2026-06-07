// Package fleet implements the multi-repo fleet dashboard: a three-lane home
// view (awaiting-approval drafts, in-flight runs, recent terminal nebulas)
// grouped by registered repository.
//
// The package reads exclusively from the canonical SQLite database — it never
// imports sensor-specific packages or the runtime. Surfaces poll the database
// so they are robust to runtime restarts.
package fleet

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// terminalStatuses are the nebula lifecycle statuses considered complete; the
// Recent lane draws from these.
var terminalStatuses = []string{"merged", "done", "shipped", "canceled", "orphaned", "failed", "rejected"}

// recentLimit caps how many terminal nebulas the Recent lane shows per repo.
const recentLimit = 10

// Fleet is the full dashboard state: one RepoLane per registered repo.
type Fleet struct {
	Repos []RepoLane
}

// RepoLane holds the three lanes' cards for a single registered repository.
type RepoLane struct {
	Path             string
	DisplayName      string // last two path components, e.g. "papapumpkin/quasar"
	AwaitingApproval []NebulaCard
	InFlight         []RunCard
	Recent           []NebulaCard
	Folded           bool
}

// NebulaCard is a list-view projection of a nebula for the awaiting/recent lanes.
type NebulaCard struct {
	ID          string
	Title       string
	SourceLabel string // "#142", "manual", or "scheduled"
	Status      string
	Age         time.Duration
	PRNumber    int
	PRStatus    string // "open" | "merged" | ""
	// FailureReason is a short label (e.g. "budget exhausted") sourced from
	// the nebula's latest failed constellation_run's _error node. Populated
	// only when Status indicates failure; empty otherwise. The Recent lane
	// renders this as a parenthetical suffix on the card.
	FailureReason string
}

// RunCard is a list-view projection of a live constellation_run.
type RunCard struct {
	RunID             string
	NebulaID          string
	NebulaTitle       string
	ConstellationName string
	CurrentNode       string
	StepIndex         int
	StepCount         int
	State             string // "running" | "paused" | "blocked_on_review"
}

// Invocation is one row of a run's star-invocation trace, rendered by the
// detail view.
type Invocation struct {
	Seq      int
	Node     string
	StarName string
	State    string
	Started  time.Time
	Ended    time.Time
}

// NebulaDetail is the read-only inspection projection of a nebula row, shown
// when the operator opens a draft (or recent) card with `d` to read its seed
// prompt and provenance before approving or rejecting it.
type NebulaDetail struct {
	ID          string
	Title       string
	SourceLabel string // "#142", "manual", "scheduled"
	SourceURL   string
	Status      string
	Description string // the seed prompt / nebula description
}

// Store is the read/command layer over the fabric database for the fleet view.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store over an open fabric database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Load builds the full Fleet by querying every non-removed repo and populating
// its three lanes. Ages are computed relative to now.
func (s *Store) Load(ctx context.Context) (Fleet, error) {
	f, err := s.listRepos(ctx)
	if err != nil {
		return Fleet{}, err
	}
	now := time.Now()
	for i := range f.Repos {
		lane := &f.Repos[i]
		if lane.AwaitingApproval, err = s.awaiting(ctx, lane.Path, now); err != nil {
			return Fleet{}, err
		}
		if lane.InFlight, err = s.inFlight(ctx, lane.Path); err != nil {
			return Fleet{}, err
		}
		if lane.Recent, err = s.recent(ctx, lane.Path, now); err != nil {
			return Fleet{}, err
		}
	}
	return f, nil
}

// LoadInFlight builds a Fleet with only the in-flight lane populated per repo.
// The background 2s tick uses this so it touches just the constellation_runs
// query, never the awaiting/recent queries — keeping idle churn minimal as the
// registered-repo count grows.
func (s *Store) LoadInFlight(ctx context.Context) (Fleet, error) {
	f, err := s.listRepos(ctx)
	if err != nil {
		return Fleet{}, err
	}
	for i := range f.Repos {
		if f.Repos[i].InFlight, err = s.inFlight(ctx, f.Repos[i].Path); err != nil {
			return Fleet{}, err
		}
	}
	return f, nil
}

// listRepos returns a Fleet whose RepoLanes carry only path/display name (no
// lane cards). It is the shared repo-enumeration step for Load/LoadInFlight.
func (s *Store) listRepos(ctx context.Context) (Fleet, error) {
	const q = `SELECT path, name FROM repos WHERE status != 'removed' ORDER BY path`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return Fleet{}, fmt.Errorf("fleet: list repos: %w", err)
	}
	defer rows.Close()

	var f Fleet
	for rows.Next() {
		var path, name string
		if err := rows.Scan(&path, &name); err != nil {
			return Fleet{}, fmt.Errorf("fleet: scan repo: %w", err)
		}
		f.Repos = append(f.Repos, RepoLane{Path: path, DisplayName: displayName(path)})
	}
	if err := rows.Err(); err != nil {
		return Fleet{}, fmt.Errorf("fleet: iterate repos: %w", err)
	}
	return f, nil
}

// awaiting returns the awaiting-approval nebulas for a repo, newest first.
func (s *Store) awaiting(ctx context.Context, repo string, now time.Time) ([]NebulaCard, error) {
	const q = `
		SELECT id, name, source_name, source_id, status, created_at, pr_number, pr_merge_sha
		FROM nebulas
		WHERE repo_path = ? AND status = 'awaiting_approval' AND gc_at IS NULL
		ORDER BY created_at DESC`
	return s.scanCards(ctx, q, repo, now)
}

// recent returns the most recent terminal nebulas for a repo, newest first.
func (s *Store) recent(ctx context.Context, repo string, now time.Time) ([]NebulaCard, error) {
	q := `
		SELECT id, name, source_name, source_id, status, created_at, pr_number, pr_merge_sha
		FROM nebulas
		WHERE repo_path = ? AND status IN (` + placeholders(len(terminalStatuses)) + `) AND gc_at IS NULL
		ORDER BY updated_at DESC
		LIMIT ?`
	args := make([]any, 0, len(terminalStatuses)+2)
	args = append(args, repo)
	for _, st := range terminalStatuses {
		args = append(args, st)
	}
	args = append(args, recentLimit)
	return s.scanCardsArgs(ctx, q, now, args...)
}

// inFlight returns the running/paused constellation runs for a repo.
func (s *Store) inFlight(ctx context.Context, repo string) ([]RunCard, error) {
	const q = `
		SELECT r.id, r.nebula_id, n.name, r.constellation_name, r.current_node,
		       r.step_index, r.step_count, r.state
		FROM constellation_runs r
		JOIN nebulas n ON n.id = r.nebula_id
		WHERE n.repo_path = ? AND r.state IN ('running', 'paused', 'blocked_on_review')
		ORDER BY r.updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q, repo)
	if err != nil {
		return nil, fmt.Errorf("fleet: query in-flight %q: %w", repo, err)
	}
	defer rows.Close()

	var out []RunCard
	for rows.Next() {
		var c RunCard
		var name sql.NullString
		if err := rows.Scan(&c.RunID, &c.NebulaID, &name, &c.ConstellationName,
			&c.CurrentNode, &c.StepIndex, &c.StepCount, &c.State); err != nil {
			return nil, fmt.Errorf("fleet: scan run: %w", err)
		}
		c.NebulaTitle = name.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanCards runs a single-repo card query (repo + now binding).
func (s *Store) scanCards(ctx context.Context, q, repo string, now time.Time) ([]NebulaCard, error) {
	return s.scanCardsArgs(ctx, q, now, repo)
}

// scanCardsArgs runs a card query with arbitrary args and maps rows to cards.
// For cards in a failed lifecycle state, FailureReason is populated from the
// latest failed constellation_run's _error node.
func (s *Store) scanCardsArgs(ctx context.Context, q string, now time.Time, args ...any) ([]NebulaCard, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fleet: query nebulas: %w", err)
	}
	defer rows.Close()

	var out []NebulaCard
	for rows.Next() {
		var (
			c                            NebulaCard
			name, srcName, srcID, mergeS sql.NullString
			created                      int64
			prNum                        sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &name, &srcName, &srcID, &c.Status, &created, &prNum, &mergeS); err != nil {
			return nil, fmt.Errorf("fleet: scan nebula card: %w", err)
		}
		c.Title = name.String
		c.SourceLabel = sourceLabel(srcName.String, srcID.String)
		c.Age = now.Sub(time.Unix(created, 0))
		c.PRNumber = int(prNum.Int64)
		c.PRStatus = prStatus(int(prNum.Int64), mergeS.String)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Second pass: populate FailureReason for failed cards. Done after the
	// initial scan so we close the SELECT rows before issuing per-card
	// lookups, avoiding a nested-query lock on the same connection.
	for i := range out {
		if !isFailedStatus(out[i].Status) {
			continue
		}
		reason, err := s.failureReasonFor(ctx, out[i].ID)
		if err != nil {
			return nil, fmt.Errorf("fleet: failure reason for %q: %w", out[i].ID, err)
		}
		out[i].FailureReason = reason
	}
	return out, nil
}

// isFailedStatus reports whether a nebula status reflects a failure that may
// carry a structured failure reason from its constellation_run.
func isFailedStatus(status string) bool {
	switch status {
	case "failed", "canceled", "rejected":
		return true
	}
	return false
}

// failureReasonFor returns the brief failure label for a nebula whose latest
// constellation_run terminated in state='failed'. It reads the run's
// dag_state_toml and extracts nodes._error.reason. Returns "" when no failed
// run is found or the state has no recorded _error reason; the caller treats
// the empty string as "no detail to display."
func (s *Store) failureReasonFor(ctx context.Context, nebulaID string) (string, error) {
	const q = `
		SELECT dag_state_toml
		FROM constellation_runs
		WHERE nebula_id = ? AND state = 'failed' AND deleted_at IS NULL
		ORDER BY completed_at DESC, updated_at DESC
		LIMIT 1`
	var dagState sql.NullString
	switch err := s.db.QueryRowContext(ctx, q, nebulaID).Scan(&dagState); err {
	case nil:
	case sql.ErrNoRows:
		return "", nil
	default:
		return "", err
	}
	if !dagState.Valid || strings.TrimSpace(dagState.String) == "" {
		return "", nil
	}
	return parseErrorReason(dagState.String), nil
}

// parseErrorReason extracts nodes._error.reason from a serialized constellation
// run state. Returns "" for any malformed input or when the _error node is
// absent — the display always degrades to the bare status glyph.
func parseErrorReason(dagStateTOML string) string {
	var s struct {
		Nodes map[string]map[string]any `toml:"nodes"`
	}
	if err := toml.Unmarshal([]byte(dagStateTOML), &s); err != nil {
		return ""
	}
	errNode, ok := s.Nodes["_error"]
	if !ok {
		return ""
	}
	reason, _ := errNode["reason"].(string)
	return reason
}

// Approve transitions a nebula to "approved" and enqueues an architect trigger,
// atomically. The runtime supervisor consumes the trigger_queue row.
func (s *Store) Approve(ctx context.Context, nebulaID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fleet: begin approve tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		"UPDATE nebulas SET status = 'approved', updated_at = ? WHERE id = ?", now, nebulaID); err != nil {
		return fmt.Errorf("fleet: approve %q: %w", nebulaID, err)
	}
	// Carry the nebula's repo_path onto the trigger so the runtime supervisor
	// (later phase) can route/filter by repo without a join back to nebulas.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO trigger_queue (nebula_id, constellation_name, state, created_at, repo_path)
		 SELECT id, 'architect', 'pending', ?, repo_path FROM nebulas WHERE id = ?`,
		now, nebulaID); err != nil {
		return fmt.Errorf("fleet: enqueue trigger %q: %w", nebulaID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fleet: commit approve %q: %w", nebulaID, err)
	}
	return nil
}

// Reject transitions a nebula to "rejected". The reason is accepted for the
// operator's record; it is not yet persisted (no column on the nebula row).
func (s *Store) Reject(ctx context.Context, nebulaID, _ string) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE nebulas SET status = 'rejected', updated_at = ? WHERE id = ?",
		time.Now().Unix(), nebulaID); err != nil {
		return fmt.Errorf("fleet: reject %q: %w", nebulaID, err)
	}
	return nil
}

// SetRunState updates a constellation run's state (pause/resume/kill). The
// scheduler reaps killed runs' worktrees on its next tick.
func (s *Store) SetRunState(ctx context.Context, runID, state string) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE constellation_runs SET state = ?, updated_at = ? WHERE id = ?",
		state, time.Now().Unix(), runID); err != nil {
		return fmt.Errorf("fleet: set run state %q=%q: %w", runID, state, err)
	}
	return nil
}

// Trace returns a run's star-invocation trace ordered by sequence.
func (s *Store) Trace(ctx context.Context, runID string) ([]Invocation, error) {
	const q = `
		SELECT seq, node, star_name, state, started_at, ended_at
		FROM star_invocations WHERE run_id = ? ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("fleet: query trace %q: %w", runID, err)
	}
	defer rows.Close()

	var out []Invocation
	for rows.Next() {
		var (
			inv            Invocation
			started, ended sql.NullInt64
		)
		if err := rows.Scan(&inv.Seq, &inv.Node, &inv.StarName, &inv.State, &started, &ended); err != nil {
			return nil, fmt.Errorf("fleet: scan invocation: %w", err)
		}
		if started.Valid {
			inv.Started = time.Unix(started.Int64, 0)
		}
		if ended.Valid {
			inv.Ended = time.Unix(ended.Int64, 0)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// NebulaDetail loads a single nebula's inspection projection (title, source
// provenance, status, seed prompt) for the read-only draft-detail view.
func (s *Store) NebulaDetail(ctx context.Context, id string) (NebulaDetail, error) {
	const q = `SELECT name, source_name, source_id, source_url, status, description
		FROM nebulas WHERE id = ?`
	var (
		name, srcName, srcID, srcURL, descr sql.NullString
		status                              string
	)
	if err := s.db.QueryRowContext(ctx, q, id).Scan(
		&name, &srcName, &srcID, &srcURL, &status, &descr); err != nil {
		return NebulaDetail{}, fmt.Errorf("fleet: nebula detail %q: %w", id, err)
	}
	return NebulaDetail{
		ID:          id,
		Title:       name.String,
		SourceLabel: sourceLabel(srcName.String, srcID.String),
		SourceURL:   srcURL.String,
		Status:      status,
		Description: descr.String,
	}, nil
}

// displayName reduces an absolute repo path to its last two components.
func displayName(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(clean, "/")
	parts = nonEmpty(parts)
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return path
}

// nonEmpty drops empty path segments (leading slash, doubled separators).
func nonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// sourceLabel derives a compact provenance label from a nebula's source fields.
func sourceLabel(sourceName, sourceID string) string {
	if sourceName == "" {
		return "manual"
	}
	if i := strings.LastIndex(sourceID, "#"); i >= 0 {
		return sourceID[i:]
	}
	if sourceID != "" {
		return sourceID
	}
	return "scheduled"
}

// prStatus derives a PR status string from the nebula's PR columns.
func prStatus(prNumber int, mergeSHA string) string {
	switch {
	case mergeSHA != "":
		return "merged"
	case prNumber > 0:
		return "open"
	default:
		return ""
	}
}

// placeholders returns n comma-separated SQL "?" placeholders.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
