package github

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// captureSecrets is a SecretResolver that records the spec it was asked to
// resolve and returns a canned value/error.
type captureSecrets struct {
	gotSpec sensors.SecretSpec
	value   string
	err     error
}

func (c *captureSecrets) Resolve(spec sensors.SecretSpec) (string, error) {
	c.gotSpec = spec
	return c.value, c.err
}

// subcommand returns the gh subcommand (args[0]) or "".
func subcommand(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// fakeListGH returns a runGHFunc that answers `issue list` with the given bytes
// and fails any other call (this sensor only lists during Poll). A non-nil
// failOn error short-circuits the list call to simulate gh failures.
func fakeListGH(list []byte, failOn error) runGHFunc {
	return func(_ context.Context, args ...string) ([]byte, error) {
		if subcommand(args) == "issue" && len(args) > 1 && args[1] == "list" {
			if failOn != nil {
				return nil, failOn
			}
			return list, nil
		}
		return nil, errors.New("unexpected gh call")
	}
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

// configured builds a Source wired with the given deps and config, asserting
// Configure succeeds.
func configured(t *testing.T, deps sourceDeps, cfg map[string]any) *Source {
	t.Helper()
	s := &Source{deps: deps}
	if err := s.Configure(cfg, &captureSecrets{value: "tok"}); err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	return s
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestPollReturnsEventsAndAdvancesCursor(t *testing.T) {
	t.Parallel()

	list := readFixture(t, "gh-issue-list.json")
	s := configured(t, testDeps(fakeListGH(list, nil)), map[string]any{"repo": "papapumpkin/quasar"})

	events, cursor, err := s.Poll(context.Background(), nil)
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	// Cursor advances to the highest issue number seen (last-issue-number-seen).
	var c issueCursor
	if err := json.Unmarshal(cursor, &c); err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if c.LastIssueNumber != 42 {
		t.Errorf("cursor.LastIssueNumber = %d, want 42", c.LastIssueNumber)
	}

	// Event identity is "<owner>/<repo>#<number>".
	wantIDs := map[string]bool{
		"papapumpkin/quasar#42": false,
		"papapumpkin/quasar#41": false,
		"papapumpkin/quasar#40": false,
	}
	for _, e := range events {
		if _, ok := wantIDs[e.ExternalID]; !ok {
			t.Errorf("unexpected event id %q", e.ExternalID)
		}
		wantIDs[e.ExternalID] = true
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("missing event id %q", id)
		}
	}
}

func TestPollCursorFiltersAlreadySeen(t *testing.T) {
	t.Parallel()

	list := readFixture(t, "gh-issue-list.json")
	s := configured(t, testDeps(fakeListGH(list, nil)), map[string]any{"repo": "papapumpkin/quasar"})

	// Cursor at 41 means only issue 42 is new.
	start, _ := encodeCursor(41)
	events, cursor, err := s.Poll(context.Background(), start)
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if len(events) != 1 || events[0].ExternalID != "papapumpkin/quasar#42" {
		t.Fatalf("events = %+v, want only issue 42", events)
	}
	if last, _ := decodeCursor(cursor); last != 42 {
		t.Errorf("cursor advanced to %d, want 42", last)
	}

	// Re-polling at 42 yields nothing and the cursor holds steady.
	none, held, err := s.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatalf("second Poll returned error: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no new events, got %d", len(none))
	}
	if last, _ := decodeCursor(held); last != 42 {
		t.Errorf("cursor = %d, want 42 (unchanged)", last)
	}
}

func TestPollMissingRepo(t *testing.T) {
	t.Parallel()

	s := configured(t, testDeps(fakeListGH(nil, nil)), nil) // no repo, detection fails
	_, _, err := s.Poll(context.Background(), nil)
	var missing *MissingRepoError
	if !errors.As(err, &missing) {
		t.Fatalf("Poll error = %v, want *MissingRepoError", err)
	}
}

func TestPollAuthFailed(t *testing.T) {
	t.Parallel()

	authErr := &ghError{ExitCode: 4, Stderr: "gh: To get started with GitHub CLI, please run: gh auth login"}
	s := configured(t, testDeps(fakeListGH(nil, authErr)), map[string]any{"repo": "owner/repo"})
	_, _, err := s.Poll(context.Background(), nil)
	var authFailed *AuthFailedError
	if !errors.As(err, &authFailed) {
		t.Fatalf("Poll error = %v, want *AuthFailedError", err)
	}
}

func TestSeedNebula(t *testing.T) {
	t.Parallel()

	list := readFixture(t, "gh-issue-list.json")
	s := configured(t, testDeps(fakeListGH(list, nil)), map[string]any{"repo": "papapumpkin/quasar"})

	events, _, err := s.Poll(context.Background(), nil)
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}

	// Find issue 42 (the rich one with goals + constraints).
	var ev sensors.Event
	for _, e := range events {
		if e.ExternalID == "papapumpkin/quasar#42" {
			ev = e
		}
	}

	content, err := s.SeedNebula(ev)
	if err != nil {
		t.Fatalf("SeedNebula returned error: %v", err)
	}
	if content.SourceName != "github" {
		t.Errorf("SourceName = %q, want github", content.SourceName)
	}
	if content.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("SourceID = %q, want papapumpkin/quasar#42", content.SourceID)
	}
	if content.SourceURL != "https://github.com/papapumpkin/quasar/issues/42" {
		t.Errorf("SourceURL = %q", content.SourceURL)
	}
	if content.Name != "truncate breaks on multibyte runes" {
		t.Errorf("Name = %q", content.Name)
	}
	if len(content.Goals) != 2 || content.Goals[0] != "Fix the off-by-one in truncate" {
		t.Errorf("Goals = %v, want the two body bullets", content.Goals)
	}
	if len(content.Constraints) != 2 || content.Constraints[0] != "Do not add new dependencies" {
		t.Errorf("Constraints = %v, want the two constraint bullets", content.Constraints)
	}
	if len(content.Labels) != 2 || content.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug, good first issue]", content.Labels)
	}
	if content.Assignee != "octocat" {
		t.Errorf("Assignee = %q, want octocat", content.Assignee)
	}
}

func TestSeedNebulaFallsBackToTitleGoal(t *testing.T) {
	t.Parallel()

	s := &Source{}
	ev := sensors.Event{
		ExternalID: "owner/repo#7",
		Raw: map[string]any{
			"title": "Investigate flaky test",
			"body":  "No bullets here, just prose.",
			"url":   "https://example.com/7",
		},
	}
	content, err := s.SeedNebula(ev)
	if err != nil {
		t.Fatalf("SeedNebula returned error: %v", err)
	}
	if len(content.Goals) != 1 || content.Goals[0] != "Investigate flaky test" {
		t.Errorf("Goals = %v, want [title] fallback", content.Goals)
	}
	if len(content.Constraints) != 0 {
		t.Errorf("Constraints = %v, want none", content.Constraints)
	}
}

func TestConfigureGHNotInstalled(t *testing.T) {
	t.Parallel()

	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		detectRepo: func() (string, error) { return "", nil },
		newRunGH:   func(string) runGHFunc { return fakeListGH(nil, nil) },
	}
	s := &Source{deps: deps}
	err := s.Configure(map[string]any{}, &captureSecrets{})
	var notInstalled *GHNotInstalledError
	if !errors.As(err, &notInstalled) {
		t.Fatalf("Configure error = %v, want *GHNotInstalledError", err)
	}
}

func TestConfigureWiresToken(t *testing.T) {
	t.Parallel()

	secrets := &captureSecrets{value: "env-token"}
	var wiredToken string
	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "", errors.New("no git") },
		newRunGH:   func(token string) runGHFunc { wiredToken = token; return fakeListGH(nil, nil) },
	}
	s := &Source{deps: deps}
	if err := s.Configure(map[string]any{"token_env": "GITHUB_TOKEN"}, secrets); err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if secrets.gotSpec.Env != "GITHUB_TOKEN" {
		t.Errorf("resolver spec Env = %q, want GITHUB_TOKEN", secrets.gotSpec.Env)
	}
	if wiredToken != "env-token" {
		t.Errorf("runGH wired with token %q, want env-token", wiredToken)
	}
}

func TestConfigureUsesConfiguredRepoOverDetection(t *testing.T) {
	t.Parallel()

	deps := sourceDeps{
		lookPath:   func(string) (string, error) { return "/usr/bin/gh", nil },
		detectRepo: func() (string, error) { return "detected/repo", nil },
		newRunGH:   func(string) runGHFunc { return fakeListGH(nil, nil) },
	}
	s := configured(t, deps, map[string]any{"repo": "config/repo"})
	if s.repo != "config/repo" {
		t.Errorf("repo = %q, want config/repo (config beats detection)", s.repo)
	}
}

func TestNameAndRegistration(t *testing.T) {
	t.Parallel()

	if New().Name() != "github_issues" {
		t.Errorf("Name() = %q, want github_issues", New().Name())
	}

	// init() must have registered the sensor under "github_issues".
	s, err := sensors.Default().BuildSensor("github_issues")
	if err != nil {
		t.Fatalf("BuildSensor(github_issues) error = %v, want nil", err)
	}
	if s.Name() != "github_issues" {
		t.Errorf("built sensor Name() = %q, want github_issues", s.Name())
	}
}
