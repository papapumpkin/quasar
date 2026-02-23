package context

import (
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

func TestCountEntries(t *testing.T) {
	t.Parallel()

	paths := []string{
		"a/b/c.go",
		"a/b/d.go",
		"a/e.go",
		"f.go",
	}
	root := buildTree(paths)

	// Unlimited depth: a, a/b, a/b/c.go, a/b/d.go, a/e.go, f.go = 6
	total := countEntries(root, 0, -1)
	if total != 6 {
		t.Errorf("expected 6 entries, got %d", total)
	}

	// Depth 0: a, f.go = 2
	shallow := countEntries(root, 0, 0)
	if shallow != 2 {
		t.Errorf("expected 2 entries at depth 0, got %d", shallow)
	}
}
