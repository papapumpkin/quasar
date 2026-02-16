package agentmail

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// InitDB executes the embedded schema DDL against db, creating all required
// tables if they do not already exist. The operation is idempotent.
func InitDB(db *sql.DB) error {
	stmts := splitStatements(schemaSQL)
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
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
		stmts = append(stmts, trimmed)
	}
	return stmts
}
