package constellations

import (
	"context"
	"strings"
	"testing"
)

// TestOpReviewerDecision covers the happy path: a valid reviewer-decision-v1
// payload is parsed into the {verdict, approved, comments} fields the
// coder-reviewer constellation routes on.
func TestOpReviewerDecision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		output       string
		wantVerdict  string
		wantApproved bool
		wantComments string
		wantCount    int
	}{
		{
			name:         "approve with no comments",
			output:       `{"verdict":"approve","comments":[]}`,
			wantVerdict:  "approve",
			wantApproved: true,
			wantComments: "",
			wantCount:    0,
		},
		{
			name:         "approve with omitted comments",
			output:       `{"verdict":"approve"}`,
			wantVerdict:  "approve",
			wantApproved: true,
			wantComments: "",
			wantCount:    0,
		},
		{
			name:         "request_changes joins comments into the revision brief",
			output:       `{"verdict":"request_changes","comments":[{"severity":"major","detail":"handle nil input"},{"severity":"minor","detail":"rename x"}]}`,
			wantVerdict:  "request_changes",
			wantApproved: false,
			wantComments: "[major] handle nil input\n[minor] rename x",
			wantCount:    2,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := opReviewerDecision(context.Background(), nil, nil, map[string]any{"output": tc.output})
			if err != nil {
				t.Fatalf("opReviewerDecision: %v", err)
			}
			if got := out["verdict"]; got != tc.wantVerdict {
				t.Errorf("verdict = %v, want %q", got, tc.wantVerdict)
			}
			if got := out["approved"]; got != tc.wantApproved {
				t.Errorf("approved = %v, want %v", got, tc.wantApproved)
			}
			if got := out["comments"]; got != tc.wantComments {
				t.Errorf("comments = %q, want %q", got, tc.wantComments)
			}
			if got := out["comments_count"]; got != tc.wantCount {
				t.Errorf("comments_count = %v, want %d", got, tc.wantCount)
			}
		})
	}
}

// TestOpReviewerDecisionErrors covers malformed input: a missing string input,
// invalid JSON, an unknown field, and every schema violation must produce a
// field-path error naming the offending field.
func TestOpReviewerDecisionErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		args      map[string]any
		wantSubst string
	}{
		{
			name:      "missing input",
			args:      map[string]any{},
			wantSubst: `missing string input "output"`,
		},
		{
			name:      "empty input",
			args:      map[string]any{"output": "   "},
			wantSubst: `missing string input "output"`,
		},
		{
			name:      "invalid json",
			args:      map[string]any{"output": "not json"},
			wantSubst: "parse reviewer-decision-v1",
		},
		{
			name:      "unknown field",
			args:      map[string]any{"output": `{"verdict":"approve","score":90}`},
			wantSubst: "parse reviewer-decision-v1",
		},
		{
			name:      "missing verdict",
			args:      map[string]any{"output": `{"comments":[]}`},
			wantSubst: `field "verdict": required`,
		},
		{
			name:      "bad verdict",
			args:      map[string]any{"output": `{"verdict":"abandon"}`},
			wantSubst: `field "verdict": must be approve or request_changes`,
		},
		{
			name:      "bad comment severity",
			args:      map[string]any{"output": `{"verdict":"request_changes","comments":[{"severity":"blocker","detail":"x"}]}`},
			wantSubst: `field "comments[0].severity"`,
		},
		{
			name:      "empty comment detail",
			args:      map[string]any{"output": `{"verdict":"request_changes","comments":[{"severity":"major","detail":"  "}]}`},
			wantSubst: `field "comments[0].detail": required`,
		},
		{
			name:      "request_changes without comments",
			args:      map[string]any{"output": `{"verdict":"request_changes","comments":[]}`},
			wantSubst: `field "comments": required when verdict is request_changes`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := opReviewerDecision(context.Background(), nil, nil, tc.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubst)
			}
		})
	}
}
