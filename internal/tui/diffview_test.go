package tui

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/handler.go b/handler.go
index abc1234..def5678 100644
--- a/handler.go
+++ b/handler.go
@@ -10,7 +10,12 @@ func Login(w http.ResponseWriter) {
 	// validate input
-	token := generateToken()
+	token, err := generateToken()
+	if err != nil {
+		http.Error(w, "fail", 500)
+		return
+	}
 	w.Header().Set("Authorization", token)
diff --git a/auth.go b/auth.go
new file mode 100644
--- /dev/null
+++ b/auth.go
@@ -0,0 +1,4 @@
+package main
+
+func generateToken() (string, error) {
+	return "tok", nil
+}
`

func TestParseUnifiedDiff(t *testing.T) {
	t.Parallel()

	files := ParseUnifiedDiff(sampleDiff)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	t.Run("first file path", func(t *testing.T) {
		if files[0].Path != "handler.go" {
			t.Errorf("expected handler.go, got %s", files[0].Path)
		}
	})

	t.Run("second file path", func(t *testing.T) {
		if files[1].Path != "auth.go" {
			t.Errorf("expected auth.go, got %s", files[1].Path)
		}
	})

	t.Run("first file hunks", func(t *testing.T) {
		if len(files[0].Hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(files[0].Hunks))
		}
		hunk := files[0].Hunks[0]
		// Count line types.
		var adds, removes, context int
		for _, l := range hunk.Lines {
			switch l.Type {
			case DiffLineAdd:
				adds++
			case DiffLineRemove:
				removes++
			case DiffLineContext:
				context++
			}
		}
		if removes != 1 {
			t.Errorf("expected 1 remove, got %d", removes)
		}
		if adds != 5 {
			t.Errorf("expected 5 adds, got %d", adds)
		}
		if context != 2 {
			t.Errorf("expected 2 context, got %d", context)
		}
	})
}

func TestParseUnifiedDiff_empty(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff("")
	if files != nil {
		t.Errorf("expected nil for empty diff, got %v", files)
	}
}

func TestComputeDiffStat(t *testing.T) {
	t.Parallel()

	files := ParseUnifiedDiff(sampleDiff)
	stat := ComputeDiffStat(files)

	if stat.FilesChanged != 2 {
		t.Errorf("expected 2 files changed, got %d", stat.FilesChanged)
	}
	if stat.Insertions != 10 {
		t.Errorf("expected 10 insertions, got %d", stat.Insertions)
	}
	if stat.Deletions != 1 {
		t.Errorf("expected 1 deletion, got %d", stat.Deletions)
	}
	if len(stat.FileStats) != 2 {
		t.Fatalf("expected 2 file stats, got %d", len(stat.FileStats))
	}

	t.Run("handler.go stats", func(t *testing.T) {
		fs := stat.FileStats[0]
		if fs.Path != "handler.go" {
			t.Errorf("expected handler.go, got %s", fs.Path)
		}
		if fs.Additions != 5 {
			t.Errorf("expected 5 additions, got %d", fs.Additions)
		}
		if fs.Deletions != 1 {
			t.Errorf("expected 1 deletion, got %d", fs.Deletions)
		}
	})
}

func TestBuildSideBySidePairs(t *testing.T) {
	t.Parallel()

	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 || len(files[0].Hunks) == 0 {
		t.Fatal("expected parsed hunks")
	}

	pairs := BuildSideBySidePairs(files[0].Hunks[0])
	if len(pairs) == 0 {
		t.Fatal("expected non-empty pairs")
	}

	// First pair should be context (both sides present).
	if pairs[0].Left == nil || pairs[0].Right == nil {
		t.Error("expected context line to have both left and right")
	}
	if pairs[0].Left.Type != DiffLineContext {
		t.Errorf("expected context type, got %d", pairs[0].Left.Type)
	}

	// Find a pair where left is remove and right is add.
	var foundPair bool
	for _, p := range pairs {
		if p.Left != nil && p.Left.Type == DiffLineRemove &&
			p.Right != nil && p.Right.Type == DiffLineAdd {
			foundPair = true
			break
		}
	}
	if !foundPair {
		t.Error("expected at least one remove/add pair")
	}
}

func TestRenderDiffView(t *testing.T) {
	t.Parallel()

	result := RenderDiffView(sampleDiff, 120)
	if result == "" {
		t.Fatal("expected non-empty rendered diff")
	}

	// Should contain file headers.
	if !strings.Contains(result, "handler.go") {
		t.Error("expected handler.go in rendered output")
	}
	if !strings.Contains(result, "auth.go") {
		t.Error("expected auth.go in rendered output")
	}

	// Should contain stat summary.
	if !strings.Contains(result, "file") && !strings.Contains(result, "changed") {
		t.Error("expected stat summary in rendered output")
	}
}

func TestRenderDiffView_empty(t *testing.T) {
	t.Parallel()

	result := RenderDiffView("", 80)
	if !strings.Contains(result, "no diff available") {
		t.Errorf("expected 'no diff available', got %s", result)
	}
}

func TestPluralS(t *testing.T) {
	t.Parallel()

	if pluralS(1) != "" {
		t.Error("expected empty string for 1")
	}
	if pluralS(0) != "s" {
		t.Error("expected 's' for 0")
	}
	if pluralS(5) != "s" {
		t.Error("expected 's' for 5")
	}
}

func TestParseHunkHeader(t *testing.T) {
	t.Parallel()

	old, new := parseHunkHeader("@@ -10,7 +10,12 @@ func Login(w http.ResponseWriter) {")
	if old != 10 {
		t.Errorf("expected old start 10, got %d", old)
	}
	if new != 10 {
		t.Errorf("expected new start 10, got %d", new)
	}
}

func TestParseGitDiffPath(t *testing.T) {
	t.Parallel()

	path := parseGitDiffPath("diff --git a/internal/foo.go b/internal/foo.go")
	if path != "internal/foo.go" {
		t.Errorf("expected internal/foo.go, got %s", path)
	}
}
