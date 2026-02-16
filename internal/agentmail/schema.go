package agentmail

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// InitDB executes the embedded schema DDL against db, creating all required
// tables if they do not already exist. The operation is idempotent.
func InitDB(ctx context.Context, db *sql.DB) error {
	stmts := splitStatements(schemaSQL)
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("agentmail: schema exec failed: %w", err)
		}
	}
	return nil
}

// splitStatements splits a SQL script on semicolons, returning only
// non-empty statements. Comments-only fragments are filtered out.
func splitStatements(script string) []string {
	raw := strings.Split(script, ";")
	stmts := make([]string, 0, len(raw))
	for _, s := range raw {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if isCommentOnly(trimmed) {
			continue
		}
		stmts = append(stmts, trimmed)
	}
	return stmts
}

// isCommentOnly reports whether s consists entirely of SQL line comments
// (lines starting with "--") and blank lines.
func isCommentOnly(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "--") {
			return false
		}
	}
	return true
}
