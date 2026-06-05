package github

import (
	"errors"
	"testing"
	"time"
)

func TestParseSourceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		defaultRepo string
		wantRepo    string
		wantNumber  int
		wantErr     bool
		wantMissing bool // expect *MissingRepoError specifically
	}{
		{name: "bare number with default repo", input: "42", defaultRepo: "owner/repo", wantRepo: "owner/repo", wantNumber: 42},
		{name: "qualified owner/repo#n", input: "acme/widgets#7", defaultRepo: "", wantRepo: "acme/widgets", wantNumber: 7},
		{name: "qualified overrides default", input: "acme/widgets#7", defaultRepo: "owner/repo", wantRepo: "acme/widgets", wantNumber: 7},
		{name: "surrounding whitespace tolerated", input: "  13  ", defaultRepo: "owner/repo", wantRepo: "owner/repo", wantNumber: 13},
		{name: "bare number without repo", input: "42", defaultRepo: "", wantErr: true, wantMissing: true},
		{name: "leading hash is ambiguous", input: "#42", defaultRepo: "owner/repo", wantErr: true},
		{name: "empty input", input: "", defaultRepo: "owner/repo", wantErr: true},
		{name: "non-numeric bare", input: "abc", defaultRepo: "owner/repo", wantErr: true},
		{name: "zero issue number", input: "0", defaultRepo: "owner/repo", wantErr: true},
		{name: "negative issue number", input: "-3", defaultRepo: "owner/repo", wantErr: true},
		{name: "non-numeric after hash", input: "owner/repo#abc", defaultRepo: "", wantErr: true},
		{name: "malformed repo before hash", input: "notarepo#7", defaultRepo: "", wantErr: true},
		{name: "too many path segments", input: "a/b/c#7", defaultRepo: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, number, err := ParseSourceID(tt.input, tt.defaultRepo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSourceID(%q, %q) = (%q, %d, nil), want error", tt.input, tt.defaultRepo, repo, number)
				}
				if tt.wantMissing {
					var missing *MissingRepoError
					if !errors.As(err, &missing) {
						t.Fatalf("error = %v, want *MissingRepoError", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSourceID(%q, %q) returned error: %v", tt.input, tt.defaultRepo, err)
			}
			if repo != tt.wantRepo || number != tt.wantNumber {
				t.Errorf("ParseSourceID(%q, %q) = (%q, %d), want (%q, %d)", tt.input, tt.defaultRepo, repo, number, tt.wantRepo, tt.wantNumber)
			}
		})
	}
}

func TestIssueViewLabelNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		view issueView
		want []string
	}{
		{name: "nil when empty", view: issueView{}, want: nil},
		{name: "preserves order", view: issueView{Labels: []ghLabel{{Name: "bug"}, {Name: "p1"}}}, want: []string{"bug", "p1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.view.LabelNames()
			if len(got) != len(tt.want) {
				t.Fatalf("LabelNames() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("LabelNames()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIssueViewPrimaryAssignee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		view issueView
		want string
	}{
		{name: "empty when unassigned", view: issueView{}, want: ""},
		{name: "first of many", view: issueView{Assignees: []ghUser{{Login: "octocat"}, {Login: "hubot"}}}, want: "octocat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.view.PrimaryAssignee(); got != tt.want {
				t.Errorf("PrimaryAssignee() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseIssueView(t *testing.T) {
	t.Parallel()

	data := []byte(`{"number":42,"title":"t","body":"b","state":"OPEN","url":"u","labels":[{"name":"bug"}],"assignees":[{"login":"octocat"}]}`)
	v, err := parseIssueView(data)
	if err != nil {
		t.Fatalf("parseIssueView returned error: %v", err)
	}
	if v.Number != 42 || v.Title != "t" || v.State != "OPEN" || v.URL != "u" {
		t.Errorf("parseIssueView = %+v, want populated fields", v)
	}
	if len(v.Labels) != 1 || v.Labels[0].Name != "bug" {
		t.Errorf("Labels = %+v, want [bug]", v.Labels)
	}

	if _, err := parseIssueView([]byte(`{not json`)); err == nil {
		t.Error("parseIssueView(malformed) = nil error, want decode error")
	}
}

func TestParseComments(t *testing.T) {
	t.Parallel()

	data := []byte(`{"comments":[
		{"author":{"login":"alice"},"body":"first","createdAt":"2026-01-02T15:04:05Z"},
		{"author":{"login":"bob"},"body":"second","createdAt":"2026-01-03T09:00:00Z"}
	]}`)

	got, err := parseComments(data)
	if err != nil {
		t.Fatalf("parseComments returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comments, want 2", len(got))
	}
	if got[0].Author != "alice" || got[0].Body != "first" {
		t.Errorf("comment[0] = %+v, want alice/first", got[0])
	}
	wantTime := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	if !got[0].CreatedAt.Equal(wantTime) {
		t.Errorf("comment[0].CreatedAt = %v, want %v", got[0].CreatedAt, wantTime)
	}
	// Ordering must be preserved (chronological as gh returns).
	if !got[0].CreatedAt.Before(got[1].CreatedAt) {
		t.Errorf("comments not in chronological order: %v then %v", got[0].CreatedAt, got[1].CreatedAt)
	}
}

func TestParseCommentsEmpty(t *testing.T) {
	t.Parallel()

	got, err := parseComments([]byte(`{"comments":[]}`))
	if err != nil {
		t.Fatalf("parseComments returned error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for empty comments", got)
	}
}

func TestParseLinkedPRs(t *testing.T) {
	t.Parallel()

	data := []byte(`{"timelineItems":[
		{"__typename":"CrossReferencedEvent","source":{"__typename":"PullRequest","url":"https://github.com/o/r/pull/1"}},
		{"__typename":"ReferencedEvent","source":{"__typename":"PullRequest","url":"https://github.com/o/r/pull/2"}},
		{"__typename":"CrossReferencedEvent","source":{"__typename":"Issue","url":"https://github.com/o/r/issues/9"}},
		{"__typename":"LabeledEvent"},
		{"__typename":"CrossReferencedEvent","source":{"__typename":"PullRequest","url":"https://github.com/o/r/pull/1"}}
	]}`)

	got := parseLinkedPRs(data)
	want := []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/pull/2"}
	if len(got) != len(want) {
		t.Fatalf("parseLinkedPRs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("parseLinkedPRs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseLinkedPRsMalformed(t *testing.T) {
	t.Parallel()

	if got := parseLinkedPRs([]byte(`not json`)); got != nil {
		t.Errorf("parseLinkedPRs(malformed) = %v, want nil (best-effort)", got)
	}
}

func TestNormalizeState(t *testing.T) {
	t.Parallel()

	tests := map[string]string{"OPEN": "open", "CLOSED": "closed", " Open ": "open", "": ""}
	for in, want := range tests {
		if got := normalizeState(in); got != want {
			t.Errorf("normalizeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url      string
		wantRepo string
		wantOK   bool
	}{
		{"git@github.com:owner/repo.git", "owner/repo", true},
		{"git@github.com:owner/repo", "owner/repo", true},
		{"https://github.com/owner/repo.git", "owner/repo", true},
		{"https://github.com/owner/repo", "owner/repo", true},
		{"ssh://git@github.com/owner/repo.git", "owner/repo", true},
		{"http://github.com/owner/repo", "owner/repo", true},
		{"", "", false},
		{"not-a-url", "", false},
		{"https://github.com/", "", false},
	}
	for _, tt := range tests {
		got, ok := parseRemoteURL(tt.url)
		if ok != tt.wantOK || got != tt.wantRepo {
			t.Errorf("parseRemoteURL(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.wantRepo, tt.wantOK)
		}
	}
}

func TestParseGitConfigOrigin(t *testing.T) {
	t.Parallel()

	cfg := `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:upstream/repo.git
[remote "origin"]
	url = git@github.com:owner/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	got, ok := parseGitConfigOrigin(cfg)
	if !ok || got != "git@github.com:owner/repo.git" {
		t.Errorf("parseGitConfigOrigin = (%q, %v), want origin url", got, ok)
	}

	if _, ok := parseGitConfigOrigin("[core]\n\tbare = false\n"); ok {
		t.Error("parseGitConfigOrigin with no origin should report ok=false")
	}
}
