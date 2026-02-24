package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectGo(t *testing.T) {
	t.Parallel()
	info := detectGo("module github.com/example/myapp\n\ngo 1.21\n")
	if info.Module != "github.com/example/myapp" {
		t.Errorf("expected module 'github.com/example/myapp', got %q", info.Module)
	}
	if info.Language != "Go" {
		t.Errorf("expected language 'Go', got %q", info.Language)
	}
}

func TestDetectNode(t *testing.T) {
	t.Parallel()
	info := detectNode(`{
  "name": "my-app",
  "version": "1.0.0"
}`)
	if info.Module != "my-app" {
		t.Errorf("expected module 'my-app', got %q", info.Module)
	}
	if info.Language != "JavaScript/TypeScript" {
		t.Errorf("expected language 'JavaScript/TypeScript', got %q", info.Language)
	}
}

func TestDetectRust(t *testing.T) {
	t.Parallel()
	info := detectRust(`[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"
`)
	if info.Module != "my-crate" {
		t.Errorf("expected module 'my-crate', got %q", info.Module)
	}
	if info.Language != "Rust" {
		t.Errorf("expected language 'Rust', got %q", info.Language)
	}
}

func TestDetectPython(t *testing.T) {
	t.Parallel()
	info := detectPython(`[project]
name = "mypackage"
version = "0.1.0"
`)
	if info.Module != "mypackage" {
		t.Errorf("expected module 'mypackage', got %q", info.Module)
	}
	if info.Language != "Python" {
		t.Errorf("expected language 'Python', got %q", info.Language)
	}
}

func TestDetectSetupPy(t *testing.T) {
	t.Parallel()
	info := detectSetupPy("from setuptools import setup\nsetup(name='foo')")
	if info.Language != "Python" {
		t.Errorf("expected language 'Python', got %q", info.Language)
	}
}

func TestParseFileList(t *testing.T) {
	t.Parallel()

	t.Run("SortsOutput", func(t *testing.T) {
		t.Parallel()
		files := parseFileList("z.go\na.go\nm.go\n")
		if len(files) != 3 {
			t.Fatalf("expected 3 files, got %d", len(files))
		}
		if files[0] != "a.go" || files[1] != "m.go" || files[2] != "z.go" {
			t.Errorf("expected sorted order, got %v", files)
		}
	})

	t.Run("SkipsEmptyLines", func(t *testing.T) {
		t.Parallel()
		files := parseFileList("\n\na.go\n\nb.go\n\n")
		if len(files) != 2 {
			t.Fatalf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		t.Parallel()
		files := parseFileList("")
		if len(files) != 0 {
			t.Fatalf("expected 0 files, got %d", len(files))
		}
	})
}

// setupTestRepo creates a temp directory with the given file structure.
// Files map paths to content. Returns the temp dir path.
func setupTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func TestScannerDeterminism(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"go.mod":             "module github.com/test/repo\n\ngo 1.21\n",
		"main.go":            "package main\n",
		"cmd/root.go":        "package cmd\n",
		"internal/loop/l.go": "package loop\n",
		"CLAUDE.md":          "# Conventions\n\nUse Go.\n",
	})

	s := &Scanner{WorkDir: dir}
	ctx := context.Background()

	result1, err := s.Scan(ctx)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	result2, err := s.Scan(ctx)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if result1 != result2 {
		t.Error("Scan produced non-deterministic output")
		t.Logf("first:\n%s", result1)
		t.Logf("second:\n%s", result2)
	}
}

func TestScannerGoProject(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"go.mod":      "module github.com/example/app\n\ngo 1.21\n",
		"main.go":     "package main\n",
		"cmd/root.go": "package cmd\n",
	})

	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(result, "github.com/example/app") {
		t.Error("expected module path in output")
	}
	if !strings.Contains(result, "Language: Go") {
		t.Error("expected 'Language: Go' in output")
	}
	if !strings.Contains(result, "## Structure") {
		t.Error("expected '## Structure' section")
	}
}

func TestScannerNodeProject(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"package.json": `{"name": "my-app", "version": "1.0.0"}`,
		"index.js":     "console.log('hi')\n",
	})

	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(result, "my-app") {
		t.Error("expected 'my-app' in output")
	}
	if !strings.Contains(result, "JavaScript/TypeScript") {
		t.Error("expected 'JavaScript/TypeScript' in output")
	}
}

func TestScannerRustProject(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"Cargo.toml":  "[package]\nname = \"my-crate\"\nversion = \"0.1.0\"\n",
		"src/main.rs": "fn main() {}\n",
	})

	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(result, "my-crate") {
		t.Error("expected 'my-crate' in output")
	}
	if !strings.Contains(result, "Rust") {
		t.Error("expected 'Rust' in output")
	}
}

func TestScannerPythonProject(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"mypackage\"\nversion = \"0.1.0\"\n",
		"src/main.py":    "print('hi')\n",
	})

	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(result, "mypackage") {
		t.Error("expected 'mypackage' in output")
	}
	if !strings.Contains(result, "Python") {
		t.Error("expected 'Python' in output")
	}
}

func TestScannerMaxSize(t *testing.T) {
	t.Parallel()
	// Create a repo with CLAUDE.md large enough to exceed a small budget.
	dir := setupTestRepo(t, map[string]string{
		"go.mod":    "module test\n\ngo 1.21\n",
		"main.go":   "package main\n",
		"CLAUDE.md": strings.Repeat("x", 5000),
	})

	s := &Scanner{
		WorkDir: dir,
		MaxSize: 1000,
	}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result) > 1000 {
		t.Errorf("expected result <= 1000 bytes, got %d", len(result))
	}
}

func TestScannerConventionsTruncation(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"go.mod":    "module test\n\ngo 1.21\n",
		"main.go":   "package main\n",
		"CLAUDE.md": strings.Repeat("conventions content ", 500),
	})

	s := &Scanner{
		WorkDir: dir,
		MaxSize: 2000,
	}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(result, "[truncated]") {
		t.Error("expected '[truncated]' marker in output")
	}
}

func TestScannerConventionsVariants(t *testing.T) {
	t.Parallel()

	t.Run("claude.md", func(t *testing.T) {
		t.Parallel()
		dir := setupTestRepo(t, map[string]string{
			"go.mod":    "module test\n\ngo 1.21\n",
			"claude.md": "# My Conventions\n",
		})
		s := &Scanner{WorkDir: dir}
		result, err := s.Scan(context.Background())
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if !strings.Contains(result, "My Conventions") {
			t.Error("expected conventions from claude.md")
		}
	})

	t.Run(".claude.md", func(t *testing.T) {
		t.Parallel()
		dir := setupTestRepo(t, map[string]string{
			"go.mod":     "module test\n\ngo 1.21\n",
			".claude.md": "# Hidden Conventions\n",
		})
		s := &Scanner{WorkDir: dir}
		result, err := s.Scan(context.Background())
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if !strings.Contains(result, "Hidden Conventions") {
			t.Error("expected conventions from .claude.md")
		}
	})
}

func TestTruncateUTF8(t *testing.T) {
	t.Parallel()

	t.Run("NoTruncation", func(t *testing.T) {
		t.Parallel()
		s := "hello"
		got := truncateUTF8(s, 10)
		if got != "hello" {
			t.Errorf("expected %q, got %q", "hello", got)
		}
	})

	t.Run("ASCIITruncation", func(t *testing.T) {
		t.Parallel()
		s := "hello world"
		got := truncateUTF8(s, 5)
		if got != "hello" {
			t.Errorf("expected %q, got %q", "hello", got)
		}
	})

	t.Run("MultiByteRuneBoundary", func(t *testing.T) {
		t.Parallel()
		// "café" = c(1) a(1) f(1) é(2) = 5 bytes total.
		s := "café"
		// Cutting at 4 would split the 2-byte é. Should back up to 3.
		got := truncateUTF8(s, 4)
		if got != "caf" {
			t.Errorf("expected %q, got %q", "caf", got)
		}
	})

	t.Run("ThreeByteRune", func(t *testing.T) {
		t.Parallel()
		// "ab€" = a(1) b(1) €(3) = 5 bytes.
		s := "ab€"
		// Cutting at 3 would split the 3-byte €. Should back up to 2.
		got := truncateUTF8(s, 3)
		if got != "ab" {
			t.Errorf("expected %q, got %q", "ab", got)
		}
	})

	t.Run("FourByteRune", func(t *testing.T) {
		t.Parallel()
		// "a𐍈" = a(1) 𐍈(4) = 5 bytes.
		s := "a𐍈"
		// Cutting at 4 would split the 4-byte rune. Should back up to 1.
		got := truncateUTF8(s, 4)
		if got != "a" {
			t.Errorf("expected %q, got %q", "a", got)
		}
	})

	t.Run("ExactRuneBoundary", func(t *testing.T) {
		t.Parallel()
		// "aé" = a(1) é(2) = 3 bytes. Cutting at 3 is exact.
		s := "aé"
		got := truncateUTF8(s, 3)
		if got != "aé" {
			t.Errorf("expected %q, got %q", "aé", got)
		}
	})

	t.Run("ZeroMax", func(t *testing.T) {
		t.Parallel()
		got := truncateUTF8("hello", 0)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestReadConventionsTruncationBudget(t *testing.T) {
	t.Parallel()

	t.Run("MarkerFitsWithinBudget", func(t *testing.T) {
		t.Parallel()
		dir := setupTestRepo(t, map[string]string{
			"CLAUDE.md": strings.Repeat("x", 500),
		})
		s := &Scanner{WorkDir: dir}
		// Budget of 100 bytes — content (500) exceeds it.
		result := s.readConventions(100)
		if len(result) > 100 {
			t.Errorf("readConventions exceeded budget: got %d bytes, want <= 100", len(result))
		}
		if !strings.Contains(result, "[truncated]") {
			t.Error("expected '[truncated]' marker")
		}
	})

	t.Run("VerySmallBudget", func(t *testing.T) {
		t.Parallel()
		dir := setupTestRepo(t, map[string]string{
			"CLAUDE.md": strings.Repeat("x", 500),
		})
		s := &Scanner{WorkDir: dir}
		// Budget smaller than the marker itself (13 bytes).
		result := s.readConventions(5)
		// Should not panic and should contain the marker.
		if !strings.Contains(result, "[truncated]") {
			t.Error("expected '[truncated]' marker even with tiny budget")
		}
	})
}

func TestWalkFilesCancellation(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	s := &Scanner{WorkDir: dir}
	s.applyDefaults()
	_, err := s.walkFiles(ctx)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestScannerMaxSizeUTF8Safe(t *testing.T) {
	t.Parallel()
	// Create a CLAUDE.md with multi-byte characters that could be split.
	dir := setupTestRepo(t, map[string]string{
		"go.mod":    "module test\n\ngo 1.21\n",
		"main.go":   "package main\n",
		"CLAUDE.md": strings.Repeat("café ", 1000), // lots of 2-byte é chars
	})

	s := &Scanner{
		WorkDir: dir,
		MaxSize: 500,
	}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result) > 500 {
		t.Errorf("expected result <= 500 bytes, got %d", len(result))
	}

	// Verify valid UTF-8 — iterate runes and check for replacement character.
	for i, r := range result {
		if r == '\uFFFD' {
			t.Errorf("invalid UTF-8 at byte offset %d", i)
			break
		}
	}
}

func TestScannerNoManifest(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t, map[string]string{
		"readme.txt": "hello\n",
	})
	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Should still produce a snapshot, just without module/language.
	if !strings.Contains(result, "## Project") {
		t.Error("expected '## Project' header even without manifest")
	}
	if !strings.Contains(result, "## Structure") {
		t.Error("expected '## Structure' section even without manifest")
	}
}

func TestScannerWalkFallback(t *testing.T) {
	t.Parallel()
	// Create a temp dir that is NOT a git repo (no .git).
	dir := setupTestRepo(t, map[string]string{
		"go.mod":   "module test\n\ngo 1.21\n",
		"main.go":  "package main\n",
		"lib/a.go": "package lib\n",
	})

	s := &Scanner{WorkDir: dir}
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// The walk fallback should still find files.
	if !strings.Contains(result, "main.go") {
		t.Error("expected 'main.go' from walk fallback")
	}
}

func TestScannerDefaults(t *testing.T) {
	t.Parallel()
	s := &Scanner{}
	s.applyDefaults()

	if s.MaxSize != DefaultMaxSize {
		t.Errorf("expected MaxSize %d, got %d", DefaultMaxSize, s.MaxSize)
	}
	if s.MaxDepth != DefaultMaxDepth {
		t.Errorf("expected MaxDepth %d, got %d", DefaultMaxDepth, s.MaxDepth)
	}
	if s.WorkDir != "." {
		t.Errorf("expected WorkDir '.', got %q", s.WorkDir)
	}
}

func TestFormatHeader(t *testing.T) {
	t.Parallel()

	t.Run("Full", func(t *testing.T) {
		t.Parallel()
		h := formatHeader(projectInfo{Module: "test/mod", Language: "Go"})
		if !strings.Contains(h, "Module: test/mod") {
			t.Error("expected module in header")
		}
		if !strings.Contains(h, "Language: Go") {
			t.Error("expected language in header")
		}
	})

	t.Run("Empty", func(t *testing.T) {
		t.Parallel()
		h := formatHeader(projectInfo{})
		if !strings.Contains(h, "## Project") {
			t.Error("expected '## Project' even with empty info")
		}
		if strings.Contains(h, "Module:") {
			t.Error("did not expect 'Module:' with empty info")
		}
	})
}
