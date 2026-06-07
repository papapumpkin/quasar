package constellations

import (
	"context"
	"strings"
	"testing"
)

// TestOpMasterReviewDecision covers the happy path: a valid schema-v1 payload is
// parsed into the routing decision and the feedback the fix path consumes.
func TestOpMasterReviewDecision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		output       string
		wantDecision string
		wantFeedback string
	}{
		{
			name:         "approve maps to ship",
			output:       `{"verdict":"approve","score":92,"reasons":[],"suggestions":[],"blocker":null}`,
			wantDecision: "ship",
			wantFeedback: "",
		},
		{
			name:         "request_changes maps to fix with feedback",
			output:       `{"verdict":"request_changes","score":61,"reasons":[{"category":"tests","detail":"no test"}],"suggestions":["add a table test","handle nil input"],"blocker":null}`,
			wantDecision: "fix",
			wantFeedback: "add a table test\nhandle nil input",
		},
		{
			name:         "abandon maps to escalate",
			output:       `{"verdict":"abandon","score":10,"reasons":[{"category":"scope","detail":"rewrites engine"}],"suggestions":[],"blocker":"out of scope"}`,
			wantDecision: "escalate",
			wantFeedback: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := opMasterReviewDecision(context.Background(), nil, nil, map[string]any{"output": tc.output})
			if err != nil {
				t.Fatalf("opMasterReviewDecision: %v", err)
			}
			if got := out["decision"]; got != tc.wantDecision {
				t.Errorf("decision = %v, want %q", got, tc.wantDecision)
			}
			if got := out["feedback"]; got != tc.wantFeedback {
				t.Errorf("feedback = %q, want %q", got, tc.wantFeedback)
			}
		})
	}
}

// TestOpMasterReviewDecisionErrors covers malformed input: missing string input,
// invalid JSON, and every schema violation produces a field-path error.
func TestOpMasterReviewDecisionErrors(t *testing.T) {
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
			wantSubst: "parse master-review-decision-v1",
		},
		{
			name:      "unknown field",
			args:      map[string]any{"output": `{"verdict":"approve","score":90,"extra":true}`},
			wantSubst: "parse master-review-decision-v1",
		},
		{
			name:      "missing verdict",
			args:      map[string]any{"output": `{"score":90}`},
			wantSubst: `field "verdict": required`,
		},
		{
			name:      "bad verdict",
			args:      map[string]any{"output": `{"verdict":"ship","score":90}`},
			wantSubst: `field "verdict": must be approve`,
		},
		{
			name:      "missing score",
			args:      map[string]any{"output": `{"verdict":"approve"}`},
			wantSubst: `field "score": required`,
		},
		{
			name:      "score out of range",
			args:      map[string]any{"output": `{"verdict":"approve","score":150}`},
			wantSubst: `field "score": must be in [0,100]`,
		},
		{
			name:      "bad reason category",
			args:      map[string]any{"output": `{"verdict":"request_changes","score":50,"reasons":[{"category":"vibes","detail":"meh"}]}`},
			wantSubst: `field "reasons[0].category"`,
		},
		{
			name:      "abandon without blocker",
			args:      map[string]any{"output": `{"verdict":"abandon","score":10}`},
			wantSubst: `field "blocker": required when verdict is abandon`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := opMasterReviewDecision(context.Background(), nil, nil, tc.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubst)
			}
		})
	}
}
