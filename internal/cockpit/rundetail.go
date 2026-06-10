package cockpit

import (
	"context"
	"database/sql"
	"fmt"
)

// RunDetail is the view model for the run-detail page: the run header card plus
// the ordered list of star invocation steps.
type RunDetail struct {
	Run   RunCard
	Steps []StepRow
}

// StepRow is one star_invocation row projected for the step-trace table.
type StepRow struct {
	Seq              int
	Node             string
	Star             string
	State            string
	Cycle            int
	CostUSD          float64
	DurationMS       int64
	RationalePreview string
}

// LoadRunDetail loads the run header via loadRun and the ordered star
// invocations from star_invocations for the given runID. It returns (detail,
// true, nil) when found, (zero, false, nil) when the run does not exist, and
// (zero, false, err) on a database error.
func LoadRunDetail(ctx context.Context, db *sql.DB, runID string) (RunDetail, bool, error) {
	rc, found, err := loadRun(ctx, db, runID)
	if err != nil {
		return RunDetail{}, false, err
	}
	if !found {
		return RunDetail{}, false, nil
	}

	steps, err := loadSteps(ctx, db, runID)
	if err != nil {
		return RunDetail{}, false, err
	}
	return RunDetail{Run: rc, Steps: steps}, true, nil
}

// loadSteps queries star_invocations for the given run, ordered by seq
// ascending (oldest-first matches insertion order; the view reverses for
// newest-relevant display).
func loadSteps(ctx context.Context, db *sql.DB, runID string) ([]StepRow, error) {
	const q = `
		SELECT seq, node, star_name, state,
		       COALESCE(cycle, 0),
		       COALESCE(cost_usd, 0),
		       COALESCE(duration_ms, 0),
		       COALESCE(rationale_preview, '')
		FROM star_invocations
		WHERE run_id = ?
		ORDER BY seq ASC`
	rows, err := db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("cockpit: load steps for run %q: %w", runID, err)
	}
	defer rows.Close()

	var out []StepRow
	for rows.Next() {
		var s StepRow
		if err := rows.Scan(&s.Seq, &s.Node, &s.Star, &s.State,
			&s.Cycle, &s.CostUSD, &s.DurationMS, &s.RationalePreview); err != nil {
			return nil, fmt.Errorf("cockpit: scan step row: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cockpit: iterate steps: %w", err)
	}
	return out, nil
}
