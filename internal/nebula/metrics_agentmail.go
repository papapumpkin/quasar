package nebula

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AgentmailMetrics holds coordination data collected from agentmail's Dolt
// database after a wave completes. These values feed the adaptive concurrency
// controller and are persisted alongside the main Metrics.
type AgentmailMetrics struct {
	ConflictCount int           // files claimed by more than one agent
	ChangeVolume  int           // changes announced since wave start
	ActiveClaims  int           // file claims currently held
	AvgClaimAge   time.Duration // mean age of active claims
}

// CollectAgentmailMetrics queries the agentmail Dolt database and populates
// coordination metrics for the given wave. Requires a database/sql connection.
// Returns nil error if db is nil (agentmail not configured), leaving metrics
// unchanged.
func CollectAgentmailMetrics(ctx context.Context, db *sql.DB, m *Metrics, waveStart time.Time) error {
	if db == nil {
		return nil
	}

	am, err := queryAgentmailMetrics(ctx, db, waveStart)
	if err != nil {
		return fmt.Errorf("collecting agentmail metrics: %w", err)
	}

	applyAgentmailMetrics(m, am)
	return nil
}

// queryAgentmailMetrics runs the individual SQL queries against agentmail's
// Dolt schema and returns the raw coordination data.
func queryAgentmailMetrics(ctx context.Context, db *sql.DB, waveStart time.Time) (AgentmailMetrics, error) {
	var am AgentmailMetrics

	conflicts, err := queryConflictCount(ctx, db)
	if err != nil {
		return am, err
	}
	am.ConflictCount = conflicts

	volume, err := queryChangeVolume(ctx, db, waveStart)
	if err != nil {
		return am, err
	}
	am.ChangeVolume = volume

	claims, avgAge, err := queryActiveClaims(ctx, db)
	if err != nil {
		return am, err
	}
	am.ActiveClaims = claims
	am.AvgClaimAge = avgAge

	return am, nil
}

// queryConflictCount returns the number of file paths currently claimed by
// more than one agent, indicating contention.
func queryConflictCount(ctx context.Context, db *sql.DB) (int, error) {
	const query = `SELECT COUNT(*) FROM (
		SELECT file_path FROM file_claims
		GROUP BY file_path
		HAVING COUNT(DISTINCT agent_id) > 1
	) AS contested`

	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("querying conflict count: %w", err)
	}
	return count, nil
}

// queryChangeVolume returns the number of changes announced since waveStart.
func queryChangeVolume(ctx context.Context, db *sql.DB, waveStart time.Time) (int, error) {
	const query = `SELECT COUNT(*) FROM changes WHERE announced_at > ?`

	var count int
	if err := db.QueryRowContext(ctx, query, waveStart).Scan(&count); err != nil {
		return 0, fmt.Errorf("querying change volume: %w", err)
	}
	return count, nil
}

// queryActiveClaims returns the number of active file claims and the average
// age of those claims relative to now.
func queryActiveClaims(ctx context.Context, db *sql.DB) (int, time.Duration, error) {
	const query = `SELECT COUNT(*), COALESCE(AVG(TIMESTAMPDIFF(SECOND, claimed_at, NOW())), 0) FROM file_claims`

	var count int
	var avgSeconds float64
	if err := db.QueryRowContext(ctx, query).Scan(&count, &avgSeconds); err != nil {
		return 0, 0, fmt.Errorf("querying active claims: %w", err)
	}
	avgAge := time.Duration(avgSeconds * float64(time.Second))
	return count, avgAge, nil
}

// applyAgentmailMetrics transfers collected agentmail data into the Metrics
// struct. The conflict count is added to TotalConflicts so that both
// orchestrator-detected and agentmail-detected conflicts are tracked.
func applyAgentmailMetrics(m *Metrics, am AgentmailMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalConflicts += am.ConflictCount
}
