package artifacts

import (
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	t.Parallel()

	t.Run("extracts frontmatter and body", func(t *testing.T) {
		t.Parallel()
		fm, body, line, err := splitFrontmatter("+++\nname = \"x\"\n+++\n\nHello body.\n")
		if err != nil {
			t.Fatalf("splitFrontmatter: %v", err)
		}
		if !strings.Contains(fm, "name = \"x\"") {
			t.Errorf("frontmatter = %q", fm)
		}
		if strings.TrimSpace(body) != "Hello body." {
			t.Errorf("body = %q", body)
		}
		if line != 2 {
			t.Errorf("fmStartLine = %d, want 2", line)
		}
	})

	t.Run("counts leading blank lines into the start line", func(t *testing.T) {
		t.Parallel()
		_, _, line, err := splitFrontmatter("\n\n+++\nk = 1\n+++\nbody")
		if err != nil {
			t.Fatalf("splitFrontmatter: %v", err)
		}
		if line != 4 {
			t.Errorf("fmStartLine = %d, want 4", line)
		}
	})

	t.Run("missing opening delimiter errors", func(t *testing.T) {
		t.Parallel()
		if _, _, _, err := splitFrontmatter("no frontmatter here"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing closing delimiter errors", func(t *testing.T) {
		t.Parallel()
		if _, _, _, err := splitFrontmatter("+++\nk = 1\nstill going"); err == nil {
			t.Fatal("expected error")
		}
	})
}
