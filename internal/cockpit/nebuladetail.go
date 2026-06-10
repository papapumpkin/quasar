package cockpit

import (
	"context"
	"database/sql"
	"fmt"
)

// NebulaDetail is the view model for the nebula-detail page: the nebula header
// card, its ordered phases, and its constellation runs.
type NebulaDetail struct {
	Nebula      NebulaCard
	Description string
	Phases      []PhaseRow
	Runs        []RunCard
}

// PhaseRow is one phases row projected for the nebula-detail phase list.
type PhaseRow struct {
	ID     string
	Seq    int
	Title  string
	Status string
}

// LoadNebulaDetail loads the nebula header, its phases ordered by seq, and its
// constellation runs ordered by created_at descending for the given nebulaID.
// It returns (detail, true, nil) when found, (zero, false, nil) when the nebula
// does not exist, and (zero, false, err) on a database error.
func LoadNebulaDetail(ctx context.Context, db *sql.DB, nebulaID string) (NebulaDetail, bool, error) {
	nc, desc, found, err := loadNebulaCard(ctx, db, nebulaID)
	if err != nil {
		return NebulaDetail{}, false, err
	}
	if !found {
		return NebulaDetail{}, false, nil
	}

	phases, err := loadPhases(ctx, db, nebulaID)
	if err != nil {
		return NebulaDetail{}, false, err
	}

	runs, err := loadNebulaRuns(ctx, db, nebulaID)
	if err != nil {
		return NebulaDetail{}, false, err
	}

	return NebulaDetail{Nebula: nc, Description: desc, Phases: phases, Runs: runs}, true, nil
}

// loadNebulaCard fetches the nebula header fields for a single nebula by ID,
// returning the NebulaCard, the description, and a found flag.
func loadNebulaCard(ctx context.Context, db *sql.DB, nebulaID string) (NebulaCard, string, bool, error) {
	const q = `
		SELECT id, name, description, status, source_name, source_id, source_url,
		       created_at, pr_number
		FROM nebulas
		WHERE id = ?`
	var (
		c                          NebulaCard
		name, desc, srcName, srcID sql.NullString
		srcURL                     sql.NullString
		created                    int64
		prNum                      sql.NullInt64
	)
	err := db.QueryRowContext(ctx, q, nebulaID).Scan(
		&c.ID, &name, &desc, &c.Status, &srcName, &srcID, &srcURL,
		&created, &prNum,
	)
	if err == sql.ErrNoRows {
		return NebulaCard{}, "", false, nil
	}
	if err != nil {
		return NebulaCard{}, "", false, fmt.Errorf("cockpit: load nebula %q: %w", nebulaID, err)
	}
	c.Title = name.String
	c.SourceName = srcName.String
	c.SourceID = srcID.String
	c.IssueURL = srcURL.String
	c.PRNumber = int(prNum.Int64)
	return c, desc.String, true, nil
}

// loadPhases queries phases for the given nebula ordered by seq ascending.
func loadPhases(ctx context.Context, db *sql.DB, nebulaID string) ([]PhaseRow, error) {
	const q = `
		SELECT id, seq, title, status
		FROM phases
		WHERE nebula_id = ?
		ORDER BY seq ASC`
	rows, err := db.QueryContext(ctx, q, nebulaID)
	if err != nil {
		return nil, fmt.Errorf("cockpit: load phases for nebula %q: %w", nebulaID, err)
	}
	defer rows.Close()

	var out []PhaseRow
	for rows.Next() {
		var p PhaseRow
		if err := rows.Scan(&p.ID, &p.Seq, &p.Title, &p.Status); err != nil {
			return nil, fmt.Errorf("cockpit: scan phase row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cockpit: iterate phases: %w", err)
	}
	return out, nil
}

// loadNebulaRuns queries constellation_runs for the given nebula ordered by
// created_at descending (most recent first).
func loadNebulaRuns(ctx context.Context, db *sql.DB, nebulaID string) ([]RunCard, error) {
	const q = `
		SELECT r.id, r.nebula_id, n.name, r.constellation_name, r.current_node,
		       r.step_index, r.step_count, r.state, r.cycle,
		       COALESCE((SELECT SUM(cost_usd) FROM star_invocations WHERE run_id = r.id), 0)
		FROM constellation_runs r
		JOIN nebulas n ON n.id = r.nebula_id
		WHERE r.nebula_id = ?
		ORDER BY r.created_at DESC`
	rows, err := db.QueryContext(ctx, q, nebulaID)
	if err != nil {
		return nil, fmt.Errorf("cockpit: load runs for nebula %q: %w", nebulaID, err)
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
			return nil, fmt.Errorf("cockpit: scan run row: %w", err)
		}
		c.Title = name.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cockpit: iterate nebula runs: %w", err)
	}
	return out, nil
}
