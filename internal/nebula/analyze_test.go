package nebula

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeCodebase(t *testing.T) {
	t.Parallel()

	t.Run("small Go module", func(t *testing.T) {
		t.Parallel()

		dir := setupTestModule(t)

		analysis, err := AnalyzeCodebase(context.Background(), dir, 32000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.ModulePath != "example.com/testmod" {
			t.Errorf("module path = %q, want %q", analysis.ModulePath, "example.com/testmod")
		}
		if analysis.ProjectSnapshot == "" {
			t.Error("project snapshot is empty")
		}

		// Should find at least the root package and internal/foo.
		if len(analysis.Packages) < 2 {
			t.Fatalf("expected at least 2 packages, got %d", len(analysis.Packages))
		}

		// Check root package.
		found := false
		for _, pkg := range analysis.Packages {
			if pkg.RelativePath == "." {
				found = true
				if pkg.ImportPath != "example.com/testmod" {
					t.Errorf("root import path = %q, want %q", pkg.ImportPath, "example.com/testmod")
				}
				if !containsStr(pkg.ExportedSymbols, "Hello") {
					t.Errorf("expected Hello in exports, got %v", pkg.ExportedSymbols)
				}
				break
			}
		}
		if !found {
			t.Error("root package not found")
		}

		// Check internal/foo package.
		found = false
		for _, pkg := range analysis.Packages {
			if pkg.RelativePath == "internal/foo" {
				found = true
				if pkg.ImportPath != "example.com/testmod/internal/foo" {
					t.Errorf("foo import path = %q, want %q", pkg.ImportPath, "example.com/testmod/internal/foo")
				}
				if !containsStr(pkg.ExportedSymbols, "Bar") {
					t.Errorf("expected Bar in exports, got %v", pkg.ExportedSymbols)
				}
				if !containsStr(pkg.ExportedSymbols, "Baz") {
					t.Errorf("expected Baz in exports, got %v", pkg.ExportedSymbols)
				}
				// unexported symbols should not appear.
				if containsStr(pkg.ExportedSymbols, "private") {
					t.Errorf("unexported 'private' should not appear in exports")
				}
				break
			}
		}
		if !found {
			t.Error("internal/foo package not found")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := AnalyzeCodebase(ctx, ".", 32000)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		t.Parallel()

		result, err := AnalyzeCodebase(context.Background(), "/nonexistent/path/xyz", 32000)
		// The scanner may or may not error on a nonexistent directory
		// depending on git availability. Either way, if it succeeds,
		// the result should be valid but empty.
		if err != nil {
			return // error is acceptable
		}
		if result == nil {
			t.Fatal("expected non-nil result when no error returned")
		}
		if result.ModulePath != "" {
			t.Errorf("expected empty module path for nonexistent dir, got %q", result.ModulePath)
		}
	})

	t.Run("excludes test files", func(t *testing.T) {
		t.Parallel()

		dir := setupTestModule(t)

		analysis, err := AnalyzeCodebase(context.Background(), dir, 32000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, pkg := range analysis.Packages {
			for _, f := range pkg.GoFiles {
				if strings.HasSuffix(f, "_test.go") {
					t.Errorf("test file %q should not appear in GoFiles", f)
				}
			}
		}
	})
}

func TestFormatForPrompt(t *testing.T) {
	t.Parallel()

	t.Run("produces valid markdown sections", func(t *testing.T) {
		t.Parallel()

		analysis := &CodebaseAnalysis{
			ProjectSnapshot: "## Project\n- Module: example.com/test\n",
			ModulePath:      "example.com/test",
			Packages: []PackageSummary{
				{
					ImportPath:      "example.com/test",
					RelativePath:    ".",
					GoFiles:         []string{"main.go"},
					ExportedSymbols: []string{"Run"},
				},
				{
					ImportPath:      "example.com/test/internal/core",
					RelativePath:    "internal/core",
					GoFiles:         []string{"core.go", "types.go"},
					ExportedSymbols: []string{"Engine", "Config"},
				},
			},
		}

		output := analysis.FormatForPrompt()

		// Check required sections.
		sections := []string{"## Project Snapshot", "## Module", "## Packages"}
		for _, section := range sections {
			if !strings.Contains(output, section) {
				t.Errorf("missing section %q in output", section)
			}
		}

		// Check content.
		if !strings.Contains(output, "example.com/test") {
			t.Error("module path not in output")
		}
		if !strings.Contains(output, "Engine, Config") {
			t.Error("exported symbols not in output")
		}
		if !strings.Contains(output, "core.go, types.go") {
			t.Error("Go files not in output")
		}
	})

	t.Run("handles empty packages", func(t *testing.T) {
		t.Parallel()

		analysis := &CodebaseAnalysis{
			ProjectSnapshot: "minimal",
			Packages:        nil,
		}

		output := analysis.FormatForPrompt()
		if !strings.Contains(output, "No Go packages detected") {
			t.Error("expected 'No Go packages detected' for empty packages")
		}
	})

	t.Run("handles empty module path", func(t *testing.T) {
		t.Parallel()

		analysis := &CodebaseAnalysis{
			ProjectSnapshot: "minimal",
		}

		output := analysis.FormatForPrompt()
		if !strings.Contains(output, "(not detected)") {
			t.Error("expected '(not detected)' for empty module path")
		}
	})
}

func TestSnapshotSizeCap(t *testing.T) {
	t.Parallel()

	// Create a project with many packages to produce a large snapshot.
	dir := t.TempDir()

	// Write go.mod.
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/big\n\ngo 1.21\n")

	// Create 50 packages with several exported symbols each.
	for i := 0; i < 50; i++ {
		pkgDir := filepath.Join(dir, "pkg", padInt(i))
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		b.WriteString("package pkg" + padInt(i) + "\n\n")
		for j := 0; j < 20; j++ {
			b.WriteString("// LongExportedFunctionNameForTesting")
			b.WriteString(padInt(j))
			b.WriteString(" is a test function.\n")
			b.WriteString("func LongExportedFunctionNameForTesting")
			b.WriteString(padInt(j))
			b.WriteString("() {}\n\n")
		}
		writeFile(t, filepath.Join(pkgDir, "code.go"), b.String())
	}

	// Use a small budget to verify truncation.
	maxSize := 4000

	analysis, err := AnalyzeCodebase(context.Background(), dir, maxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := analysis.FormatForPrompt()

	// The raw snapshot is capped by the scanner. The FormatForPrompt adds
	// package data on top, so total output can exceed maxSnapshotSize.
	// But the snapshot portion itself should be within budget.
	if len(analysis.ProjectSnapshot) > maxSize {
		t.Errorf("project snapshot size %d exceeds max %d", len(analysis.ProjectSnapshot), maxSize)
	}
	// Verify it's non-empty and has required sections.
	if !strings.Contains(output, "## Project Snapshot") {
		t.Error("missing Project Snapshot section")
	}
}

// --- helpers ---

// setupTestModule creates a temporary Go module with packages for testing.
func setupTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// go.mod
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/testmod\n\ngo 1.21\n")

	// Root package with one export.
	writeFile(t, filepath.Join(dir, "main.go"), `package main

// Hello prints a greeting.
func Hello() {}

func main() {}
`)

	// internal/foo package with exports and unexported symbols.
	fooDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(fooDir, "bar.go"), `package foo

// Bar is an exported type.
type Bar struct{}

// Baz is an exported function.
func Baz() {}

func private() {}
`)

	// A test file that should be excluded.
	writeFile(t, filepath.Join(fooDir, "bar_test.go"), `package foo

import "testing"

func TestBar(t *testing.T) {}
`)

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func padInt(i int) string {
	return fmt.Sprintf("%02d", i)
}
