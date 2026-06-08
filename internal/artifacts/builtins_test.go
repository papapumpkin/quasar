package artifacts

import (
	"io/fs"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// frontmatterTOML extracts the TOML frontmatter from a star/skill Markdown file.
// It mirrors the nebula phase parser: the file must open with a +++ delimiter,
// and the frontmatter is everything up to the next +++. This is a deliberately
// small local copy — the real loader (Phase 02) owns the canonical splitter;
// this test only needs to prove the embedded defaults are well-formed enough to
// load.
func frontmatterTOML(t *testing.T, path, content string) string {
	t.Helper()
	const delim = "+++"
	if !strings.HasPrefix(content, delim) {
		t.Fatalf("%s: does not start with +++ frontmatter delimiter", path)
	}
	rest := content[len(delim):]
	end := strings.Index(rest, delim)
	if end < 0 {
		t.Fatalf("%s: missing closing +++ delimiter", path)
	}
	return rest[:end]
}

// TestEmbeddedDefaultsParse asserts that every artifact file embedded via
// DefaultsFS is structurally well-formed: constellation TOML decodes, and star
// and skill Markdown carries TOML frontmatter that decodes. It is the binary's
// guard against shipping a malformed default that would only fail at runtime.
//
// This validates structure, not semantics. Loader-level checks — skill
// resolution merging tools_add into a star's allowed list, expression
// compilation of edge `when` strings, DAG/terminal-node validation — live with
// the loader (Phase 02) and the runtime (Phase 05) and are exercised there.
func TestEmbeddedDefaultsParse(t *testing.T) {
	t.Parallel()

	var constellations, markdown int
	err := fs.WalkDir(DefaultsFS, "defaults", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, rerr := DefaultsFS.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}

		switch {
		case strings.HasSuffix(path, ".toml"):
			var doc map[string]any
			if uerr := toml.Unmarshal(data, &doc); uerr != nil {
				t.Errorf("constellation TOML %s: %v", path, uerr)
			}
			constellations++

		case strings.HasSuffix(path, ".md") && !strings.Contains(path, "/sensors/"):
			// stars/ and skills/ carry TOML frontmatter; sensors/README.md is
			// prose and is intentionally skipped.
			fm := frontmatterTOML(t, path, string(data))
			var doc map[string]any
			if uerr := toml.Unmarshal([]byte(fm), &doc); uerr != nil {
				t.Errorf("frontmatter %s: %v", path, uerr)
			}
			markdown++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded defaults: %v", err)
	}

	// Guard against an embed glob that silently matches nothing.
	if constellations != 6 {
		t.Errorf("expected 6 embedded constellation TOML files, parsed %d", constellations)
	}
	if markdown != 12 {
		t.Errorf("expected 12 embedded star+skill Markdown files, parsed %d", markdown)
	}
}
