package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// sensorName is the registry key for the GitHub Issues sensor. The "_issues"
// suffix leaves room for a future "github_prs" sensor type.
const sensorName = "github_issues"

// forgeName is the forge the produced seed nebulas attribute their source to.
// It is intentionally distinct from sensorName: many sensor types ("issues",
// "prs") can feed the same forge.
const forgeName = "github"

// pollLimit caps how many issues a single Poll pulls from gh. The cursor
// guarantees forward progress across polls, so a bounded page is sufficient.
const pollLimit = 100

// ghInstallURL is surfaced in GHNotInstalledError so operators know where to go.
const ghInstallURL = "https://cli.github.com/"

// Source is the GitHub Issues Sensor. It is read-only: it shells out to `gh`
// solely to list and read issues. It never performs git write operations —
// those belong to internal/gitops/.
//
// Source is safe for concurrent use once Configure has run: all fields are set
// during Configure and the runGH hook is expected to be reentrant.
type Source struct {
	repo           string   // "owner/repo" used to scope issue lists and qualify ids
	token          string   // resolved in Configure; "" -> defer to gh's own auth
	labelFilter    []string // when set, an issue must carry every label to qualify
	assigneeFilter string   // when set, restrict to this assignee ("@me"/"@none" deferred to gh)
	runGH          runGHFunc // injectable for tests; defaults to the real gh binary
	deps           sourceDeps
}

// Compile-time assertion that Source satisfies the poll-driven sensor.
var _ sensors.Sensor = (*Source)(nil)

// sourceDeps groups the externally-observable side effects Configure performs so
// they can be stubbed in tests without package-level mutable state.
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

// New constructs an unconfigured GitHub Source. It is the constructor
// registered with the sensor registry; the runtime calls Configure afterward
// with the instance's [config] block and a SecretResolver.
func New() *Source {
	return &Source{deps: defaultDeps()}
}

// Name returns the sensor type's name and registry key.
func (s *Source) Name() string { return sensorName }

// Configure reads repo from the config block (falling back to .git/config
// auto-detection), verifies the gh CLI is on PATH, resolves the token via the
// token_file > token_env > gh-auth precedence, and wires the gh shell-out. A
// missing gh binary yields a *GHNotInstalledError.
func (s *Source) Configure(raw map[string]any, secrets sensors.SecretResolver) error {
	if s.deps.lookPath == nil {
		s.deps = defaultDeps()
	}
	if _, err := s.deps.lookPath("gh"); err != nil {
		return &GHNotInstalledError{}
	}

	repo := stringFromCfg(raw, "repo")
	if repo == "" {
		// Best-effort: an undetectable repo only becomes an error at Poll time.
		if detected, err := s.deps.detectRepo(); err == nil {
			repo = detected
		}
	}

	token, err := secrets.Resolve(sensors.SecretSpec{
		Env:  stringFromCfg(raw, "token_env"),
		File: stringFromCfg(raw, "token_file"),
	})
	if err != nil {
		return fmt.Errorf("resolve github token: %w", err)
	}

	s.repo = repo
	s.token = token
	s.labelFilter = stringsFromCfg(raw, "labels")
	s.assigneeFilter = stringFromCfg(raw, "assignee")
	s.runGH = s.deps.newRunGH(token)
	return nil
}

// issueCursor is the sensor-defined opaque cursor: the highest issue number
// observed so far. Issues with a greater number are new work.
type issueCursor struct {
	LastIssueNumber int `json:"last_issue_number"`
}

// Poll lists open issues via gh and returns one Event per issue whose number is
// greater than the cursor, advancing newCursor to the highest number seen. The
// last-issue-number-seen cursor is monotonic, so re-polling never re-emits work
// already turned into seed nebulas.
func (s *Source) Poll(ctx context.Context, cursor json.RawMessage) ([]sensors.Event, json.RawMessage, error) {
	if s.repo == "" {
		return nil, cursor, &MissingRepoError{}
	}
	if s.runGH == nil {
		return nil, cursor, fmt.Errorf("github: sensor not configured (call Configure first)")
	}

	last, err := decodeCursor(cursor)
	if err != nil {
		return nil, cursor, err
	}

	out, err := s.runGH(ctx, s.listArgs()...)
	if err != nil {
		return nil, cursor, classifyGHError(s.repo, 0, err)
	}

	views, err := parseIssueList(out)
	if err != nil {
		return nil, cursor, fmt.Errorf("github: parse issue list for %s: %w", s.repo, err)
	}

	var events []sensors.Event
	highest := last
	for _, v := range views {
		if v.Number <= last {
			continue
		}
		if !s.matchesFilters(v) {
			continue
		}
		if v.Number > highest {
			highest = v.Number
		}
		events = append(events, sensors.Event{
			ExternalID: fmt.Sprintf("%s#%d", s.repo, v.Number),
			Raw: map[string]any{
				"repo":     s.repo,
				"number":   v.Number,
				"title":    v.Title,
				"body":     v.Body,
				"state":    normalizeState(v.State),
				"url":      v.URL,
				"labels":   v.LabelNames(),
				"assignee": v.PrimaryAssignee(),
			},
		})
	}

	newCursor, err := encodeCursor(highest)
	if err != nil {
		return nil, cursor, err
	}
	return events, newCursor, nil
}

// SeedNebula renders one Event into seed nebula content. It derives goals and
// constraints from the issue body (see deriveGoalsAndConstraints) and stamps
// the source provenance: source_name="github" and source_id="<owner>/<repo>#<n>".
func (s *Source) SeedNebula(event sensors.Event) (*sensors.SeedNebulaContent, error) {
	title := rawString(event.Raw, "title")
	body := rawString(event.Raw, "body")
	if event.ExternalID == "" {
		return nil, fmt.Errorf("github: event missing external id")
	}

	goals, constraints := deriveGoalsAndConstraints(title, body)

	return &sensors.SeedNebulaContent{
		Name:        title,
		Description: body,
		SourceName:  forgeName,
		SourceID:    event.ExternalID,
		SourceURL:   rawString(event.Raw, "url"),
		Goals:       goals,
		Constraints: constraints,
		Labels:      rawStrings(event.Raw, "labels"),
		Assignee:    rawString(event.Raw, "assignee"),
	}, nil
}

// listArgs builds the `gh issue list` argv, narrowing server-side by label and
// assignee when configured. Server-side narrowing keeps the response small and
// lets gh resolve special assignee tokens like "@me"; matchesFilters re-checks
// the returned issues so the cursor logic never depends on gh honoring a flag.
func (s *Source) listArgs() []string {
	args := []string{"issue", "list",
		"--repo", s.repo,
		"--state", "open",
		"--limit", strconv.Itoa(pollLimit),
		"--json", "number,title,body,state,labels,assignees,url",
	}
	for _, label := range s.labelFilter {
		args = append(args, "--label", label)
	}
	if s.assigneeFilter != "" {
		args = append(args, "--assignee", s.assigneeFilter)
	}
	return args
}

// matchesFilters reports whether an issue satisfies the configured label and
// assignee filters. Labels are ANDed (an issue must carry every configured
// label), mirroring gh's `--label a --label b` semantics. A "@"-prefixed
// assignee filter (e.g. "@me", "@none") is a gh-resolved token that cannot be
// matched against a login client-side, so it is deferred to gh and treated as a
// pass here.
func (s *Source) matchesFilters(v issueView) bool {
	if !hasAllLabels(v.LabelNames(), s.labelFilter) {
		return false
	}
	if s.assigneeFilter == "" || strings.HasPrefix(s.assigneeFilter, "@") {
		return true
	}
	for _, login := range v.assigneeLogins() {
		if login == s.assigneeFilter {
			return true
		}
	}
	return false
}

// hasAllLabels reports whether have contains every label in want. An empty want
// matches everything.
func hasAllLabels(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]bool, len(have))
	for _, l := range have {
		set[l] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// decodeCursor parses the opaque cursor into a last-issue-number. An empty or
// null cursor means "first poll" (number 0).
func decodeCursor(cursor json.RawMessage) (int, error) {
	if len(cursor) == 0 || string(cursor) == "null" {
		return 0, nil
	}
	var c issueCursor
	if err := json.Unmarshal(cursor, &c); err != nil {
		return 0, fmt.Errorf("github: decode cursor: %w", err)
	}
	return c.LastIssueNumber, nil
}

// encodeCursor marshals the highest-seen issue number into the opaque cursor.
func encodeCursor(last int) (json.RawMessage, error) {
	data, err := json.Marshal(issueCursor{LastIssueNumber: last})
	if err != nil {
		return nil, fmt.Errorf("github: encode cursor: %w", err)
	}
	return data, nil
}

// deriveGoalsAndConstraints extracts goals and constraints from a markdown issue
// body. Bullet lines ("- " / "* ") under a "constraints" or "acceptance"
// heading become constraints; all other bullet lines become goals. When no
// bullets yield goals, the issue title is used as the single goal so the seed
// nebula always carries at least one.
func deriveGoalsAndConstraints(title, body string) (goals, constraints []string) {
	inConstraints := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.ToLower(trimmed)
			inConstraints = strings.Contains(heading, "constraint") || strings.Contains(heading, "acceptance")
			continue
		}
		item, ok := bulletText(trimmed)
		if !ok {
			continue
		}
		if inConstraints {
			constraints = append(constraints, item)
		} else {
			goals = append(goals, item)
		}
	}
	if len(goals) == 0 {
		if t := strings.TrimSpace(title); t != "" {
			goals = []string{t}
		}
	}
	return goals, constraints
}

// bulletText returns the text of a markdown bullet line ("- x" / "* x") and
// true, or "" and false when the line is not a bullet.
func bulletText(trimmed string) (string, bool) {
	for _, marker := range []string{"- ", "* "} {
		if strings.HasPrefix(trimmed, marker) {
			text := strings.TrimSpace(trimmed[len(marker):])
			// Drop a leading task-list checkbox so "- [ ] do x" -> "do x".
			text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "[ ]"), "[x]"))
			if text != "" {
				return text, true
			}
		}
	}
	return "", false
}

// rawString reads a string value from an Event.Raw map, returning "" when the
// key is absent or not a string.
func rawString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	if v, ok := raw[key].(string); ok {
		return v
	}
	return ""
}

// rawStrings reads a []string value from an Event.Raw map, returning nil when
// the key is absent or not a []string.
func rawStrings(raw map[string]any, key string) []string {
	if raw == nil {
		return nil
	}
	if v, ok := raw[key].([]string); ok {
		return v
	}
	return nil
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

// stringsFromCfg reads a []string from a config map. TOML decodes a string
// array as []any of strings, so both []string and []any are accepted; non-string
// elements and absent/foreign keys yield nil.
func stringsFromCfg(cfg map[string]any, key string) []string {
	if cfg == nil {
		return nil
	}
	switch v := cfg[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
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

// MissingRepoError reports that no repository could be determined from the
// sensor config or .git/config, so issue listing cannot be scoped.
type MissingRepoError struct {
	SourceID string
}

// Error implements the error interface.
func (e *MissingRepoError) Error() string {
	if e.SourceID != "" {
		return fmt.Sprintf("github: cannot resolve repository for source id %q: set repo = \"owner/repo\" in the sensor config, or use the explicit \"owner/repo#%s\" form", e.SourceID, e.SourceID)
	}
	return "github: no repository configured; set repo = \"owner/repo\" in the sensor config"
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

// Is reports whether target is the source-agnostic sensors.ErrTicketNotFound
// sentinel, letting source-neutral callers detect a missing item via errors.Is
// without importing this adapter package.
func (e *TicketNotFoundError) Is(target error) bool {
	return target == sensors.ErrTicketNotFound
}

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

// init registers the GitHub Issues sensor under "github_issues" in the default
// registry. The cmd layer imports this package for its side effect so the
// sensor is wired.
func init() {
	sensors.Default().RegisterSensor(sensorName, func() (sensors.Sensor, error) {
		return New(), nil
	})
}
