package constellations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// reviewerSchemaName is the schema version the reviewer star emits and this
// operator validates against. It is surfaced in error messages so a malformed
// payload points the author at the contract it broke.
const reviewerSchemaName = "reviewer-decision-v1"

// validReviewerVerdicts and validReviewerSeverities enumerate the closed sets
// the schema allows. The reviewer's vocabulary is deliberately narrower than the
// master reviewer's: it only approves or requests changes (no score, no
// abandon), so it gets its own operator rather than overloading
// master_review_decision.
var (
	validReviewerVerdicts   = map[string]bool{"approve": true, "request_changes": true}
	validReviewerSeverities = map[string]bool{"critical": true, "major": true, "minor": true}
)

// reviewerDecision is the typed form of the reviewer star's JSON output (schema
// reviewer-decision-v1). Verdict is a pointer so a field that was omitted is
// distinguishable from one supplied empty, letting a missing required field be
// reported with its path.
type reviewerDecision struct {
	Verdict  *string           `json:"verdict"`
	Comments []reviewerComment `json:"comments"`
}

// reviewerComment is one review note: a severity and the change it requests.
type reviewerComment struct {
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// opReviewerDecision parses the reviewer star's JSON output (passed as
// args["output"]) into a typed decision, validates it against schema
// reviewer-decision-v1, and returns the fields the coder-reviewer constellation
// routes on.
//
// Output: {"verdict": approve|request_changes, "approved": bool,
// "comments": <comments joined>, "comments_count": int}. The constellation gates
// the revise back-edge on `nodes.decide.verdict == 'request_changes'` and the
// done edge on `nodes.decide.approved`, and feeds `nodes.decide.comments` back to
// the coder as the revision brief.
//
// It errors — with a field-path message — when the output is not valid JSON or
// violates the schema (unknown verdict or severity, an empty comment detail, or a
// request_changes verdict with no comments).
func opReviewerDecision(_ context.Context, _ *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	raw, ok := args["output"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("reviewer_decision: missing string input %q", "output")
	}

	dec, err := parseReviewerDecision(raw)
	if err != nil {
		return nil, err
	}

	verdict := *dec.Verdict
	return map[string]any{
		"verdict":        verdict,
		"approved":       verdict == "approve",
		"comments":       joinReviewerComments(dec.Comments),
		"comments_count": len(dec.Comments),
	}, nil
}

// parseReviewerDecision decodes and validates a raw payload against schema
// reviewer-decision-v1, returning a field-path error on the first violation. It
// is split out so the validation rules are unit-testable without a runtime.
func parseReviewerDecision(raw string) (*reviewerDecision, error) {
	var dec reviewerDecision
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&dec); err != nil {
		return nil, fmt.Errorf("reviewer_decision: parse %s: %w", reviewerSchemaName, err)
	}
	if err := validateReviewerDecision(&dec); err != nil {
		return nil, err
	}
	return &dec, nil
}

// validateReviewerDecision enforces the schema's required fields and closed value
// sets. Every error names the offending field's path.
func validateReviewerDecision(dec *reviewerDecision) error {
	if dec.Verdict == nil {
		return reviewerSchemaErr("verdict", "required")
	}
	if !validReviewerVerdicts[*dec.Verdict] {
		return reviewerSchemaErr("verdict", "must be approve or request_changes (got %q)", *dec.Verdict)
	}
	for i, c := range dec.Comments {
		if !validReviewerSeverities[c.Severity] {
			return reviewerSchemaErr(fmt.Sprintf("comments[%d].severity", i),
				"must be critical, major, or minor (got %q)", c.Severity)
		}
		if strings.TrimSpace(c.Detail) == "" {
			return reviewerSchemaErr(fmt.Sprintf("comments[%d].detail", i), "required")
		}
	}
	if *dec.Verdict == "request_changes" && len(dec.Comments) == 0 {
		return reviewerSchemaErr("comments", "required when verdict is request_changes")
	}
	return nil
}

// reviewerSchemaErr builds a uniform field-path error against the reviewer schema.
func reviewerSchemaErr(field, format string, args ...any) error {
	return fieldSchemaErr("reviewer_decision", reviewerSchemaName, field, format, args...)
}

// joinReviewerComments renders the comment list into the single feedback string
// the coder consumes on the next revision, one "[severity] detail" per line.
func joinReviewerComments(comments []reviewerComment) string {
	if len(comments) == 0 {
		return ""
	}
	lines := make([]string, len(comments))
	for i, c := range comments {
		lines[i] = fmt.Sprintf("[%s] %s", c.Severity, c.Detail)
	}
	return strings.Join(lines, "\n")
}
