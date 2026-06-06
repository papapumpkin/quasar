package fabric

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// migrationsFS embeds the ordered SQL migrations applied after the base schema.
// Files are named NNN_description.sql and run in lexical order, each at most
// once, tracked in the schema_migrations table.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations applies any embedded migrations not yet recorded in
// schema_migrations. It is idempotent: each migration runs at most once, in
// lexical filename order, inside its own transaction. Migrations may contain
// non-idempotent DDL (e.g. ALTER TABLE) because the ledger prevents re-runs.
func runMigrations(ctx context.Context, db *sql.DB) error {
	const createLedger = `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.ExecContext(ctx, createLedger); err != nil {
		return fmt.Errorf("fabric: create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		applied, err := migrationApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

// migrationNames returns the embedded migration filenames in lexical order.
func migrationNames() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("fabric: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// migrationApplied reports whether the named migration is already recorded.
func migrationApplied(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRowContext(ctx, "SELECT name FROM schema_migrations WHERE name = ?", name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fabric: check migration %q: %w", name, err)
	}
	return true, nil
}

// applyMigration executes the named migration file and records it, atomically.
func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	body, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("fabric: read migration %q: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fabric: begin migration %q: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("fabric: apply migration %q: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
		return fmt.Errorf("fabric: record migration %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fabric: commit migration %q: %w", name, err)
	}
	return nil
}
