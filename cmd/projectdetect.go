package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

// verifyCommands holds the per-language verification gate commands written into
// [verify] by `quasar init` and reported by `quasar doctor`. An empty field
// means that gate is not applicable to the detected language.
type verifyCommands struct {
	Lang  string // human-facing language/toolchain label, "" if undetected
	Test  string
	Lint  string
	Build string
}

// languageMarkers maps a marker filename to its verify commands, scanned in a
// fixed priority order. The first marker found in the working directory wins so
// detection is deterministic in polyglot repos (Makefile is last as a fallback).
var languageMarkers = []struct {
	file string
	cmds verifyCommands
}{
	{"go.mod", verifyCommands{Lang: "Go", Test: "go test ./...", Lint: "go vet ./...", Build: "go build ./..."}},
	{"Cargo.toml", verifyCommands{Lang: "Rust", Test: "cargo test", Lint: "cargo clippy", Build: "cargo build"}},
	{"package.json", verifyCommands{Lang: "Node.js", Test: "npm test", Lint: "npm run lint", Build: "npm run build"}},
	{"pyproject.toml", verifyCommands{Lang: "Python", Test: "pytest", Lint: "ruff check .", Build: ""}},
	{"Gemfile", verifyCommands{Lang: "Ruby", Test: "bundle exec rspec", Lint: "rubocop", Build: ""}},
	{"mix.exs", verifyCommands{Lang: "Elixir", Test: "mix test", Lint: "mix credo", Build: "mix compile"}},
	{"pom.xml", verifyCommands{Lang: "Java (Maven)", Test: "mvn test", Lint: "", Build: "mvn package"}},
	{"build.gradle", verifyCommands{Lang: "Java (Gradle)", Test: "gradle test", Lint: "", Build: "gradle build"}},
	{"Makefile", verifyCommands{Lang: "Make", Test: "make test", Lint: "make lint", Build: "make build"}},
}

// detectVerifyCommands scans dir for known language markers and returns the
// verify commands for the first match. The returned Lang is "" when no marker
// is found, in which case the caller should leave [verify] commented out.
func detectVerifyCommands(dir string) verifyCommands {
	for _, m := range languageMarkers {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return m.cmds
		}
	}
	return verifyCommands{}
}

// detectGitHubRepo reads <dir>/.git/config and returns the "owner/repo" slug of
// the origin remote when it points at github.com. The boolean reports success.
// It deliberately reads .git/config rather than shelling out to git: Quasar's
// safety perimeter forbids invoking the git binary outside internal/gitops/, and
// detection only needs a read of a plain file.
func detectGitHubRepo(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return "", false
	}
	url, ok := parseGitConfigOrigin(string(data))
	if !ok {
		return "", false
	}
	return parseGitHubRemoteURL(url)
}

// detectOriginURL reads <dir>/.git/config and returns the raw origin remote URL,
// or "" if none is configured. Used by `quasar doctor` to display the remote
// without imposing the github.com host requirement detectGitHubRepo applies.
func detectOriginURL(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return ""
	}
	url, _ := parseGitConfigOrigin(string(data))
	return url
}

// findGitRoot walks up from dir looking for a .git entry (directory or file, the
// latter for worktrees/submodules). It returns the worktree root and true, or
// "" and false when dir is not inside a git repository.
func findGitRoot(dir string) (string, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// parseGitConfigOrigin extracts the origin remote URL from raw .git/config
// contents. It is a minimal INI scan, sufficient for the standard
// [remote "origin"] / url = … layout git writes. Mirrors the same helper in the
// github adapter; kept here so the cmd layer needs no adapter import (see the
// integrations arch test).
func parseGitConfigOrigin(content string) (string, bool) {
	inOrigin := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inOrigin = trimmed == `[remote "origin"]`
			continue
		}
		if inOrigin && strings.HasPrefix(trimmed, "url") {
			if i := strings.Index(trimmed, "="); i != -1 {
				return strings.TrimSpace(trimmed[i+1:]), true
			}
		}
	}
	return "", false
}

// parseGitHubRemoteURL extracts an "owner/repo" slug from a git remote URL when
// the host is github.com, accepting scp-like (git@github.com:owner/repo.git) and
// URL (https://github.com/owner/repo.git, ssh://…) forms. The boolean reports
// whether parsing succeeded and the host was github.com.
func parseGitHubRemoteURL(url string) (string, bool) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")

	if rest, ok := strings.CutPrefix(url, "git@"); ok {
		host, path, found := strings.Cut(rest, ":")
		if !found || host != "github.com" {
			return "", false
		}
		return cleanGitHubPath(path)
	}

	for _, scheme := range []string{"ssh://", "https://", "http://"} {
		rest, ok := strings.CutPrefix(url, scheme)
		if !ok {
			continue
		}
		// rest is "[user@]host/owner/repo".
		authority, path, found := strings.Cut(rest, "/")
		if !found {
			return "", false
		}
		// Drop any "user@" userinfo prefix before the host.
		if _, host, hasUser := strings.Cut(authority, "@"); hasUser {
			authority = host
		}
		if authority != "github.com" {
			return "", false
		}
		return cleanGitHubPath(path)
	}
	return "", false
}

// cleanGitHubPath reduces a path like "owner/repo" (possibly with extra leading
// segments) to its trailing owner/repo pair.
func cleanGitHubPath(p string) (string, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", false
	}
	return owner + "/" + repo, true
}
