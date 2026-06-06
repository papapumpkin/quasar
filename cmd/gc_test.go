package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/papapumpkin/quasar/internal/gc"
)

func TestAuditDetail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry gc.AuditEntry
		want  string
	}{
		{"nebula", gc.AuditEntry{NebulaID: "neb-1"}, "neb-1"},
		{"run", gc.AuditEntry{RunID: "run-1"}, "run-1"},
		{"hash", gc.AuditEntry{Hash: "deadbeef"}, "deadbeef"},
		{"repo", gc.AuditEntry{RepoPath: "/repo"}, "/repo"},
		{"count fallback", gc.AuditEntry{Count: 5}, "count=5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := auditDetail(c.entry); got != c.want {
				t.Errorf("auditDetail = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGCAuditPath(t *testing.T) {
	t.Parallel()
	got := gcAuditPath("/var/lib/quasar/fabric.db")
	want := filepath.Join("/var/lib/quasar", "gc-audit.log")
	if got != want {
		t.Errorf("gcAuditPath = %q, want %q", got, want)
	}
}

// newAuditCmd builds a minimal cobra command carrying the --since flag and a
// context, matching what runGCAudit reads.
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Duration("since", 24*time.Hour, "")
	cmd.SetContext(context.Background())
	return cmd
}

func TestRunGCAudit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fabric.db")
	viper.Set("fabric_db", dbPath)
	t.Cleanup(func() { viper.Set("fabric_db", "") })

	t.Run("reports gracefully when no audit log exists", func(t *testing.T) {
		if err := runGCAudit(newAuditCmd(), nil); err != nil {
			t.Errorf("runGCAudit with no log = %v, want nil", err)
		}
	})

	t.Run("reads an existing audit log without error", func(t *testing.T) {
		entry := gc.AuditEntry{
			TS:       time.Now().UTC().Format(time.RFC3339),
			Category: gc.CategoryCompletedNebulas,
			Action:   gc.ActionMark,
			NebulaID: "neb-1",
		}
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(gcAuditPath(dbPath), append(line, '\n'), 0o644); err != nil {
			t.Fatalf("write audit log: %v", err)
		}
		if err := runGCAudit(newAuditCmd(), nil); err != nil {
			t.Errorf("runGCAudit = %v, want nil", err)
		}
	})
}
