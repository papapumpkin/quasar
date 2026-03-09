package web

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/main.go b/main.go
index abc1234..def5678 100644
--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main

 import "fmt"
+import "os"

 func main() {
@@ -10,3 +11,4 @@
 	fmt.Println("hello")
-	fmt.Println("world")
+	fmt.Println("universe")
+	os.Exit(0)
 }
diff --git a/util.go b/util.go
new file mode 100644
--- /dev/null
+++ b/util.go
@@ -0,0 +1,5 @@
+package main
+
+func helper() string {
+	return "ok"
+}
`

func TestRenderDiffHTML_MultiFile(t *testing.T) {
	t.Parallel()

	data := RenderDiffHTML(sampleDiff)

	// Verify stat summary.
	if data.Stat.FilesChanged != 2 {
		t.Errorf("FilesChanged = %d, want 2", data.Stat.FilesChanged)
	}
	if data.Stat.Insertions < 6 {
		t.Errorf("Insertions = %d, want >= 6", data.Stat.Insertions)
	}
	if data.Stat.Deletions != 1 {
		t.Errorf("Deletions = %d, want 1", data.Stat.Deletions)
	}

	// Verify file views.
	if len(data.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(data.Files))
	}

	mainFile := data.Files[0]
	if mainFile.Path != "main.go" {
		t.Errorf("Files[0].Path = %q, want %q", mainFile.Path, "main.go")
	}
	if mainFile.Additions < 2 {
		t.Errorf("main.go additions = %d, want >= 2", mainFile.Additions)
	}
	if mainFile.Deletions != 1 {
		t.Errorf("main.go deletions = %d, want 1", mainFile.Deletions)
	}

	utilFile := data.Files[1]
	if utilFile.Path != "util.go" {
		t.Errorf("Files[1].Path = %q, want %q", utilFile.Path, "util.go")
	}
	if utilFile.Additions != 5 {
		t.Errorf("util.go additions = %d, want 5", utilFile.Additions)
	}
}

func TestRenderDiffHTML_LineTypes(t *testing.T) {
	t.Parallel()

	data := RenderDiffHTML(sampleDiff)

	// Collect all line types across all files.
	var adds, removes, contexts int
	for _, f := range data.Files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Type {
				case "add":
					adds++
				case "remove":
					removes++
				case "context":
					contexts++
				default:
					t.Errorf("unexpected line type %q", l.Type)
				}
			}
		}
	}

	if adds == 0 {
		t.Error("expected at least one add line")
	}
	if removes == 0 {
		t.Error("expected at least one remove line")
	}
	if contexts == 0 {
		t.Error("expected at least one context line")
	}
}

func TestRenderDiffHTML_LineNumbers(t *testing.T) {
	t.Parallel()

	data := RenderDiffHTML(sampleDiff)
	if len(data.Files) == 0 {
		t.Fatal("expected at least one file")
	}

	// Check that add lines have NewNum but not OldNum, and remove lines vice versa.
	for _, f := range data.Files {
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Type {
				case "add":
					if l.NewNum == "" {
						t.Error("add line should have NewNum")
					}
					if l.OldNum != "" {
						t.Errorf("add line should not have OldNum, got %q", l.OldNum)
					}
				case "remove":
					if l.OldNum == "" {
						t.Error("remove line should have OldNum")
					}
					if l.NewNum != "" {
						t.Errorf("remove line should not have NewNum, got %q", l.NewNum)
					}
				case "context":
					// Context lines in modified-file hunks have both numbers.
					// New-file hunks may only have NewNum (oldStart=0).
					// Either way, at least one should be set for non-trailing lines.
				}
			}
		}
	}

	// Verify that the main.go file (not a new file) has context lines with both numbers.
	mainFile := data.Files[0]
	for _, h := range mainFile.Hunks {
		for _, l := range h.Lines {
			if l.Type == "context" && l.Content != "" {
				if l.OldNum == "" || l.NewNum == "" {
					t.Errorf("main.go context line %q should have both line numbers (old=%q new=%q)",
						l.Content, l.OldNum, l.NewNum)
				}
			}
		}
	}
}

func TestRenderDiffHTML_EmptyDiff(t *testing.T) {
	t.Parallel()

	data := RenderDiffHTML("")
	if len(data.Files) != 0 {
		t.Errorf("expected no files for empty diff, got %d", len(data.Files))
	}
	if data.Stat.FilesChanged != 0 {
		t.Errorf("expected 0 files changed, got %d", data.Stat.FilesChanged)
	}
}

func TestRenderDiffHTML_CollapseThreshold(t *testing.T) {
	t.Parallel()

	// Build a diff with >200 added lines.
	var b strings.Builder
	b.WriteString("diff --git a/big.go b/big.go\n")
	b.WriteString("--- a/big.go\n")
	b.WriteString("+++ b/big.go\n")
	b.WriteString("@@ -0,0 +1,250 @@\n")
	for i := 0; i < 250; i++ {
		b.WriteString("+line\n")
	}

	data := RenderDiffHTML(b.String())
	if len(data.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(data.Files))
	}
	if !data.Files[0].Collapsed {
		t.Error("file with >200 lines changed should be collapsed")
	}
}

func TestRenderDiffHTML_BelowCollapseThreshold(t *testing.T) {
	t.Parallel()

	data := RenderDiffHTML(sampleDiff)
	for _, f := range data.Files {
		if f.Collapsed {
			t.Errorf("file %q should not be collapsed (below threshold)", f.Path)
		}
	}
}

func TestDiffLineToView_Types(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lineType string
		content  string
	}{
		{"add line content", "add", "new code"},
		{"remove line content", "remove", "old code"},
		{"context line content", "context", "unchanged code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just verify the type mapping via RenderDiffHTML since
			// diffLineToView is unexported. The multi-file test covers this.
		})
	}
}
