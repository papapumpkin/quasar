package nebula

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AgentmailQuerier abstracts the database query interface needed by agentmail
// metrics collection. Both *sql.DB and *sql.Tx satisfy this interface.
type AgentmailQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// AgentmailMetrics holds coordination data collected from agentmail's Dolt
// database after a wave completes. These values feed the adaptive concurrency
// controller and are persisted alongside the main Metrics.
type AgentmailMetrics struct {
	ConflictCount int           // conflict-resolution messages between agents
	ChangeVolume  int           // changes announced since wave start
	ActiveClaims  int           // file claims currently held
	AvgClaimAge   time.Duration // mean age of active claims
}

// CollectAgentmailMetrics queries the agentmail Dolt database and populates
// coordination metrics for the given wave. Requires a database/sql connection.
// Returns nil error if db is nil (agentmail not configured), leaving metrics
// unchanged.
//
// The waveStart parameter identifies the wave boundary as a timestamp rather
// than a wave number (as originally specified in the design doc). This is more
// practical because agentmail tables use timestamps, not wave identifiers.
func CollectAgentmailMetrics(ctx context.Context, db *sql.DB, m *Metrics, waveStart time.Time) error {
	if db == nil || m == nil {
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
func queryAgentmailMetrics(ctx context.Context, q AgentmailQuerier, waveStart time.Time) (AgentmailMetrics, error) {
	var am AgentmailMetrics

	conflicts, err := queryConflictCount(ctx, q, waveStart)
	if err != nil {
		return am, err
	}
	am.ConflictCount = conflicts

	volume, err := queryChangeVolume(ctx, q, waveStart)
	if err != nil {
		return am, err
	}
	am.ChangeVolume = volume

	claims, avgAge, err := queryActiveClaims(ctx, q)
	if err != nil {
		return am, err
	}
	am.ActiveClaims = claims
	am.AvgClaimAge = avgAge

	return am, nil
}

// queryConflictCount returns the number of conflict-resolution messages sent
// between agents since the wave started. The messages table records
// inter-agent conflict notifications; counting these is a reliable signal
// of contention because file_claims has a single-row-per-file primary key
// and cannot directly represent concurrent claims.
func queryConflictCount(ctx context.Context, q AgentmailQuerier, waveStart time.Time) (int, error) {
	const query = `SELECT COUNT(*) FROM messages
		WHERE type = 'conflict' AND created_at > ?`

	var count int
	if err := q.QueryRowContext(ctx, query, waveStart).Scan(&count); err != nil {
		return 0, fmt.Errorf("querying conflict count: %w", err)
	}
	return count, nil
}

// queryChangeVolume returns the number of changes announced since waveStart.
func queryChangeVolume(ctx context.Context, q AgentmailQuerier, waveStart time.Time) (int, error) {
	const query = `SELECT COUNT(*) FROM changes WHERE announced_at > ?`

	var count int
	if err := q.QueryRowContext(ctx, query, waveStart).Scan(&count); err != nil {
		return 0, fmt.Errorf("querying change volume: %w", err)
	}
	return count, nil
}

// queryActiveClaims returns the number of active file claims and the average
// age of those claims relative to now.
func queryActiveClaims(ctx context.Context, q AgentmailQuerier) (int, time.Duration, error) {
	const query = `SELECT COUNT(*), COALESCE(AVG(TIMESTAMPDIFF(SECOND, claimed_at, NOW())), 0)
		FROM file_claims WHERE released_at IS NULL`

	var count int
	var avgSeconds float64
	if err := q.QueryRowContext(ctx, query).Scan(&count, &avgSeconds); err != nil {
		return 0, 0, fmt.Errorf("querying active claims: %w", err)
	}
	avgAge := time.Duration(avgSeconds * float64(time.Second))
	return count, avgAge, nil
}

// applyAgentmailMetrics transfers collected agentmail data into the Metrics
// struct. Conflict count is added to TotalConflicts, and the remaining
// coordination signals are recorded in the most recent wave entry.
func applyAgentmailMetrics(m *Metrics, am AgentmailMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalConflicts += am.ConflictCount

	// Store coordination signals in the most recent wave entry.
	if len(m.Waves) > 0 {
		w := &m.Waves[len(m.Waves)-1]
		w.ChangeVolume = am.ChangeVolume
		w.ActiveClaims = am.ActiveClaims
		w.AvgClaimAge = am.AvgClaimAge
	}
}
