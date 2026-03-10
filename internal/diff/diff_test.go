package diff

import "testing"

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
