package integrations

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// allowedSecretModes are the only file permission bits accepted for a secret
// file on Unix. Anything looser (group/other readable) is rejected so a
// misconfigured container surfaces the problem immediately.
var allowedSecretModes = map[os.FileMode]bool{
	0o600: true,
	0o400: true,
}

// SecretSpec describes how a secret should be loaded. Both fields are
// optional; an adapter that needs a secret should call ResolveSecret and
// decide whether an empty result is an error or a fallback (e.g. GitHub can
// fall back to `gh auth token`).
type SecretSpec struct {
	Env  string // environment variable name, e.g. "GITHUB_TOKEN"
	File string // filesystem path, e.g. "/run/secrets/github_token"
}

// SecretLooseError reports that a secret file has permissions looser than
// 0600/0400. It names the offending path so the operator can fix the mount.
type SecretLooseError struct {
	Path string
	Mode os.FileMode
}

// Error implements the error interface.
func (e *SecretLooseError) Error() string {
	return fmt.Sprintf("secret file %q has insecure permissions %#o; require 0600 or 0400", e.Path, e.Mode.Perm())
}

// SecretResolver resolves a SecretSpec to its concrete value. It is an
// interface so tests can inject a fake without touching the filesystem.
type SecretResolver interface {
	// Resolve returns the secret value for the given spec, or an error if a
	// configured source could not be read securely.
	Resolve(spec SecretSpec) (string, error)
}

// OSSecretResolver is the production SecretResolver. It reads from the real
// environment and filesystem via ResolveSecret.
type OSSecretResolver struct{}

// Resolve implements SecretResolver by delegating to ResolveSecret.
func (OSSecretResolver) Resolve(spec SecretSpec) (string, error) {
	return ResolveSecret(spec)
}

// ResolveSecret returns the secret value with this precedence:
//
//  1. File (takes precedence — supports Docker --secret mounts)
//  2. Env (12-factor pattern)
//  3. Empty string
//
// File reads are restricted: on Unix the file MUST have mode 0600 or 0400;
// looser permissions yield a *SecretLooseError so misconfigured containers
// surface the issue immediately. Returned secrets are trimmed of trailing
// newlines (common in Docker secret files).
func ResolveSecret(spec SecretSpec) (string, error) {
	if spec.File != "" {
		return readSecretFile(spec.File)
	}
	if spec.Env != "" {
		if v, ok := os.LookupEnv(spec.Env); ok {
			return v, nil
		}
	}
	return "", nil
}

// readSecretFile stats the file for safe permissions, then reads and trims it.
func readSecretFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", path, err)
	}

	// File mode bits are a Unix concept; skip the check on Windows where the
	// permission model differs and Stat does not report meaningful bits.
	if runtime.GOOS != "windows" {
		perm := info.Mode().Perm()
		if !allowedSecretModes[perm] {
			return "", &SecretLooseError{Path: path, Mode: info.Mode()}
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}
