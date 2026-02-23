package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// initTestRepo creates a temporary git repo with files for testing.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo.
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	// Create files.
	writeFile(t, dir, "go.mod", "module github.com/example/test\n\ngo 1.21\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "CLAUDE.md", "# Conventions\n\n- Use Go\n- Test everything\n")

	mkdirAll(t, dir, "cmd")
	writeFile(t, dir, "cmd/root.go", "package cmd\n")

	mkdirAll(t, dir, "internal/loop")
	writeFile(t, dir, "internal/loop/loop.go", "package loop\n")

	// Git add and commit.
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, dir, sub string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestScanDeterministic(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{}
	ctx := context.Background()

	snap1, err := s.Scan(ctx, dir)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}

	snap2, err := s.Scan(ctx, dir)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}

	if snap1 != snap2 {
		t.Error("snapshots are not deterministic")
	}
}

func TestScanContainsProjectInfo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain module name.
	if !strings.Contains(snap, "github.com/example/test") {
		t.Error("snapshot should contain module name")
	}

	// Should contain language.
	if !strings.Contains(snap, "Go") {
		t.Error("snapshot should contain language")
	}

	// Should contain directory tree.
	if !strings.Contains(snap, "cmd/") {
		t.Error("snapshot should contain directory tree")
	}

	// Should contain conventions.
	if !strings.Contains(snap, "Use Go") {
		t.Error("snapshot should contain CLAUDE.md content")
	}
}

func TestScanTreeDepthLimit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{TreeDepth: 1}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// At depth 1, we should see "internal/" and "internal/loop/" but not
	// the contents of internal/loop/.
	if !strings.Contains(snap, "internal/") {
		t.Error("depth 1 should show internal/")
	}
	// loop.go is at depth 2 (internal -> loop -> loop.go), should not appear.
	if strings.Contains(snap, "loop.go") {
		t.Error("depth 1 should not show loop.go (depth 2)")
	}
}

func TestScanMaxChars(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{MaxChars: 100}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(snap) > 100 {
		t.Errorf("snapshot exceeds MaxChars: %d > 100", len(snap))
	}
	if !strings.Contains(snap, "[... truncated for size ...]") {
		t.Error("truncated snapshot should have marker")
	}
}

func TestScanNonGoRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	writeFile(t, dir, "package.json", `{"name": "my-app", "version": "1.0.0"}`)

	mkdirAll(t, dir, "src")
	writeFile(t, dir, "src/index.js", "console.log('hello');\n")

	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(snap, "my-app") {
		t.Error("should detect package.json name")
	}
	if !strings.Contains(snap, "JavaScript") {
		t.Error("should detect JavaScript language")
	}
}

func TestScanNoConventions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	writeFile(t, dir, "main.py", "print('hello')\n")

	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should not have conventions section.
	if strings.Contains(snap, "## Conventions") {
		t.Error("should not have conventions section without CLAUDE.md")
	}

	// Should still have structure.
	if !strings.Contains(snap, "## Structure") {
		t.Error("should have structure section")
	}
}

func TestScanUntrackedConventionIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	writeFile(t, dir, "main.go", "package main\n")

	// Add an untracked CLAUDE.md (not git-added).
	writeFile(t, dir, "CLAUDE.md", "# Should be ignored\n")

	run(t, dir, "git", "add", "main.go")
	run(t, dir, "git", "commit", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Untracked CLAUDE.md should not appear in snapshot.
	if strings.Contains(snap, "## Conventions") {
		t.Error("untracked CLAUDE.md should not produce conventions section")
	}
	if strings.Contains(snap, "Should be ignored") {
		t.Error("untracked CLAUDE.md content should not appear in snapshot")
	}
}

func TestScanEmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	// Create an empty initial commit (git allows --allow-empty).
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should still produce a valid snapshot with project header.
	if !strings.Contains(snap, "# Project Context") {
		t.Error("empty repo should still have project context header")
	}
	if !strings.Contains(snap, "## Structure") {
		t.Error("empty repo should still have structure section")
	}
	// Should not have conventions.
	if strings.Contains(snap, "## Conventions") {
		t.Error("empty repo should not have conventions section")
	}
}

func TestScanDeterminismStress(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{}
	ctx := context.Background()
	const runs = 100

	// Run scans in parallel and collect results.
	snapshots := make([]string, runs)
	errs := make([]error, runs)
	var wg sync.WaitGroup
	wg.Add(runs)

	for i := 0; i < runs; i++ {
		go func(idx int) {
			defer wg.Done()
			snap, err := s.Scan(ctx, dir)
			snapshots[idx] = snap
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// Check for scan errors.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("scan %d failed: %v", i, err)
		}
	}

	// All snapshots must be byte-identical.
	for i := 1; i < runs; i++ {
		if snapshots[0] != snapshots[i] {
			t.Errorf("snapshot 0 and %d differ (len %d vs %d)",
				i, len(snapshots[0]), len(snapshots[i]))
			// Show first divergence point for debugging.
			for j := 0; j < len(snapshots[0]) && j < len(snapshots[i]); j++ {
				if snapshots[0][j] != snapshots[i][j] {
					start := j - 20
					if start < 0 {
						start = 0
					}
					end0 := j + 20
					if end0 > len(snapshots[0]) {
						end0 = len(snapshots[0])
					}
					endi := j + 20
					if endi > len(snapshots[i]) {
						endi = len(snapshots[i])
					}
					t.Errorf("first difference at byte %d:\n  snap 0: ...%q...\n  snap %d: ...%q...",
						j, snapshots[0][start:end0], i, snapshots[i][start:endi])
					break
				}
			}
			break // one failure is enough
		}
	}
}

func TestScanDeterministicByteEquality(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	s := &Scanner{}
	ctx := context.Background()

	// Run scan 5 times and assert all are byte-identical.
	var snapshots []string
	for i := 0; i < 5; i++ {
		snap, err := s.Scan(ctx, dir)
		if err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
		snapshots = append(snapshots, snap)
	}

	for i := 1; i < len(snapshots); i++ {
		if snapshots[0] != snapshots[i] {
			t.Errorf("snapshot 0 and %d differ:\n--- snap 0 ---\n%s\n--- snap %d ---\n%s",
				i, snapshots[0], i, snapshots[i])
		}
	}
}

func TestScanRustProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"my-crate\"\nversion = \"0.1.0\"\n")

	mkdirAll(t, dir, "src")
	writeFile(t, dir, "src/main.rs", "fn main() {}\n")

	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(snap, "Rust") {
		t.Error("should detect Rust language")
	}
	if !strings.Contains(snap, "my-crate") {
		t.Error("should detect Cargo.toml name")
	}
}

func TestScanNoManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")

	writeFile(t, dir, "README.txt", "hello\n")
	writeFile(t, dir, "data.csv", "a,b,c\n")

	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	s := &Scanner{}
	snap, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Without a manifest, it should fall back to the directory name.
	if !strings.Contains(snap, "**Directory**") {
		t.Error("repo without manifest should show directory fallback")
	}
	// Should not show language or module.
	if strings.Contains(snap, "**Language**") {
		t.Error("repo without manifest should not show language")
	}
}
