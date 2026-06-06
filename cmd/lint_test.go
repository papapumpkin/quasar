package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// runLintIn executes the lint command against repo with the given flags and
// returns captured stderr plus its error (an *exitCodeError on failure).
func runLintIn(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	cmd := lintCmd
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"--repo", repo}, args...))
	err := cmd.RunE(cmd, args)
	// Reset flags to defaults so subtests do not leak state.
	t.Cleanup(func() {
		_ = cmd.Flags().Set("strict", "false")
		_ = cmd.Flags().Set("json", "false")
		_ = cmd.Flags().Set("repo", "")
	})
	return errBuf.String(), err
}

func TestLintCommandCleanRepo(t *testing.T) {
	repo := t.TempDir()
	if err := lintCmd.Flags().Set("repo", repo); err != nil {
		t.Fatal(err)
	}
	_, err := runLintIn(t, repo)
	if err != nil {
		t.Fatalf("lint clean repo: %v", err)
	}
}

func TestLintCommandReportsAndExits(t *testing.T) {
	repo := t.TempDir()
	writeConstellation(t, repo, "bad.toml", `name = "bad"
[[nodes]]
id = "n"
type = "star"
star = "ghost"
[[edges]]
from = "n"
to = "_done"
`)
	if err := lintCmd.Flags().Set("repo", repo); err != nil {
		t.Fatal(err)
	}
	stderr, err := runLintIn(t, repo)
	if err == nil {
		t.Fatal("expected non-nil error for invalid artifacts")
	}
	var ec *exitCodeError
	if !errors.As(err, &ec) || ec.code != 1 {
		t.Errorf("expected exit code 1, got %v", err)
	}
	if !bytes.Contains([]byte(stderr), []byte("ghost")) {
		t.Errorf("stderr missing finding: %q", stderr)
	}
}

func writeConstellation(t *testing.T, repo, name, content string) {
	t.Helper()
	dir := filepath.Join(repo, "constellations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
