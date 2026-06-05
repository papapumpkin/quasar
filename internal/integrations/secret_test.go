package integrations

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveSecret_EnvOnly(t *testing.T) {
	const key = "QUASAR_TEST_SECRET_ENV"
	t.Setenv(key, "env-value")

	got, err := ResolveSecret(SecretSpec{Env: key})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "env-value" {
		t.Errorf("got %q, want env-value", got)
	}
}

func TestResolveSecret_Neither(t *testing.T) {
	t.Parallel()

	got, err := ResolveSecret(SecretSpec{})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestResolveSecret_MissingEnv(t *testing.T) {
	t.Parallel()

	got, err := ResolveSecret(SecretSpec{Env: "QUASAR_DEFINITELY_UNSET_VAR_XYZ"})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for unset env", got)
	}
}

func TestResolveSecret_FileMode0600(t *testing.T) {
	path := writeSecretFile(t, "secret.txt", "file-token\n", 0o600)

	got, err := ResolveSecret(SecretSpec{File: path})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "file-token" {
		t.Errorf("got %q, want file-token (trailing newline trimmed)", got)
	}
}

func TestResolveSecret_FileMode0400(t *testing.T) {
	path := writeSecretFile(t, "ro.txt", "ro-token", 0o400)

	got, err := ResolveSecret(SecretSpec{File: path})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "ro-token" {
		t.Errorf("got %q, want ro-token", got)
	}
}

func TestResolveSecret_FileMode0644Rejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	path := writeSecretFile(t, "loose.txt", "loose-token", 0o644)

	_, err := ResolveSecret(SecretSpec{File: path})
	if err == nil {
		t.Fatal("expected SecretLooseError for mode 0644, got nil")
	}
	var loose *SecretLooseError
	if !errors.As(err, &loose) {
		t.Fatalf("error = %v, want *SecretLooseError", err)
	}
	if loose.Path != path {
		t.Errorf("SecretLooseError.Path = %q, want %q", loose.Path, path)
	}
}

func TestResolveSecret_FilePrecedesEnv(t *testing.T) {
	const key = "QUASAR_TEST_SECRET_PRECEDENCE"
	t.Setenv(key, "env-value")
	path := writeSecretFile(t, "precedence.txt", "file-value\n", 0o600)

	got, err := ResolveSecret(SecretSpec{Env: key, File: path})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "file-value" {
		t.Errorf("got %q, want file-value (file takes precedence)", got)
	}
}

func TestResolveSecret_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := ResolveSecret(SecretSpec{File: filepath.Join(t.TempDir(), "nope.txt")})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestResolveSecret_TrimsTrailingNewlines(t *testing.T) {
	path := writeSecretFile(t, "trim.txt", "token\r\n", 0o600)

	got, err := ResolveSecret(SecretSpec{File: path})
	if err != nil {
		t.Fatalf("ResolveSecret returned error: %v", err)
	}
	if got != "token" {
		t.Errorf("got %q, want token", got)
	}
}

// writeSecretFile creates a file in a temp dir with the given content and mode,
// returning its absolute path. chmod is applied explicitly because the umask
// may strip bits at creation time.
func writeSecretFile(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod secret file: %v", err)
	}
	return path
}
