package artifacts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSource(t *testing.T) {
	t.Run("embedded path resolves through the defaults FS", func(t *testing.T) {
		// This is the path shape stored on a loaded embedded artifact's SourcePath.
		data, err := ReadSource(EmbeddedPath + "defaults/constellations/architect.toml")
		if err != nil {
			t.Fatalf("ReadSource embedded: %v", err)
		}
		if !bytes.Contains(data, []byte("architect")) {
			t.Errorf("embedded architect.toml missing expected content:\n%s", data)
		}
	})

	t.Run("missing embedded file errors", func(t *testing.T) {
		if _, err := ReadSource(EmbeddedPath + "defaults/does-not-exist.toml"); err == nil {
			t.Error("expected an error for a missing embedded file")
		}
	})

	t.Run("non-embedded path falls through to the filesystem", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "star.md")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		data, err := ReadSource(path)
		if err != nil {
			t.Fatalf("ReadSource disk: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("got %q, want %q", data, "hello")
		}
	})
}
