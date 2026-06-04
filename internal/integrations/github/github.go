package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/papapumpkin/quasar/internal/integrations"
)

// ghInstallURL is surfaced in GHNotInstalledError so operators know where to go.
const ghInstallURL = "https://cli.github.com/"

// Source is the GitHub Issues TicketSource. It is read-only: it shells out to
// `gh` solely to fetch issue metadata, comments, and cross-references. It never
// performs git write operations — those belong to internal/gitops/.
//
// Source is safe for concurrent use: all fields are set at construction and the
// runGH hook is expected to be reentrant.
type Source struct {
	repo  string    // "owner/repo" used when a source-id is a bare number
	token string    // resolved at construction; "" -> defer to gh's own auth
	runGH runGHFunc // injectable for tests; defaults to the real gh binary
}

// Compile-time assertion that Source satisfies the read-side integration.
var _ integrations.TicketSource = (*Source)(nil)

// sourceDeps groups the externally-observable side effects New performs so they
// can be stubbed in tests without package-level mutable state.
type sourceDeps struct {
	lookPath   func(file string) (string, error)
	detectRepo func() (string, error)
	newRunGH   func(token string) runGHFunc
}

// defaultDeps wires the production side effects.
func defaultDeps() sourceDeps {
	return sourceDeps{
		lookPath:   exec.LookPath,
		detectRepo: detectRepoFromGitConfig,
		newRunGH:   defaultRunGH,
	}
}

// New constructs a GitHub Source from its config section and a secret resolver.
// It is the constructor registered with the integration registry.
//
// It (1) reads repo from config, falling back to .git/config auto-detection;
// (2) verifies the gh CLI is on PATH; (3) resolves the token via the phase-1
// precedence (token_file > token_env > gh's own auth); and (4) wires the real
// gh shell-out. A missing gh binary yields a *GHNotInstalledError.
func New(cfg map[string]any, secrets integrations.SecretResolver) (*Source, error) {
	return newSource(cfg, secrets, defaultDeps())
}

// newSource is the dependency-injected core of New, used directly by tests.
func newSource(cfg map[string]any, secrets integrations.SecretResolver, deps sourceDeps) (*Source, error) {
	if _, err := deps.lookPath("gh"); err != nil {
		return nil, &GHNotInstalledError{}
	}

	repo := stringFromCfg(cfg, "repo")
	if repo == "" {
		// Best-effort: an undetectable repo is not fatal here. It only becomes
		// an error if a bare-number source-id is later fetched (MissingRepoError).
		if detected, err := deps.detectRepo(); err == nil {
			repo = detected
		}
	}

	token, err := secrets.Resolve(integrations.SecretSpec{
		Env:  stringFromCfg(cfg, "token_env"),
		File: stringFromCfg(cfg, "token_file"),
	})
	if err != nil {
		return nil, fmt.Errorf("resolve github token: %w", err)
	}

	// When token is empty, gh is left to resolve its own credential chain (gh
	// config, keychain, ambient GH_TOKEN). We do NOT probe `gh auth status` at
	// construction: that would be a subprocess side effect in a constructor and
	// in tests. An unauthenticated environment instead surfaces as an
	// *AuthFailedError on the first Fetch, which is the failure callers act on.
	return &Source{
		repo:  repo,
		token: token,
		runGH: deps.newRunGH(token),
	}, nil
}

// Name returns the adapter name and registry key.
func (s *Source) Name() string { return "github" }

// Fetch retrieves a single GitHub issue plus its comments and any linked pull
// requests. It performs three gh calls: metadata, comments, and (best-effort,
// non-fatal) timeline cross-references. gh failures are mapped onto typed
// errors (*TicketNotFoundError, *AuthFailedError) for downstream UX handling.
func (s *Source) Fetch(ctx context.Context, sourceID string) (*integrations.Ticket, error) {
	repo, number, err := ParseSourceID(sourceID, s.repo)
	if err != nil {
		return nil, err
	}
	numStr := strconv.Itoa(number)

	metaOut, err := s.runGH(ctx, "issue", "view", numStr, "--repo", repo, "--json", "number,title,body,state,labels,assignees,url")
	if err != nil {
		return nil, classifyGHError(repo, number, err)
	}
	view, err := parseIssueView(metaOut)
	if err != nil {
		return nil, fmt.Errorf("github: parse issue %s#%d: %w", repo, number, err)
	}

	commentsOut, err := s.runGH(ctx, "issue", "view", numStr, "--repo", repo, "--json", "comments")
	if err != nil {
		return nil, classifyGHError(repo, number, err)
	}
	comments, err := parseComments(commentsOut)
	if err != nil {
		return nil, fmt.Errorf("github: parse comments for %s#%d: %w", repo, number, err)
	}

	// Linked PRs are a nicety, not a requirement: tolerate any failure.
	var linked []string
	if tlOut, tlErr := s.runGH(ctx, "issue", "view", numStr, "--repo", repo, "--json", "timelineItems"); tlErr == nil {
		linked = parseLinkedPRs(tlOut)
	}

	return &integrations.Ticket{
		SourceName: "github",
		SourceID:   fmt.Sprintf("%s#%d", repo, number),
		Number:     number,
		Title:      view.Title,
		Body:       view.Body,
		State:      normalizeState(view.State),
		Labels:     view.LabelNames(),
		Assignee:   view.PrimaryAssignee(),
		URL:        view.URL,
		Comments:   comments,
		LinkedWork: linked,
	}, nil
}

// stringFromCfg reads a string value from a config map, returning "" when the
// key is absent or not a string.
func stringFromCfg(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

// detectRepoFromGitConfig reads ./.git/config and parses the origin remote into
// an "owner/repo" slug. This avoids shelling out to git (forbidden in this
// package); proper detection moves to internal/gitops/ in a later phase.
func detectRepoFromGitConfig() (string, error) {
	data, err := os.ReadFile(filepath.Join(".git", "config"))
	if err != nil {
		return "", err
	}
	url, ok := parseGitConfigOrigin(string(data))
	if !ok {
		return "", fmt.Errorf("no origin remote in .git/config")
	}
	repo, ok := parseRemoteURL(url)
	if !ok {
		return "", fmt.Errorf("could not parse origin url %q", url)
	}
	return repo, nil
}

// classifyGHError maps a gh failure onto a typed adapter error when the shape is
// recognizable, falling back to the raw error otherwise. gh exit codes: 4 means
// authentication required; a 404 / "could not resolve" indicates the issue does
// not exist or is inaccessible.
func classifyGHError(repo string, number int, err error) error {
	var ge *ghError
	if !errors.As(err, &ge) {
		return err
	}
	stderr := strings.ToLower(ge.Stderr)

	if ge.ExitCode == 4 || strings.Contains(stderr, "gh auth login") || strings.Contains(stderr, "authentication") {
		return &AuthFailedError{Repo: repo, err: err}
	}
	if strings.Contains(stderr, "could not resolve") || strings.Contains(stderr, "404") || strings.Contains(stderr, "not found") {
		return &TicketNotFoundError{Repo: repo, Number: number, err: err}
	}
	return err
}

// GHNotInstalledError reports that the gh CLI was not found on PATH.
type GHNotInstalledError struct{}

// Error implements the error interface.
func (e *GHNotInstalledError) Error() string {
	return fmt.Sprintf("github: gh CLI not found on PATH; install it from %s", ghInstallURL)
}

// MissingRepoError reports that a bare-number source-id was given but no
// repository could be determined from config or .git/config.
type MissingRepoError struct {
	SourceID string
}

// Error implements the error interface.
func (e *MissingRepoError) Error() string {
	return fmt.Sprintf("github: cannot resolve repository for source id %q: set [integrations.github].repo = \"owner/repo\" in .quasar.yaml, or use the explicit \"owner/repo#%s\" form", e.SourceID, e.SourceID)
}

// TicketNotFoundError reports that gh could not find the requested issue (404
// or "could not resolve").
type TicketNotFoundError struct {
	Repo   string
	Number int
	err    error
}

// Error implements the error interface.
func (e *TicketNotFoundError) Error() string {
	return fmt.Sprintf("github: issue %s#%d not found", e.Repo, e.Number)
}

// Unwrap exposes the underlying gh error for errors.Is/As traversal.
func (e *TicketNotFoundError) Unwrap() error { return e.err }

// AuthFailedError reports that gh is not authenticated for the requested repo.
type AuthFailedError struct {
	Repo string
	err  error
}

// Error implements the error interface.
func (e *AuthFailedError) Error() string {
	return fmt.Sprintf("github: authentication failed for %s; run `gh auth login` or set token_env/token_file", e.Repo)
}

// Unwrap exposes the underlying gh error for errors.Is/As traversal.
func (e *AuthFailedError) Unwrap() error { return e.err }

// init registers the GitHub adapter under "github" in the default registry. The
// cmd layer imports this package for its side effect so the adapter is wired.
func init() {
	integrations.Default().RegisterTicketSource("github", func(cfg map[string]any, secrets integrations.SecretResolver) (integrations.TicketSource, error) {
		return New(cfg, secrets)
	})
}
