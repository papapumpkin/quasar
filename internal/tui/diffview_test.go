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

func TestParseHunkContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "function context",
			line: "@@ -10,7 +10,12 @@ func Login(w http.ResponseWriter) {",
			want: "func Login(w http.ResponseWriter) {",
		},
		{
			name: "method context",
			line: "@@ -5,3 +5,8 @@ func (l *Loop) Run(ctx context.Context) error {",
			want: "func (l *Loop) Run(ctx context.Context) error {",
		},
		{
			name: "no context",
			line: "@@ -1,4 +1,6 @@",
			want: "",
		},
		{
			name: "empty context after @@",
			line: "@@ -1,4 +1,6 @@ ",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseHunkContext(tt.line)
			if got != tt.want {
				t.Errorf("parseHunkContext(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseUnifiedDiff_HunkHeader(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 || len(files[0].Hunks) == 0 {
		t.Fatal("expected parsed hunks")
	}
	hunk := files[0].Hunks[0]
	if hunk.Header != "func Login(w http.ResponseWriter) {" {
		t.Errorf("expected hunk header 'func Login(w http.ResponseWriter) {', got %q", hunk.Header)
	}
}

func TestParseUnifiedDiff_ChangeType(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	// handler.go is modified (no special header).
	if files[0].Change != ChangeModified {
		t.Errorf("expected handler.go to be ChangeModified, got %d", files[0].Change)
	}
	// auth.go has "new file mode".
	if files[1].Change != ChangeAdded {
		t.Errorf("expected auth.go to be ChangeAdded, got %d", files[1].Change)
	}
}

func TestParseUnifiedDiff_DeletedFile(t *testing.T) {
	t.Parallel()
	raw := `diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package main
-
-func old() {}
`
	files := ParseUnifiedDiff(raw)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Change != ChangeDeleted {
		t.Errorf("expected ChangeDeleted, got %d", files[0].Change)
	}
}

func TestParseUnifiedDiff_RenamedFile(t *testing.T) {
	t.Parallel()
	raw := `diff --git a/old.go b/new.go
similarity index 95%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1,3 +1,3 @@
 package main
-func old() {}
+func renamed() {}
`
	files := ParseUnifiedDiff(raw)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Change != ChangeRenamed {
		t.Errorf("expected ChangeRenamed, got %d", files[0].Change)
	}
}

func TestChangeTypeGlyph(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ct   ChangeType
		want string
	}{
		{ChangeModified, "M"},
		{ChangeAdded, "A"},
		{ChangeDeleted, "D"},
		{ChangeRenamed, "R"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := ChangeTypeGlyph(tt.ct); got != tt.want {
				t.Errorf("ChangeTypeGlyph(%d) = %q, want %q", tt.ct, got, tt.want)
			}
		})
	}
}

func TestRenderStatBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		adds     int
		dels     int
		width    int
		wantPlus bool
		wantDash bool
	}{
		{"adds only", 5, 0, 10, true, false},
		{"dels only", 0, 3, 10, false, true},
		{"mixed", 5, 3, 8, true, true},
		{"zero total", 0, 0, 10, false, false},
		{"zero width", 5, 3, 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderStatBar(tt.adds, tt.dels, tt.width)
			if tt.wantPlus && !strings.Contains(got, "+") {
				t.Error("expected '+' in stat bar")
			}
			if tt.wantDash && !strings.Contains(got, "-") {
				t.Error("expected '-' in stat bar")
			}
			if !tt.wantPlus && !tt.wantDash && got != "" {
				t.Errorf("expected empty bar for zero counts, got %q", got)
			}
		})
	}
}

func TestRenderFileDiffWithOpts_Collapsed(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 {
		t.Fatal("expected parsed files")
	}
	opts := DiffRenderOpts{
		SideBySide:     false,
		CollapsedHunks: map[int]bool{0: true},
	}
	result := renderFileDiffWithOpts(files[0], 120, opts)
	// Collapsed hunk should show summary with ▸.
	if !strings.Contains(result, "▸") {
		t.Error("expected collapse indicator ▸ in output")
	}
}

func TestRenderFileDiffWithOpts_Unified(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 {
		t.Fatal("expected parsed files")
	}
	opts := DiffRenderOpts{SideBySide: false}
	result := renderFileDiffWithOpts(files[0], 120, opts)
	// Unified mode should not contain the side-by-side separator.
	if strings.Contains(result, " │ ") {
		t.Error("unified mode should not contain side-by-side separator")
	}
}

func TestRenderFileDiffWithOpts_SideBySide(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 {
		t.Fatal("expected parsed files")
	}
	opts := DiffRenderOpts{SideBySide: true}
	result := renderFileDiffWithOpts(files[0], 120, opts)
	if !strings.Contains(result, "handler.go") {
		t.Error("expected file path in output")
	}
}

func TestRenderSingleFileDiffWithOpts(t *testing.T) {
	t.Parallel()
	opts := DiffRenderOpts{SideBySide: false}
	result := RenderSingleFileDiffWithOpts(sampleDiff, "handler.go", 120, opts)
	if !strings.Contains(result, "handler.go") {
		t.Error("expected handler.go in rendered output")
	}
}

func TestRenderSingleFileDiffWithOpts_NotFound(t *testing.T) {
	t.Parallel()
	opts := DiffRenderOpts{}
	result := RenderSingleFileDiffWithOpts(sampleDiff, "nonexistent.go", 120, opts)
	if !strings.Contains(result, "no diff for") {
		t.Error("expected 'no diff for' message")
	}
}

func TestHunkCount(t *testing.T) {
	t.Parallel()
	count := HunkCount(sampleDiff, "handler.go")
	if count != 1 {
		t.Errorf("expected 1 hunk for handler.go, got %d", count)
	}
	count = HunkCount(sampleDiff, "nonexistent.go")
	if count != 0 {
		t.Errorf("expected 0 hunks for nonexistent file, got %d", count)
	}
}

func TestComputeDiffStat_ChangeType(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	stat := ComputeDiffStat(files)
	if stat.FileStats[0].Change != ChangeModified {
		t.Errorf("expected handler.go stat Change=ChangeModified, got %d", stat.FileStats[0].Change)
	}
	if stat.FileStats[1].Change != ChangeAdded {
		t.Errorf("expected auth.go stat Change=ChangeAdded, got %d", stat.FileStats[1].Change)
	}
}

func TestRenderFileDiffWithOpts_HunkContext(t *testing.T) {
	t.Parallel()
	files := ParseUnifiedDiff(sampleDiff)
	if len(files) == 0 {
		t.Fatal("expected parsed files")
	}
	// Both unified and side-by-side should show hunk context.
	for _, sbs := range []bool{true, false} {
		opts := DiffRenderOpts{SideBySide: sbs}
		result := renderFileDiffWithOpts(files[0], 120, opts)
		if !strings.Contains(result, "func Login") {
			t.Errorf("SideBySide=%v: expected hunk context 'func Login' in output", sbs)
		}
	}
}
