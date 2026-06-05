// Package github implements a read-only GitHub Issues Sensor backed by the `gh`
// CLI. It is the only sensor Quasar ships with at launch; other trackers (Jira,
// Linear, …) follow the same shape in their own sub-packages.
//
// The package is deliberately split so the bulk of the logic is unit-testable
// without a real gh binary:
//
//   - parse.go  pure functions: source-id parsing, JSON decoding, field helpers
//   - exec.go   the thin, swappable shell-out wrapper around `gh`
//   - github.go the Source sensor wiring those together + registry init()
//
// All git operations are intentionally absent here: vanilla `git` belongs in
// internal/gitops/, and `gh` is used ONLY for issue reading.
package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// ParseSourceID resolves a source-id string into an "owner/repo" slug and an
// issue number. Three input shapes are accepted:
//
//	"42"             bare number; repo taken from defaultRepo
//	"owner/repo#42"  fully qualified; repo from the input
//	"#42"            ambiguous; rejected
//
// When the input is a bare number and defaultRepo is empty, a *MissingRepoError
// is returned naming both fixes. A leading "#" with no repo is rejected as
// ambiguous rather than silently using defaultRepo, because that form signals
// the caller meant to be explicit.
func ParseSourceID(input, defaultRepo string) (repo string, number int, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", 0, fmt.Errorf("github: empty source id")
	}

	if i := strings.Index(input, "#"); i != -1 {
		repo = strings.TrimSpace(input[:i])
		numStr := strings.TrimSpace(input[i+1:])
		if repo == "" {
			return "", 0, fmt.Errorf("github: ambiguous source id %q: use %q or a bare number with the sensor's repo configured", input, "owner/repo#"+numStr)
		}
		if !validRepo(repo) {
			return "", 0, fmt.Errorf("github: invalid repository %q in source id %q: want \"owner/repo\"", repo, input)
		}
		number, err = parseIssueNumber(numStr)
		if err != nil {
			return "", 0, fmt.Errorf("github: invalid issue number in source id %q: %w", input, err)
		}
		return repo, number, nil
	}

	number, err = parseIssueNumber(input)
	if err != nil {
		return "", 0, fmt.Errorf("github: invalid source id %q: want a number or \"owner/repo#number\": %w", input, err)
	}
	if defaultRepo == "" {
		return "", 0, &MissingRepoError{SourceID: input}
	}
	return defaultRepo, number, nil
}

// parseIssueNumber parses a positive issue number, rejecting zero and negatives.
func parseIssueNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("issue number must be positive, got %d", n)
	}
	return n, nil
}

// validRepo reports whether s has the "owner/repo" shape with both parts set.
func validRepo(s string) bool {
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// ghLabel mirrors a single label object from `gh issue view --json labels`.
type ghLabel struct {
	Name string `json:"name"`
}

// ghUser mirrors a GitHub user object (author/assignee) from gh JSON output.
type ghUser struct {
	Login string `json:"login"`
}

// issueView is the decoded shape of `gh issue view --json
// number,title,body,state,labels,assignees,url`.
type issueView struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	Labels    []ghLabel `json:"labels"`
	Assignees []ghUser  `json:"assignees"`
}

// LabelNames returns the label names in source order; nil when there are none.
func (v issueView) LabelNames() []string {
	if len(v.Labels) == 0 {
		return nil
	}
	names := make([]string, 0, len(v.Labels))
	for _, l := range v.Labels {
		names = append(names, l.Name)
	}
	return names
}

// PrimaryAssignee returns the first assignee's login, or "" if unassigned.
// GitHub issues can have multiple assignees; Quasar only models a single one.
func (v issueView) PrimaryAssignee() string {
	if len(v.Assignees) == 0 {
		return ""
	}
	return v.Assignees[0].Login
}

// parseIssueView decodes the metadata JSON object from `gh issue view`.
func parseIssueView(data []byte) (issueView, error) {
	var v issueView
	if err := json.Unmarshal(data, &v); err != nil {
		return issueView{}, fmt.Errorf("decode issue metadata: %w", err)
	}
	return v, nil
}

// parseIssueList decodes the JSON array emitted by `gh issue list --json …`
// into issueView values, preserving gh's source ordering. gh lists newest
// issues first by default; the sensor sorts/filters by issue number.
func parseIssueList(data []byte) ([]issueView, error) {
	var views []issueView
	if err := json.Unmarshal(data, &views); err != nil {
		return nil, fmt.Errorf("decode issue list: %w", err)
	}
	return views, nil
}

// commentEnvelope is the decoded shape of `gh issue view --json comments`.
type commentEnvelope struct {
	Comments []ghComment `json:"comments"`
}

// ghComment mirrors a single comment from gh JSON output. gh emits createdAt as
// an RFC3339 timestamp, which time.Time unmarshals natively.
type ghComment struct {
	Author    ghUser    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// parseComments decodes the comments JSON object into chronological
// sensors.Comment values, preserving gh's source ordering.
func parseComments(data []byte) ([]sensors.Comment, error) {
	var env commentEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode issue comments: %w", err)
	}
	if len(env.Comments) == 0 {
		return nil, nil
	}
	out := make([]sensors.Comment, 0, len(env.Comments))
	for _, c := range env.Comments {
		out = append(out, sensors.Comment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	return out, nil
}

// timelineEnvelope is the decoded shape of `gh issue view --json timelineItems`.
type timelineEnvelope struct {
	TimelineItems []timelineItem `json:"timelineItems"`
}

// timelineItem captures the subset of a timeline entry we care about: cross- or
// plain references whose source is a pull request.
type timelineItem struct {
	Typename string `json:"__typename"`
	Source   struct {
		Typename string `json:"__typename"`
		URL      string `json:"url"`
	} `json:"source"`
}

// parseLinkedPRs extracts pull-request URLs referenced from the issue timeline.
// It is best-effort: unknown entry shapes are skipped rather than erroring, and
// duplicate URLs are de-duplicated while preserving first-seen order.
func parseLinkedPRs(data []byte) []string {
	var env timelineEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	var (
		urls []string
		seen = map[string]bool{}
	)
	for _, item := range env.TimelineItems {
		if item.Typename != "CrossReferencedEvent" && item.Typename != "ReferencedEvent" {
			continue
		}
		if item.Source.Typename != "PullRequest" || item.Source.URL == "" {
			continue
		}
		if seen[item.Source.URL] {
			continue
		}
		seen[item.Source.URL] = true
		urls = append(urls, item.Source.URL)
	}
	return urls
}

// normalizeState lowercases gh's "OPEN"/"CLOSED" into Quasar's "open"/"closed"
// convention while leaving any adapter-specific state untouched.
func normalizeState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}

// parseRemoteURL extracts an "owner/repo" slug from a git remote URL in either
// scp-like (git@github.com:owner/repo.git) or URL (https://github.com/owner/
// repo.git, ssh://…) form. The boolean reports whether parsing succeeded.
func parseRemoteURL(url string) (string, bool) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")

	if strings.HasPrefix(url, "git@") {
		if i := strings.Index(url, ":"); i != -1 {
			return cleanRepoPath(url[i+1:])
		}
		return "", false
	}

	for _, scheme := range []string{"ssh://", "https://", "http://"} {
		if strings.HasPrefix(url, scheme) {
			rest := url[len(scheme):]
			if i := strings.Index(rest, "/"); i != -1 {
				return cleanRepoPath(rest[i+1:])
			}
			return "", false
		}
	}
	return "", false
}

// cleanRepoPath reduces a path like "owner/repo" (possibly with extra leading
// segments) to its trailing owner/repo pair.
func cleanRepoPath(p string) (string, bool) {
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

// parseGitConfigOrigin extracts the origin remote URL from raw .git/config
// contents. It is a minimal INI scan, sufficient for the standard
// [remote "origin"] / url = … layout git writes.
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
