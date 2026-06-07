package constellations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// masterReviewSchemaName is the schema version the master-reviewer star emits
// and this operator validates against. It is surfaced in error messages so a
// malformed payload points the author at the contract it broke.
const masterReviewSchemaName = "master-review-decision-v1"

// validVerdicts and validCategories enumerate the closed sets the schema allows.
// A value outside either set is a field-path error, not a silently tolerated
// extra.
var (
	validVerdicts   = map[string]bool{"approve": true, "request_changes": true, "abandon": true}
	validCategories = map[string]bool{"correctness": true, "tests": true, "safety": true, "style": true, "scope": true}
)

// verdictToDecision maps a rubric verdict onto the routing vocabulary the
// master-review constellation's edges gate on (ship / fix / escalate). Keeping
// the rubric's verdict language separate from the routing decision lets the star
// speak the rubric's terms while the constellation routes on stable tokens.
var verdictToDecision = map[string]string{
	"approve":         "ship",
	"request_changes": "fix",
	"abandon":         "escalate",
}

// masterReviewDecision is the typed form of the master-reviewer star's JSON
// output (schema master-review-decision-v1). Pointers/slices distinguish a
// field that was supplied (even if empty) from one that was omitted, so missing
// required fields are reported with their path.
type masterReviewDecision struct {
	Verdict     *string              `json:"verdict"`
	Score       *int                 `json:"score"`
	Reasons     []masterReviewReason `json:"reasons"`
	Suggestions []string             `json:"suggestions"`
	Blocker     *string              `json:"blocker"`
}

// masterReviewReason is one scored observation in a decision.
type masterReviewReason struct {
	Category string `json:"category"`
	Detail   string `json:"detail"`
}

// opMasterReviewDecision parses the master-reviewer star's JSON output (passed
// as args["output"]) into a typed decision, validates it against schema
// master-review-decision-v1, and returns the fields the constellation routes on.
//
// Output: {"decision": ship|fix|escalate, "verdict": <raw>, "score": int,
// "feedback": <suggestions joined>, "blocker": <string|"">}.
//
// It errors — with a field-path message — when the output is not valid JSON or
// violates the schema (unknown verdict, out-of-range score, bad reason category,
// or a missing required field). The constellation gates on `decision`, e.g.
// `when = "nodes.decide.decision == 'ship'"`.
func opMasterReviewDecision(_ context.Context, _ *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	raw, ok := args["output"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("master_review_decision: missing string input %q", "output")
	}

	dec, err := parseMasterReviewDecision(raw)
	if err != nil {
		return nil, err
	}

	verdict := *dec.Verdict
	return map[string]any{
		"decision": verdictToDecision[verdict],
		"verdict":  verdict,
		"score":    *dec.Score,
		"feedback": strings.Join(dec.Suggestions, "\n"),
		"blocker":  derefOr(dec.Blocker, ""),
	}, nil
}

// parseMasterReviewDecision decodes and validates a raw payload against schema
// master-review-decision-v1, returning a field-path error on the first
// violation. It is split out so the validation rules are unit-testable without a
// runtime.
func parseMasterReviewDecision(raw string) (*masterReviewDecision, error) {
	var dec masterReviewDecision
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&dec); err != nil {
		return nil, fmt.Errorf("master_review_decision: parse %s: %w", masterReviewSchemaName, err)
	}
	if err := validateMasterReviewDecision(&dec); err != nil {
		return nil, err
	}
	return &dec, nil
}

// validateMasterReviewDecision enforces the schema's required fields and closed
// value sets. Every error names the offending field's path.
func validateMasterReviewDecision(dec *masterReviewDecision) error {
	if dec.Verdict == nil {
		return schemaErr("verdict", "required")
	}
	if !validVerdicts[*dec.Verdict] {
		return schemaErr("verdict", "must be approve, request_changes, or abandon (got %q)", *dec.Verdict)
	}
	if dec.Score == nil {
		return schemaErr("score", "required")
	}
	if *dec.Score < 0 || *dec.Score > 100 {
		return schemaErr("score", "must be in [0,100] (got %d)", *dec.Score)
	}
	for i, r := range dec.Reasons {
		if !validCategories[r.Category] {
			return schemaErr(fmt.Sprintf("reasons[%d].category", i),
				"must be correctness, tests, safety, style, or scope (got %q)", r.Category)
		}
	}
	if *dec.Verdict == "abandon" && strings.TrimSpace(derefOr(dec.Blocker, "")) == "" {
		return schemaErr("blocker", "required when verdict is abandon")
	}
	return nil
}

// schemaErr builds a uniform field-path error against the master-review schema.
func schemaErr(field, format string, args ...any) error {
	return fieldSchemaErr("master_review_decision", masterReviewSchemaName, field, format, args...)
}

// fieldSchemaErr formats the "<op>: <schema>: field "<field>": <msg>" error
// shared by the decision operators (master_review_decision, reviewer_decision),
// so their schema-violation messages stay uniform.
func fieldSchemaErr(op, schema, field, format string, args ...any) error {
	return fmt.Errorf("%s: %s: field %q: %s", op, schema, field, fmt.Sprintf(format, args...))
}

// derefOr returns *p, or def when p is nil.
func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
