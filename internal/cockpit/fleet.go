package cockpit

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// terminalStatuses are the nebula lifecycle statuses considered complete; the
// Recent lane draws from these. Mirrors internal/tui/fleet.
var terminalStatuses = []string{"merged", "done", "shipped", "canceled", "orphaned", "failed", "rejected"}

// recentLimit caps how many terminal nebulas the Recent lane shows per repo.
const recentLimit = 10

// Fleet is the full Mission Control dashboard state: one RepoLane per
// registered repository.
type Fleet struct {
	Repos []RepoLane
}

// RepoLane holds the three lanes' cards for a single registered repository.
type RepoLane struct {
	Path        string
	DisplayName string
	Awaiting    []NebulaCard
	InFlight    []RunCard
	Recent      []NebulaCard
}

// NebulaCard is a list-view projection of a nebula for the awaiting/recent
// lanes.
type NebulaCard struct {
	ID         string
	Title      string // nebula name
	Status     string // awaiting_approval | merged | failed | open_pr | ...
	SourceName string // e.g. "github"
	SourceID   string // e.g. issue number / "papapumpkin/quasar#42"
	IssueURL   string // may be ""
	PRNumber   int    // 0 if none
	AgeLabel   string // e.g. "14m"
}

// RunCard is a list-view projection of a live constellation_run.
type RunCard struct {
	ID                string
	NebulaID          string
	Title             string // nebula name
	ConstellationName string
	CurrentNode       string
	StepIndex         int
	StepCount         int
	State             string
	CostUSD           float64
	Cycle             int
	MaxCycles         int
}

// LoadFleet builds the full Fleet by querying every non-removed repo and
// populating its three lanes (awaiting-approval, in-flight, recent). Ages are
// computed relative to now. It reads exclusively from the fabric SQLite
// database so it is robust to runtime restarts.
func LoadFleet(ctx context.Context, db *sql.DB) (Fleet, error) {
	repos, err := listRepos(ctx, db)
	if err != nil {
		return Fleet{}, err
	}
	now := time.Now()
	for i := range repos {
		lane := &repos[i]
		if lane.Awaiting, err = loadAwaiting(ctx, db, lane.Path, now); err != nil {
			return Fleet{}, err
		}
		if lane.InFlight, err = loadInFlight(ctx, db, lane.Path); err != nil {
			return Fleet{}, err
		}
		if lane.Recent, err = loadRecent(ctx, db, lane.Path, now); err != nil {
			return Fleet{}, err
		}
	}
	return Fleet{Repos: repos}, nil
}

// listRepos returns the registered, non-removed repos as bare RepoLanes
// (path + display name, no cards). Mirrors the TUI fleet repo enumeration.
func listRepos(ctx context.Context, db *sql.DB) ([]RepoLane, error) {
	const q = `SELECT path, name FROM repos WHERE status != 'removed' ORDER BY path`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("cockpit: list repos: %w", err)
	}
	defer rows.Close()

	var out []RepoLane
	for rows.Next() {
		var path, name string
		if err := rows.Scan(&path, &name); err != nil {
			return nil, fmt.Errorf("cockpit: scan repo: %w", err)
		}
		out = append(out, RepoLane{Path: path, DisplayName: displayName(path)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cockpit: iterate repos: %w", err)
	}
	return out, nil
}

// loadAwaiting returns the awaiting-approval nebulas for a repo, newest first.
func loadAwaiting(ctx context.Context, db *sql.DB, repo string, now time.Time) ([]NebulaCard, error) {
	const q = `
		SELECT id, name, source_name, source_id, source_url, status, created_at, pr_number
		FROM nebulas
		WHERE repo_path = ? AND status = 'awaiting_approval' AND gc_at IS NULL
		ORDER BY created_at DESC`
	return scanCards(ctx, db, now, q, repo)
}

// loadRecent returns the most recent terminal nebulas for a repo, newest first.
func loadRecent(ctx context.Context, db *sql.DB, repo string, now time.Time) ([]NebulaCard, error) {
	q := `
		SELECT id, name, source_name, source_id, source_url, status, created_at, pr_number
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
	return scanCards(ctx, db, now, q, args...)
}

// loadInFlight returns the running/paused constellation runs for a repo joined
// to their nebulas, with per-run summed star-invocation cost.
func loadInFlight(ctx context.Context, db *sql.DB, repo string) ([]RunCard, error) {
	const q = `
		SELECT r.id, r.nebula_id, n.name, r.constellation_name, r.current_node,
		       r.step_index, r.step_count, r.state, r.cycle,
		       COALESCE((SELECT SUM(cost_usd) FROM star_invocations WHERE run_id = r.id), 0)
		FROM constellation_runs r
		JOIN nebulas n ON n.id = r.nebula_id
		WHERE n.repo_path = ? AND r.state IN ('running', 'paused', 'blocked_on_review')
		ORDER BY r.updated_at DESC`
	rows, err := db.QueryContext(ctx, q, repo)
	if err != nil {
		return nil, fmt.Errorf("cockpit: query in-flight %q: %w", repo, err)
	}
	defer rows.Close()

	var out []RunCard
	for rows.Next() {
		var (
			c    RunCard
			name sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.NebulaID, &name, &c.ConstellationName,
			&c.CurrentNode, &c.StepIndex, &c.StepCount, &c.State, &c.Cycle, &c.CostUSD); err != nil {
			return nil, fmt.Errorf("cockpit: scan run: %w", err)
		}
		c.Title = name.String
		// MaxCycles has no dedicated column on constellation_runs; a later task
		// enriches it from the constellation manifest. Leave the zero value.
		out = append(out, c)
	}
	return out, rows.Err()
}

// loadRun fetches a single constellation_run by its ID, returning the RunCard
// and a found flag. The query mirrors loadInFlight but filters by run id.
func loadRun(ctx context.Context, db *sql.DB, runID string) (RunCard, bool, error) {
	const q = `
		SELECT r.id, r.nebula_id, n.name, r.constellation_name, r.current_node,
		       r.step_index, r.step_count, r.state, r.cycle,
		       COALESCE((SELECT SUM(cost_usd) FROM star_invocations WHERE run_id = r.id), 0)
		FROM constellation_runs r
		JOIN nebulas n ON n.id = r.nebula_id
		WHERE r.id = ?`
	var (
		c    RunCard
		name sql.NullString
	)
	err := db.QueryRowContext(ctx, q, runID).Scan(
		&c.ID, &c.NebulaID, &name, &c.ConstellationName,
		&c.CurrentNode, &c.StepIndex, &c.StepCount, &c.State, &c.Cycle, &c.CostUSD,
	)
	if err == sql.ErrNoRows {
		return RunCard{}, false, nil
	}
	if err != nil {
		return RunCard{}, false, fmt.Errorf("cockpit: load run %q: %w", runID, err)
	}
	c.Title = name.String
	return c, true, nil
}

// scanCards runs a nebula card query and maps rows to NebulaCards.
func scanCards(ctx context.Context, db *sql.DB, now time.Time, q string, args ...any) ([]NebulaCard, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cockpit: query nebulas: %w", err)
	}
	defer rows.Close()

	var out []NebulaCard
	for rows.Next() {
		var (
			c                            NebulaCard
			name, srcName, srcID, srcURL sql.NullString
			created                      int64
			prNum                        sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &name, &srcName, &srcID, &srcURL, &c.Status, &created, &prNum); err != nil {
			return nil, fmt.Errorf("cockpit: scan nebula card: %w", err)
		}
		c.Title = name.String
		c.SourceName = srcName.String
		c.SourceID = srcID.String
		c.IssueURL = srcURL.String
		c.PRNumber = int(prNum.Int64)
		c.AgeLabel = ageLabel(now.Sub(time.Unix(created, 0)))
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cockpit: iterate nebulas: %w", err)
	}
	return out, nil
}

// displayName reduces an absolute repo path to its last two components, e.g.
// "papapumpkin/quasar".
func displayName(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	parts := nonEmpty(strings.Split(clean, "/"))
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

// ageLabel renders a duration as a compact human label, e.g. "14m", "1h", "3d".
func ageLabel(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// placeholders returns n comma-separated SQL "?" placeholders.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
