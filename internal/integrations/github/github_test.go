package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/integrations"
)

// captureSecrets is a SecretResolver that records the spec it was asked to
// resolve and returns a canned value/error.
type captureSecrets struct {
	gotSpec integrations.SecretSpec
	value   string
	err     error
}

func (c *captureSecrets) Resolve(spec integrations.SecretSpec) (string, error) {
	c.gotSpec = spec
	return c.value, c.err
}

// fakeGH builds a runGHFunc that dispatches on the --json field requested,
// returning the matching fixture bytes. A non-nil failOn error is returned for
// the metadata call to simulate gh failures.
func fakeGH(meta, comments, timeline []byte, failOn error) runGHFunc {
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch jsonField(args) {
		case "comments":
			return comments, nil
		case "timelineItems":
			return timeline, nil
		default: // metadata call (number,title,...)
			if failOn != nil {
				return nil, failOn
			}
			return meta, nil
		}
	}
}

// jsonField returns the value passed after the "--json" flag, or "".
func jsonField(args []string) string {
	for i, a := range args {
		if a == "--json" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// testDeps returns sourceDeps that never touch the real environment: gh is
// "found", repo auto-detection fails, and runGH is the provided fake.
func testDeps(run runGHFunc) sourceDeps {
	return sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "", errors.New("no git") },
		newRunGH:   func(string) runGHFunc { return run },
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestFetchHappyPath(t *testing.T) {
	t.Parallel()

	meta := readFixture(t, "gh-issue-42.json")
	comments := readFixture(t, "gh-issue-comments.json")
	timeline := []byte(`{"timelineItems":[{"__typename":"CrossReferencedEvent","source":{"__typename":"PullRequest","url":"https://github.com/papapumpkin/quasar/pull/99"}}]}`)

	deps := testDeps(fakeGH(meta, comments, timeline, nil))
	src, err := newSource(map[string]any{"repo": "papapumpkin/quasar"}, &captureSecrets{value: "tok"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}

	tk, err := src.Fetch(context.Background(), "42")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if tk.SourceName != "github" {
		t.Errorf("SourceName = %q, want github", tk.SourceName)
	}
	if tk.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("SourceID = %q, want papapumpkin/quasar#42", tk.SourceID)
	}
	if tk.Number != 42 {
		t.Errorf("Number = %d, want 42", tk.Number)
	}
	if tk.Title != "truncate breaks on multibyte runes" {
		t.Errorf("Title = %q", tk.Title)
	}
	if tk.State != "open" {
		t.Errorf("State = %q, want open (normalized)", tk.State)
	}
	if len(tk.Labels) != 2 || tk.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug, good first issue]", tk.Labels)
	}
	if tk.Assignee != "octocat" {
		t.Errorf("Assignee = %q, want octocat (primary)", tk.Assignee)
	}
	if tk.URL != "https://github.com/papapumpkin/quasar/issues/42" {
		t.Errorf("URL = %q", tk.URL)
	}
	if len(tk.Comments) != 2 || tk.Comments[0].Author != "alice" {
		t.Errorf("Comments = %+v, want 2 chronological comments", tk.Comments)
	}
	if len(tk.LinkedWork) != 1 || tk.LinkedWork[0] != "https://github.com/papapumpkin/quasar/pull/99" {
		t.Errorf("LinkedWork = %v, want one linked PR", tk.LinkedWork)
	}
}

func TestFetchQualifiedSourceIDOverridesRepo(t *testing.T) {
	t.Parallel()

	meta := readFixture(t, "gh-issue-42.json")
	deps := testDeps(fakeGH(meta, []byte(`{"comments":[]}`), nil, nil))
	src, err := newSource(map[string]any{"repo": "papapumpkin/quasar"}, &captureSecrets{value: "tok"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}

	tk, err := src.Fetch(context.Background(), "acme/widgets#7")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if tk.SourceID != "acme/widgets#7" {
		t.Errorf("SourceID = %q, want acme/widgets#7 (qualified id wins)", tk.SourceID)
	}
}

func TestFetchAuthFailed(t *testing.T) {
	t.Parallel()

	authErr := &ghError{ExitCode: 4, Stderr: "gh: To get started with GitHub CLI, please run: gh auth login"}
	deps := testDeps(fakeGH(nil, nil, nil, authErr))
	src, err := newSource(map[string]any{"repo": "owner/repo"}, &captureSecrets{value: "tok"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}

	_, err = src.Fetch(context.Background(), "1")
	var authFailed *AuthFailedError
	if !errors.As(err, &authFailed) {
		t.Fatalf("Fetch error = %v, want *AuthFailedError", err)
	}
}

func TestFetchNotFound(t *testing.T) {
	t.Parallel()

	notFound := &ghError{ExitCode: 1, Stderr: "GraphQL: Could not resolve to an Issue with the number of 999."}
	deps := testDeps(fakeGH(nil, nil, nil, notFound))
	src, err := newSource(map[string]any{"repo": "owner/repo"}, &captureSecrets{value: "tok"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}

	_, err = src.Fetch(context.Background(), "999")
	var nf *TicketNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Fetch error = %v, want *TicketNotFoundError", err)
	}
}

func TestFetchMissingRepo(t *testing.T) {
	t.Parallel()

	// No repo configured and detection fails -> bare number is unresolvable.
	deps := testDeps(fakeGH(nil, nil, nil, nil))
	src, err := newSource(nil, &captureSecrets{value: "tok"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}

	_, err = src.Fetch(context.Background(), "42")
	var missing *MissingRepoError
	if !errors.As(err, &missing) {
		t.Fatalf("Fetch error = %v, want *MissingRepoError", err)
	}
}

func TestNewGHNotInstalled(t *testing.T) {
	t.Parallel()

	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		detectRepo: func() (string, error) { return "", nil },
		newRunGH:   func(string) runGHFunc { return fakeGH(nil, nil, nil, nil) },
	}

	_, err := newSource(map[string]any{}, &captureSecrets{}, deps)
	var notInstalled *GHNotInstalledError
	if !errors.As(err, &notInstalled) {
		t.Fatalf("newSource error = %v, want *GHNotInstalledError", err)
	}
}

func TestNewTokenFromEnvSpec(t *testing.T) {
	t.Parallel()

	secrets := &captureSecrets{value: "env-token"}
	var wiredToken string
	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "", errors.New("no git") },
		newRunGH:   func(token string) runGHFunc { wiredToken = token; return fakeGH(nil, nil, nil, nil) },
	}

	_, err := newSource(map[string]any{"token_env": "GITHUB_TOKEN"}, secrets, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}
	if secrets.gotSpec.Env != "GITHUB_TOKEN" {
		t.Errorf("resolver spec Env = %q, want GITHUB_TOKEN", secrets.gotSpec.Env)
	}
	if wiredToken != "env-token" {
		t.Errorf("runGH wired with token %q, want env-token", wiredToken)
	}
}

func TestNewTokenFromFileSpec(t *testing.T) {
	t.Parallel()

	secrets := &captureSecrets{value: "file-token"}
	var wiredToken string
	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "", errors.New("no git") },
		newRunGH:   func(token string) runGHFunc { wiredToken = token; return fakeGH(nil, nil, nil, nil) },
	}

	_, err := newSource(map[string]any{"token_file": "/run/secrets/gh"}, secrets, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}
	if secrets.gotSpec.File != "/run/secrets/gh" {
		t.Errorf("resolver spec File = %q, want /run/secrets/gh", secrets.gotSpec.File)
	}
	if wiredToken != "file-token" {
		t.Errorf("runGH wired with token %q, want file-token", wiredToken)
	}
}

func TestNewUsesConfiguredRepoOverDetection(t *testing.T) {
	t.Parallel()

	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "detected/repo", nil },
		newRunGH:   func(string) runGHFunc { return fakeGH(nil, nil, nil, nil) },
	}

	src, err := newSource(map[string]any{"repo": "config/repo"}, &captureSecrets{value: "t"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}
	if src.repo != "config/repo" {
		t.Errorf("repo = %q, want config/repo (config beats detection)", src.repo)
	}
}

func TestNewFallsBackToDetectedRepo(t *testing.T) {
	t.Parallel()

	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "detected/repo", nil },
		newRunGH:   func(string) runGHFunc { return fakeGH(nil, nil, nil, nil) },
	}

	src, err := newSource(map[string]any{}, &captureSecrets{value: "t"}, deps)
	if err != nil {
		t.Fatalf("newSource returned error: %v", err)
	}
	if src.repo != "detected/repo" {
		t.Errorf("repo = %q, want detected/repo (auto-detection)", src.repo)
	}
}

func TestNameAndRegistration(t *testing.T) {
	t.Parallel()

	if (&Source{}).Name() != "github" {
		t.Errorf("Name() = %q, want github", (&Source{}).Name())
	}

	// init() must have registered the adapter under "github" in the default
	// registry. Build it via a fake resolver; gh need not be installed because
	// BuildTicketSource only invokes the constructor, which is allowed to fail
	// on a missing binary — so we only assert the constructor is reachable.
	_, err := integrations.Default().BuildTicketSource("github", map[string]any{}, integrations.OSSecretResolver{})
	if err != nil && !errors.As(err, new(*GHNotInstalledError)) {
		// A GHNotInstalledError is acceptable in CI without gh; any other
		// error (e.g. "no TicketSource registered") indicates init() failed.
		t.Fatalf("BuildTicketSource(github) error = %v, want nil or *GHNotInstalledError", err)
	}
}
