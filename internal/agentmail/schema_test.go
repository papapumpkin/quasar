package agentmail

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// testDSN returns the DSN for a test Dolt/MySQL instance.
// Set AGENTMAIL_TEST_DSN to override the default.
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("AGENTMAIL_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "root@tcp(127.0.0.1:3306)/agentmail_test"
}

func TestSplitStatements(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "CREATE TABLE foo (id INT);", 1},
		{"multiple", "CREATE TABLE a (id INT); CREATE TABLE b (id INT);", 2},
		{"trailing semicolon", "SELECT 1;", 1},
		{"comments only", "-- just a comment", 0},
		{"whitespace between", "SELECT 1; \n\n ; SELECT 2;", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitStatements(tt.input)
			if len(got) != tt.want {
				t.Errorf("splitStatements(%q) returned %d statements, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestSchemaSQL_Embedded(t *testing.T) {
	t.Parallel()
	if schemaSQL == "" {
		t.Fatal("schemaSQL is empty; embed directive may have failed")
	}
	// Verify all expected tables are present in the embedded SQL.
	for _, table := range []string{"agents", "messages", "file_claims", "changes"} {
		if !strings.Contains(schemaSQL, table) {
			t.Errorf("schemaSQL missing expected table %q", table)
		}
	}
}

func TestInitDB(t *testing.T) {
	dsn := testDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("skipping integration test: cannot open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.PingContext(ctx); err != nil {
		t.Skipf("skipping integration test: database not reachable: %v", err)
	}

	// Run InitDB twice to verify idempotency.
	if err := InitDB(ctx, db); err != nil {
		t.Fatalf("InitDB (first call) failed: %v", err)
	}
	if err := InitDB(ctx, db); err != nil {
		t.Fatalf("InitDB (second call, idempotency check) failed: %v", err)
	}

	// Verify all tables exist by querying them.
	tables := []string{"agents", "messages", "file_claims", "changes"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			_, err := db.ExecContext(ctx, fmt.Sprintf("SELECT 1 FROM `%s` LIMIT 1", table))
			if err != nil {
				t.Errorf("table %q not queryable after InitDB: %v", table, err)
			}
		})
	}
}
