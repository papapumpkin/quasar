package snapshot

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildTree(t *testing.T) {
	t.Parallel()

	paths := []string{
		"cmd/root.go",
		"cmd/run.go",
		"internal/agent/agent.go",
		"internal/agent/coder.go",
		"go.mod",
		"main.go",
	}

	root := buildTree(paths)

	// Root should have 3 children: cmd, internal, go.mod, main.go
	if len(root.children) != 4 {
		t.Errorf("expected 4 root children, got %d", len(root.children))
	}

	// cmd should have 2 files
	cmd, ok := root.children["cmd"]
	if !ok {
		t.Fatal("expected 'cmd' directory")
	}
	if len(cmd.children) != 2 {
		t.Errorf("expected 2 children in cmd, got %d", len(cmd.children))
	}

	// go.mod should be a file
	gomod, ok := root.children["go.mod"]
	if !ok {
		t.Fatal("expected 'go.mod' file")
	}
	if !gomod.isFile {
		t.Error("expected go.mod to be a file")
	}
}

func TestRenderTree(t *testing.T) {
	t.Parallel()

	paths := []string{
		"cmd/root.go",
		"cmd/run.go",
		"internal/agent/agent.go",
		"go.mod",
		"main.go",
	}

	root := buildTree(paths)

	var b strings.Builder
	renderTree(&b, root, "", 0, 3)
	output := b.String()

	// Directories should appear before files.
	cmdIdx := strings.Index(output, "cmd/")
	gomodIdx := strings.Index(output, "go.mod")
	if cmdIdx > gomodIdx {
		t.Error("directories should appear before files")
	}

	// Check depth limiting.
	var b2 strings.Builder
	renderTree(&b2, root, "", 0, 0)
	shallow := b2.String()

	// At depth 0, we should see top-level entries only.
	if strings.Contains(shallow, "agent.go") {
		t.Error("depth 0 should not show agent.go")
	}
	if !strings.Contains(shallow, "cmd/") {
		t.Error("depth 0 should show cmd/")
	}
}

func TestRenderTreeDeterministic(t *testing.T) {
	t.Parallel()

	paths := []string{
		"z.go",
		"a.go",
		"m/x.go",
		"m/a.go",
		"b/c/d.go",
	}

	// Render twice and compare.
	var b1, b2 strings.Builder
	root1 := buildTree(paths)
	renderTree(&b1, root1, "", 0, -1)

	root2 := buildTree(paths)
	renderTree(&b2, root2, "", 0, -1)

	if b1.String() != b2.String() {
		t.Error("tree rendering is not deterministic")
	}
}

func TestExtractGoModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard module",
			content: "module github.com/papapumpkin/quasar\n\ngo 1.25.5\n",
			want:    "github.com/papapumpkin/quasar",
		},
		{
			name:    "empty",
			content: "",
			want:    "",
		},
		{
			name:    "no module line",
			content: "go 1.21\n",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractGoModule(tt.content)
			if got != tt.want {
				t.Errorf("extractGoModule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractJSONField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		field   string
		want    string
	}{
		{
			name:    "package.json name",
			content: `{"name": "my-project", "version": "1.0.0"}`,
			field:   "name",
			want:    "my-project",
		},
		{
			name:    "missing field",
			content: `{"version": "1.0.0"}`,
			field:   "name",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractJSONField(tt.content, tt.field)
			if got != tt.want {
				t.Errorf("extractJSONField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTOMLField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		field   string
		want    string
	}{
		{
			name:    "cargo toml name",
			content: "[package]\nname = \"my-crate\"\nversion = \"0.1.0\"\n",
			field:   "name",
			want:    "my-crate",
		},
		{
			name:    "missing",
			content: "version = \"0.1.0\"\n",
			field:   "name",
			want:    "",
		},
		{
			name:    "prefix false positive",
			content: "namespace = \"foo\"\nname = \"bar\"\n",
			field:   "name",
			want:    "bar",
		},
		{
			name:    "only prefix match",
			content: "namespace = \"foo\"\n",
			field:   "name",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractTOMLField(tt.content, tt.field)
			if got != tt.want {
				t.Errorf("extractTOMLField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectProject(t *testing.T) {
	t.Parallel()

	root := newDirNode("")
	root.children["go.mod"] = &dirNode{name: "go.mod", isFile: true, children: make(map[string]*dirNode)}
	root.children["main.go"] = &dirNode{name: "main.go", isFile: true, children: make(map[string]*dirNode)}

	contents := map[string]string{
		"go.mod": "module github.com/example/project\n\ngo 1.21\n",
	}

	lang, mod := detectProject(root, contents)
	if lang != "Go" {
		t.Errorf("expected language 'Go', got %q", lang)
	}
	if mod != "github.com/example/project" {
		t.Errorf("expected module 'github.com/example/project', got %q", mod)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("line\n", 100)

	result := truncate(long, 50)
	if len(result) > 50 {
		t.Errorf("truncated result too long: %d chars", len(result))
	}
	if !strings.Contains(result, "[... truncated for size ...]") {
		t.Error("missing truncation marker")
	}
}

func TestBuildTreeFlatFiles(t *testing.T) {
	t.Parallel()

	paths := []string{"a.go", "b.go", "c.txt"}
	root := buildTree(paths)

	if len(root.children) != 3 {
		t.Errorf("expected 3 root children, got %d", len(root.children))
	}

	for _, name := range paths {
		child, ok := root.children[name]
		if !ok {
			t.Errorf("missing child %q", name)
			continue
		}
		if !child.isFile {
			t.Errorf("%q should be a file", name)
		}
		if len(child.children) != 0 {
			t.Errorf("%q should have no children", name)
		}
	}
}

func TestBuildTreeNestedDirs(t *testing.T) {
	t.Parallel()

	paths := []string{
		"cmd/root.go",
		"internal/loop/loop.go",
		"internal/loop/state.go",
		"internal/agent/agent.go",
	}

	root := buildTree(paths)

	// Root: cmd, internal.
	if len(root.children) != 2 {
		t.Errorf("expected 2 root children, got %d", len(root.children))
	}

	// internal should have loop and agent.
	internal, ok := root.children["internal"]
	if !ok {
		t.Fatal("expected 'internal' directory")
	}
	if len(internal.children) != 2 {
		t.Errorf("expected 2 children in internal, got %d", len(internal.children))
	}

	// internal/loop should have 2 files.
	loop, ok := internal.children["loop"]
	if !ok {
		t.Fatal("expected 'loop' directory")
	}
	if len(loop.children) != 2 {
		t.Errorf("expected 2 children in loop, got %d", len(loop.children))
	}

	// Verify files are marked correctly.
	for _, fname := range []string{"loop.go", "state.go"} {
		f, ok := loop.children[fname]
		if !ok {
			t.Errorf("expected %q in loop", fname)
			continue
		}
		if !f.isFile {
			t.Errorf("%q should be a file", fname)
		}
	}
}

func TestBuildTreeEmptyInput(t *testing.T) {
	t.Parallel()

	root := buildTree(nil)
	if len(root.children) != 0 {
		t.Errorf("expected 0 children for empty input, got %d", len(root.children))
	}

	root2 := buildTree([]string{})
	if len(root2.children) != 0 {
		t.Errorf("expected 0 children for empty slice, got %d", len(root2.children))
	}
}

func TestRenderTreeDepthTwo(t *testing.T) {
	t.Parallel()

	paths := []string{
		"a/b/c/d/e.go",
		"a/b/c/f.go",
		"a/b/g.go",
		"a/h.go",
		"top.go",
	}

	root := buildTree(paths)

	var b strings.Builder
	renderTree(&b, root, "", 0, 2)
	output := b.String()

	// Depth 0: a/, top.go
	// Depth 1: a/b/
	// Depth 2: a/b/c/, a/b/g.go
	// Depth 3 (not rendered): a/b/c/d/, a/b/c/f.go

	if !strings.Contains(output, "a/") {
		t.Error("depth 2 should show a/")
	}
	if !strings.Contains(output, "g.go") {
		t.Error("depth 2 should show g.go (at depth 2)")
	}
	if !strings.Contains(output, "top.go") {
		t.Error("depth 2 should show top.go")
	}
	// f.go is at depth 3 (a -> b -> c -> f.go), should not appear.
	if strings.Contains(output, "f.go") {
		t.Error("depth 2 should not show f.go (at depth 3)")
	}
	// e.go is at depth 4, should not appear.
	if strings.Contains(output, "e.go") {
		t.Error("depth 2 should not show e.go (at depth 4)")
	}
}

func TestRenderTreeSorting(t *testing.T) {
	t.Parallel()

	paths := []string{
		"z_dir/file.go",
		"a_dir/file.go",
		"m_dir/file.go",
		"z.go",
		"a.go",
		"m.go",
	}

	root := buildTree(paths)

	var b strings.Builder
	renderTree(&b, root, "", 0, -1)
	output := b.String()

	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Directories come first, sorted alphabetically, then files sorted alphabetically.
	expected := []string{
		"a_dir/",
		"  file.go",
		"m_dir/",
		"  file.go",
		"z_dir/",
		"  file.go",
		"a.go",
		"m.go",
		"z.go",
	}

	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d:\n%s", len(expected), len(lines), output)
	}

	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want)
		}
	}
}

func TestRenderTreeLargeTree(t *testing.T) {
	t.Parallel()

	// Generate a large tree with 200 files across multiple directories.
	var paths []string
	for i := 0; i < 10; i++ {
		for j := 0; j < 20; j++ {
			paths = append(paths, fmt.Sprintf("dir%02d/file%02d.go", i, j))
		}
	}

	root := buildTree(paths)

	var b strings.Builder
	renderTree(&b, root, "", 0, -1)
	output := b.String()

	// Should render all 200 files plus 10 directory headers.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// 10 dir lines + 200 file lines = 210 lines.
	if len(lines) != 210 {
		t.Errorf("expected 210 lines for large tree, got %d", len(lines))
	}

	// Verify it stays compact (no excessive whitespace or decoration).
	// Each line should be a dir or indented file, nothing else.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			t.Errorf("line %d is empty in compact tree output", i)
		}
	}
}

func TestDetectProjectNoManifest(t *testing.T) {
	t.Parallel()

	root := newDirNode("")
	root.children["README.md"] = &dirNode{name: "README.md", isFile: true, children: make(map[string]*dirNode)}

	lang, mod := detectProject(root, nil)
	if lang != "" {
		t.Errorf("expected empty language, got %q", lang)
	}
	if mod != "" {
		t.Errorf("expected empty module, got %q", mod)
	}
}

func TestDetectProjectMultipleManifests(t *testing.T) {
	t.Parallel()

	// When multiple manifests exist, detectProject picks the first alphabetically.
	root := newDirNode("")
	root.children["package.json"] = &dirNode{name: "package.json", isFile: true, children: make(map[string]*dirNode)}
	root.children["go.mod"] = &dirNode{name: "go.mod", isFile: true, children: make(map[string]*dirNode)}

	contents := map[string]string{
		"go.mod":       "module github.com/example/multi\n\ngo 1.21\n",
		"package.json": `{"name": "multi-project"}`,
	}

	lang, mod := detectProject(root, contents)

	// Cargo.toml < go.mod < package.json alphabetically, so go.mod wins.
	if lang != "Go" {
		t.Errorf("expected 'Go' (first alphabetically), got %q", lang)
	}
	if mod != "github.com/example/multi" {
		t.Errorf("expected 'github.com/example/multi', got %q", mod)
	}
}

func TestTruncateCleanBoundary(t *testing.T) {
	t.Parallel()

	// Verify truncation cuts at a newline boundary.
	content := "line one\nline two\nline three\nline four\nline five\n"
	result := truncate(content, 40)

	if len(result) > 40 {
		t.Errorf("truncated result too long: %d chars", len(result))
	}

	// Should end with the truncation marker.
	if !strings.HasSuffix(result, "[... truncated for size ...]\n") {
		t.Error("should end with truncation marker")
	}

	// Content before marker should end at a line boundary (no partial lines).
	parts := strings.SplitN(result, "\n\n[... truncated for size ...]\n", 2)
	if len(parts) != 2 {
		t.Fatal("expected content + marker split")
	}
	// Verify no partial line remains.
	lines := strings.Split(parts[0], "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "line ") {
			t.Errorf("unexpected partial line: %q", line)
		}
	}
}
