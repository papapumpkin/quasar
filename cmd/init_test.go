package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates a file with the given content under
// dir, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunInitWith(t *testing.T) {
	t.Parallel()

	t.Run("empty directory writes all sections", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		if err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}

		content := readConfig(t, dir)
		for _, want := range []string{"claude_path:", "pre_commit:", "max_review_cycles:", "sensors/"} {
			if !strings.Contains(content, want) {
				t.Errorf("generated config missing %q\n%s", want, content)
			}
		}
		// No language or repo detected: verify is commented out, the deprecated
		// integrations block is never emitted, and no sensor is scaffolded.
		if strings.Contains(content, "\nverify:") {
			t.Errorf("expected [verify] commented out for empty dir, got:\n%s", content)
		}
		if strings.Contains(content, "integrations:") {
			t.Errorf("init must not emit the deprecated integrations block, got:\n%s", content)
		}
		if _, err := os.Stat(filepath.Join(dir, "sensors", "github.toml")); !os.IsNotExist(err) {
			t.Error("empty dir (no github remote) should not scaffold a sensor")
		}
	})

	t.Run("go.mod populates verify", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/x\n")

		if err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}

		content := readConfig(t, dir)
		for _, want := range []string{"verify:", `test: "go test ./..."`, `lint: "go vet ./..."`, `build: "go build ./..."`} {
			if !strings.Contains(content, want) {
				t.Errorf("generated config missing %q\n%s", want, content)
			}
		}
	})

	t.Run("https github remote populates repo", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "config"),
			"[remote \"origin\"]\n\turl = https://github.com/papapumpkin/quasar.git\n")

		if err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}

		// The detected repo is written to a scaffolded sensors/github.toml, not
		// the config (which carries no integrations/sensors key).
		if strings.Contains(readConfig(t, dir), "integrations:") {
			t.Error("init must not emit the deprecated integrations block")
		}
		sensor := readSensor(t, dir)
		for _, want := range []string{`type = "github_issues"`, `repo = "papapumpkin/quasar"`} {
			if !strings.Contains(sensor, want) {
				t.Errorf("scaffolded sensor missing %q\n%s", want, sensor)
			}
		}
	})

	t.Run("scp github remote populates repo", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "config"),
			"[remote \"origin\"]\n\turl = git@github.com:papapumpkin/quasar.git\n")

		if err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}
		if !strings.Contains(readSensor(t, dir), `repo = "papapumpkin/quasar"`) {
			t.Errorf("expected detected repo from scp url in the scaffolded sensor")
		}
	})

	t.Run("non-github remote scaffolds no sensor", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "config"),
			"[remote \"origin\"]\n\turl = https://gitlab.com/owner/repo.git\n")

		if err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}
		if strings.Contains(readConfig(t, dir), "integrations:") {
			t.Error("init must not emit the deprecated integrations block")
		}
		if _, err := os.Stat(filepath.Join(dir, "sensors", "github.toml")); !os.IsNotExist(err) {
			t.Error("a non-github remote should not scaffold a github sensor")
		}
	})

	t.Run("refuses overwrite without force", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, configFileName), "existing: true\n")

		err := runInitWith(dir, false, false, strings.NewReader(""), &bytes.Buffer{})
		if err == nil {
			t.Fatal("expected error overwriting without --force")
		}
		if !strings.Contains(err.Error(), "--force") {
			t.Errorf("error should mention --force, got: %v", err)
		}
		// Original content preserved.
		if readConfig(t, dir) != "existing: true\n" {
			t.Error("file was overwritten despite missing --force")
		}
	})

	t.Run("force with yes overwrites", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, configFileName), "existing: true\n")

		if err := runInitWith(dir, true, true, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}
		if strings.Contains(readConfig(t, dir), "existing: true") {
			t.Error("expected file to be overwritten with --force --yes")
		}
	})

	t.Run("force declined at prompt leaves file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, configFileName), "existing: true\n")

		out := &bytes.Buffer{}
		if err := runInitWith(dir, true, false, strings.NewReader("n\n"), out); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}
		if readConfig(t, dir) != "existing: true\n" {
			t.Error("declining the prompt should leave the file unchanged")
		}
	})

	t.Run("force confirmed at prompt overwrites", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, configFileName), "existing: true\n")

		if err := runInitWith(dir, true, false, strings.NewReader("y\n"), &bytes.Buffer{}); err != nil {
			t.Fatalf("runInitWith: %v", err)
		}
		if strings.Contains(readConfig(t, dir), "existing: true") {
			t.Error("confirming the prompt should overwrite the file")
		}
	})
}

// readConfig reads the generated .quasar.yaml from dir.
func readConfig(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	return string(data)
}

// readSensor reads the scaffolded sensors/github.toml from dir.
func readSensor(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "sensors", "github.toml"))
	if err != nil {
		t.Fatalf("read scaffolded sensor: %v", err)
	}
	return string(data)
}
