package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// linkRe matches Markdown inline links: [label](target).
var linkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// docsDir returns the absolute path to this docs/ directory.
func docsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(thisFile)
}

// TestInternalLinksResolve verifies that every relative Markdown link in the
// docs/ tree points at a file that exists. External links (http, https, mailto)
// and pure anchors (#section) are skipped; an anchor suffix on a file link is
// stripped before the existence check.
func TestInternalLinksResolve(t *testing.T) {
	t.Parallel()

	dir := docsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading docs dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		mdPath := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(mdPath)
		if err != nil {
			t.Fatalf("reading %s: %v", mdPath, err)
		}
		for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
			checkLink(t, e.Name(), m[1])
		}
	}
}

// checkLink validates a single link target found in the named markdown file.
func checkLink(t *testing.T, mdFile, target string) {
	t.Helper()

	if isExternalOrAnchor(target) {
		return
	}
	// Strip any anchor suffix: safety.md#token-scopes -> safety.md.
	if i := strings.IndexByte(target, '#'); i != -1 {
		target = target[:i]
	}
	if target == "" {
		return
	}
	abs := filepath.Join(docsDir(t), target)
	if _, err := os.Stat(abs); err != nil {
		t.Errorf("%s: broken link %q (resolved to %s): %v", mdFile, target, abs, err)
	}
}

// isExternalOrAnchor reports whether target is an external URL or a pure anchor
// and should not be resolved as a file path.
func isExternalOrAnchor(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "#")
}
