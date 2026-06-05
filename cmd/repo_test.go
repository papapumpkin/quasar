package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRepo executes a fresh repo command tree with args against a temp DB,
// capturing stdout and stderr. Building a new tree per call (via newRepoCmd)
// gives each invocation clean flag state, so flags never leak between tests.
func runRepo(t *testing.T, dbPath string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	cmd := newRepoCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append(args, "--db", dbPath))
	err = cmd.Execute()
	return out.String(), errBuf.String(), err
}

// gitRepoDir creates a directory with a .git subdirectory.
func gitRepoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if mkErr := os.Mkdir(filepath.Join(dir, ".git"), 0o755); mkErr != nil {
		t.Fatalf("mkdir .git: %v", mkErr)
	}
	return dir
}

func TestRepoRegisterAndList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fabric.db")
	repo := gitRepoDir(t)

	_, stderr, err := runRepo(t, dbPath, "register", repo, "--name", "myrepo")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !strings.Contains(stderr, "registered:") || !strings.Contains(stderr, "myrepo") {
		t.Errorf("register output = %q, want confirmation with name", stderr)
	}

	_, stderr, err = runRepo(t, dbPath, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stderr, repo) || !strings.Contains(stderr, "active") {
		t.Errorf("list output = %q, want repo path and status", stderr)
	}
}

func TestRepoListJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fabric.db")
	repo := gitRepoDir(t)
	if _, _, err := runRepo(t, dbPath, "register", repo); err != nil {
		t.Fatalf("register: %v", err)
	}

	stdout, _, err := runRepo(t, dbPath, "list", "--json")
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1", len(got))
	}
	if got[0]["path"] != repo {
		t.Errorf("json path = %v, want %q", got[0]["path"], repo)
	}
}

func TestRepoPauseResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fabric.db")
	repo := gitRepoDir(t)
	if _, _, err := runRepo(t, dbPath, "register", repo); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, _, err := runRepo(t, dbPath, "pause", repo); err != nil {
		t.Fatalf("pause: %v", err)
	}
	stdout, _, err := runRepo(t, dbPath, "list", "--status", "paused", "--json")
	if err != nil {
		t.Fatalf("list paused: %v", err)
	}
	if !strings.Contains(stdout, repo) {
		t.Errorf("paused list = %q, want repo present", stdout)
	}

	if _, _, err := runRepo(t, dbPath, "resume", repo); err != nil {
		t.Fatalf("resume: %v", err)
	}
	stdout, _, err = runRepo(t, dbPath, "list", "--status", "active", "--json")
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if !strings.Contains(stdout, repo) {
		t.Errorf("active list = %q, want repo present after resume", stdout)
	}
}

// TestRepoListFlagsDoNotLeak guards against flag state bleeding between command
// invocations: a prior `list --json` must not turn a later plain `list` into
// JSON. Each runRepo call must execute against a fresh command tree.
func TestRepoListFlagsDoNotLeak(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fabric.db")
	repo := gitRepoDir(t)
	if _, _, err := runRepo(t, dbPath, "register", repo); err != nil {
		t.Fatalf("register: %v", err)
	}

	// First call sets --json on its command instance.
	if _, _, err := runRepo(t, dbPath, "list", "--json"); err != nil {
		t.Fatalf("list --json: %v", err)
	}

	// Second call omits --json: it must print the human table to stderr and
	// leave stdout empty. If the flag leaked, stdout would contain JSON.
	stdout, stderr, err := runRepo(t, dbPath, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("plain list wrote to stdout = %q, want empty (json flag leaked)", stdout)
	}
	if !strings.Contains(stderr, repo) {
		t.Errorf("plain list stderr = %q, want repo path in human table", stderr)
	}
}

func TestRepoRegisterInvalidExitCode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fabric.db")
	bad := t.TempDir() // no .git

	_, _, err := runRepo(t, dbPath, "register", bad)
	if err == nil {
		t.Fatal("expected error registering non-git dir")
	}
	var ec *exitCodeError
	if !asExitError(err, &ec) {
		t.Fatalf("err %v is not an exitCodeError", err)
	}
	if ec.code != 3 {
		t.Errorf("exit code = %d, want 3 for invalid path", ec.code)
	}
}

// asExitError is a tiny errors.As wrapper kept local to avoid importing errors
// in every test.
func asExitError(err error, target **exitCodeError) bool {
	for err != nil {
		if ec, ok := err.(*exitCodeError); ok {
			*target = ec
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
