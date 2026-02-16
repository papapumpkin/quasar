package agentmail

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Message represents a row in the messages table.
type Message struct {
	ID        int64
	SenderID  string
	Channel   string
	Subject   string
	Body      string
	CreatedAt time.Time
}

// FileClaim represents a row in the file_claims table.
type FileClaim struct {
	FilePath  string
	AgentID   string
	ClaimedAt time.Time
}

// Change represents a row in the changes table.
type Change struct {
	ID          int64
	AgentID     string
	FilePath    string
	Summary     string
	AnnouncedAt time.Time
}

// Store provides data access for agentmail coordination state backed by a
// Dolt (MySQL-compatible) database.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store that uses the given database connection for
// persistence. The caller is responsible for calling InitDB before using
// the Store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// newUUID generates a random UUID v4 string using crypto/rand.
func newUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generating UUID: %w", err)
	}
	// Set version 4 and variant bits per RFC 4122.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}

// RegisterAgent inserts a new agent into the agents table with a generated
// UUID and returns the agent ID.
func (s *Store) RegisterAgent(ctx context.Context, name, role string) (string, error) {
	id, err := newUUID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO agents (id, name, role) VALUES (?, ?, ?)",
		id, name, role)
	if err != nil {
		return "", fmt.Errorf("registering agent: %w", err)
	}
	return id, nil
}

// Heartbeat updates the last_heartbeat timestamp for the given agent to NOW().
func (s *Store) Heartbeat(ctx context.Context, agentID string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE agents SET last_heartbeat = NOW() WHERE id = ?", agentID)
	if err != nil {
		return fmt.Errorf("updating heartbeat: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking heartbeat rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("heartbeat: agent %q not found", agentID)
	}
	return nil
}

// CleanupStaleAgents releases all file claims for agents whose last_heartbeat
// is older than timeout ago.
func (s *Store) CleanupStaleAgents(ctx context.Context, timeout time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM file_claims WHERE agent_id IN (
			SELECT id FROM agents WHERE last_heartbeat < NOW() - INTERVAL ? SECOND
		)`, int(timeout.Seconds()))
	if err != nil {
		return fmt.Errorf("cleaning up stale agents: %w", err)
	}
	return nil
}

// SendMessage inserts a message into the messages table and returns the
// auto-generated message ID.
func (s *Store) SendMessage(ctx context.Context, senderID, channel, subject, body string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO messages (sender_id, channel, subject, body) VALUES (?, ?, ?, ?)",
		senderID, channel, subject, body)
	if err != nil {
		return 0, fmt.Errorf("sending message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting message id: %w", err)
	}
	return id, nil
}

// ReadMessages returns messages ordered newest-first. If since is non-nil only
// messages created after that time are returned. If channel is non-nil only
// messages on that channel are returned.
func (s *Store) ReadMessages(ctx context.Context, since *time.Time, channel *string) ([]Message, error) {
	var (
		clauses []string
		args    []any
	)
	if since != nil {
		clauses = append(clauses, "created_at > ?")
		args = append(args, *since)
	}
	if channel != nil {
		clauses = append(clauses, "channel = ?")
		args = append(args, *channel)
	}

	query := "SELECT id, sender_id, channel, subject, body, created_at FROM messages"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Channel, &m.Subject, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ClaimFiles attempts to claim the given file paths for the agent. Files that
// are already claimed by another agent are returned in the conflicts slice.
// Successfully claimed files are returned in the claimed slice. The operation
// uses a transaction so that all claims are atomic.
func (s *Store) ClaimFiles(ctx context.Context, agentID string, files []string) (claimed, conflicts []string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("beginning claim transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, f := range files {
		// Check if file is already claimed.
		var existingAgent string
		qErr := tx.QueryRowContext(ctx,
			"SELECT agent_id FROM file_claims WHERE file_path = ?", f).Scan(&existingAgent)
		switch {
		case qErr == sql.ErrNoRows:
			// Not claimed — insert.
			if _, iErr := tx.ExecContext(ctx,
				"INSERT INTO file_claims (file_path, agent_id) VALUES (?, ?)",
				f, agentID); iErr != nil {
				err = fmt.Errorf("claiming file %q: %w", f, iErr)
				return nil, nil, err
			}
			claimed = append(claimed, f)
		case qErr != nil:
			err = fmt.Errorf("checking claim for %q: %w", f, qErr)
			return nil, nil, err
		default:
			if existingAgent == agentID {
				// Already claimed by this agent — treat as success.
				claimed = append(claimed, f)
			} else {
				conflicts = append(conflicts, f)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing claim transaction: %w", err)
	}
	return claimed, conflicts, nil
}

// ReleaseFiles removes file claims held by the given agent. Only claims
// belonging to the agent are deleted. The paths that were actually released
// are returned.
func (s *Store) ReleaseFiles(ctx context.Context, agentID string, files []string) ([]string, error) {
	var released []string
	for _, f := range files {
		res, err := s.db.ExecContext(ctx,
			"DELETE FROM file_claims WHERE file_path = ? AND agent_id = ?",
			f, agentID)
		if err != nil {
			return released, fmt.Errorf("releasing file %q: %w", f, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return released, fmt.Errorf("checking release of %q: %w", f, err)
		}
		if n > 0 {
			released = append(released, f)
		}
	}
	return released, nil
}

// GetFileClaims returns current file claims. If files is nil all claims are
// returned; otherwise only claims for the specified paths.
func (s *Store) GetFileClaims(ctx context.Context, files []string) ([]FileClaim, error) {
	var (
		query string
		args  []any
	)

	if files == nil {
		query = "SELECT file_path, agent_id, claimed_at FROM file_claims ORDER BY claimed_at DESC"
	} else {
		if len(files) == 0 {
			return nil, nil
		}
		placeholders := make([]string, len(files))
		for i, f := range files {
			placeholders[i] = "?"
			args = append(args, f)
		}
		query = "SELECT file_path, agent_id, claimed_at FROM file_claims WHERE file_path IN (" +
			strings.Join(placeholders, ",") + ") ORDER BY claimed_at DESC"
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("getting file claims: %w", err)
	}
	defer rows.Close()

	var claims []FileClaim
	for rows.Next() {
		var c FileClaim
		if err := rows.Scan(&c.FilePath, &c.AgentID, &c.ClaimedAt); err != nil {
			return nil, fmt.Errorf("scanning file claim: %w", err)
		}
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// AnnounceChange inserts a change announcement and returns the change ID.
func (s *Store) AnnounceChange(ctx context.Context, agentID, filePath, summary string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO changes (agent_id, file_path, summary) VALUES (?, ?, ?)",
		agentID, filePath, summary)
	if err != nil {
		return 0, fmt.Errorf("announcing change: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting change id: %w", err)
	}
	return id, nil
}

// GetChangesSince returns changes ordered newest-first. If since is non-nil
// only changes announced after that time are returned. If agentID is non-nil
// only changes from that agent are returned.
func (s *Store) GetChangesSince(ctx context.Context, since *time.Time, agentID *string) ([]Change, error) {
	var (
		clauses []string
		args    []any
	)
	if since != nil {
		clauses = append(clauses, "announced_at > ?")
		args = append(args, *since)
	}
	if agentID != nil {
		clauses = append(clauses, "agent_id = ?")
		args = append(args, *agentID)
	}

	query := "SELECT id, agent_id, file_path, summary, announced_at FROM changes"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY announced_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("getting changes: %w", err)
	}
	defer rows.Close()

	var changes []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.ID, &c.AgentID, &c.FilePath, &c.Summary, &c.AnnouncedAt); err != nil {
			return nil, fmt.Errorf("scanning change: %w", err)
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}
