package snapshot

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildTree(t *testing.T) {
	t.Parallel()

	t.Run("EmptyInput", func(t *testing.T) {
		t.Parallel()
		root := buildTree(nil)
		if root.name != "." {
			t.Fatalf("expected root name '.', got %q", root.name)
		}
		if len(root.children) != 0 {
			t.Fatalf("expected 0 children, got %d", len(root.children))
		}
	})

	t.Run("FlatFiles", func(t *testing.T) {
		t.Parallel()
		root := buildTree([]string{"a.go", "b.go", "c.go"})
		if len(root.files) != 3 {
			t.Fatalf("expected 3 files, got %d", len(root.files))
		}
	})

	t.Run("NestedDirs", func(t *testing.T) {
		t.Parallel()
		root := buildTree([]string{
			"cmd/root.go",
			"cmd/run.go",
			"internal/loop/loop.go",
		})
		if len(root.children) != 2 {
			t.Fatalf("expected 2 children (cmd, internal), got %d", len(root.children))
		}
	})
}

func TestRenderTree(t *testing.T) {
	t.Parallel()

	t.Run("BasicRender", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"cmd/root.go",
			"cmd/run.go",
			"internal/loop/loop.go",
			"main.go",
		}
		root := buildTree(files)
		result := renderTree(root, 5, 10000)

		if !strings.Contains(result, "cmd/") {
			t.Error("expected 'cmd/' in output")
		}
		if !strings.Contains(result, "internal/") {
			t.Error("expected 'internal/' in output")
		}
		if !strings.Contains(result, "main.go") {
			t.Error("expected 'main.go' in output")
		}
	})

	t.Run("DepthLimiting", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"a/b/c/d.go",
		}
		root := buildTree(files)

		// Depth 1 should show a/ and b/ but not c/.
		result := renderTree(root, 1, 10000)
		if !strings.Contains(result, "a/") {
			t.Error("expected 'a/' in depth-limited output")
		}
		if !strings.Contains(result, "b/") {
			t.Error("expected 'b/' in depth-limited output")
		}
		// c/ is at depth 2, should not appear.
		if strings.Contains(result, "c/") {
			t.Error("did not expect 'c/' in depth-1 output")
		}
	})

	t.Run("AlphabeticalOrder", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"zebra.go",
			"alpha.go",
			"middle.go",
		}
		root := buildTree(files)
		result := renderTree(root, 3, 10000)

		idxA := strings.Index(result, "alpha.go")
		idxM := strings.Index(result, "middle.go")
		idxZ := strings.Index(result, "zebra.go")
		if idxA >= idxM || idxM >= idxZ {
			t.Errorf("expected alphabetical order, got a=%d m=%d z=%d", idxA, idxM, idxZ)
		}
	})

	t.Run("DeterministicOutput", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"z.go",
			"a.go",
			"m/x.go",
			"m/a.go",
		}
		result1 := renderTree(buildTree(files), 3, 10000)
		result2 := renderTree(buildTree(files), 3, 10000)
		if result1 != result2 {
			t.Error("renderTree produced non-deterministic output")
		}
	})

	t.Run("MaxBytesRespected", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"a/very_long_filename_1.go",
			"a/very_long_filename_2.go",
			"a/very_long_filename_3.go",
			"a/very_long_filename_4.go",
			"a/very_long_filename_5.go",
		}
		root := buildTree(files)
		result := renderTree(root, 3, 50)
		if len(result) > 50 {
			t.Errorf("expected output <= 50 bytes, got %d", len(result))
		}
	})

	t.Run("CollapseThreshold", func(t *testing.T) {
		t.Parallel()
		// Create a directory with more than collapseThreshold entries.
		var files []string
		for i := 0; i < collapseThreshold+5; i++ {
			files = append(files, fmt.Sprintf("bigdir/file_%03d.go", i))
		}
		root := buildTree(files)
		result := renderTree(root, 3, 10000)
		if !strings.Contains(result, "entries)") {
			t.Error("expected collapsed directory summary with 'entries)'")
		}
	})

	t.Run("DirectoriesBeforeFiles", func(t *testing.T) {
		t.Parallel()
		files := []string{
			"main.go",
			"cmd/root.go",
		}
		root := buildTree(files)
		result := renderTree(root, 3, 10000)

		idxCmd := strings.Index(result, "cmd/")
		idxMain := strings.Index(result, "main.go")
		if idxCmd >= idxMain {
			t.Error("expected directories before files in output")
		}
	})
}
