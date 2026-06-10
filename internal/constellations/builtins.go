package constellations

import (
	"context"
	"fmt"
	"sort"
)

// Operator is a Go-implemented builtin node. It receives the live Runtime (for
// store/git access), the run State (read its nebula/nodes, mutate cost/cycle),
// and the node's rendered inputs, and returns outputs recorded under the node's
// ID. Operators register themselves from init() into a package-level registry.
type Operator func(ctx context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error)

// operatorRegistry maps builtin operator names to implementations. It is
// populated only from init() functions (single-threaded program startup), so it
// needs no lock.
var operatorRegistry = map[string]Operator{}

// registerOperator adds an operator under name, panicking on a duplicate so a
// collision surfaces at startup rather than as a silent shadow.
func registerOperator(name string, op Operator) {
	if _, exists := operatorRegistry[name]; exists {
		panic(fmt.Sprintf("constellations: operator %q already registered", name))
	}
	operatorRegistry[name] = op
}

// lookupOperator returns the operator registered under name.
func lookupOperator(name string) (Operator, bool) {
	op, ok := operatorRegistry[name]
	return op, ok
}

// OperatorNames returns the registered operator names, sorted. Used by doctor/
// validation surfaces to report the available builtins.
func OperatorNames() []string {
	names := make([]string, 0, len(operatorRegistry))
	for n := range operatorRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func init() {
	registerOperator("render_seed_prompt", opRenderSeedPrompt)
	registerOperator("render_fix_prompt", opRenderFixPrompt)
	registerOperator("persist_phases", opPersistPhases)
	registerOperator("master_review_decision", opMasterReviewDecision)
	registerOperator("reviewer_decision", opReviewerDecision)
	registerOperator(opCommitName, opCommit)
	registerOperator("notify_human", opNotifyHuman)
	registerOperator("fail_run", opFailRun)
	registerOperator("verify_test", opVerify("test"))
	registerOperator("verify_lint", opVerify("lint"))
	registerOperator("verify_build", opVerify("build"))
	registerOperator(opMergeAttemptName, opMergeAttempt)
	registerOperator(opFulfillEntanglementsName, opFulfillEntanglements)
	registerOperator(opRenderConflictContextName, opRenderConflictContext)
	registerOperator(opConflictResolutionDecisionName, opConflictResolutionDecision)
	registerOperator(opEmitConflictTelemetryName, opEmitConflictTelemetry)
	registerOperator(opGitopsPushName, opGitopsPush)
	registerOperator(opGHOpenPRName, opGHOpenPR)
}

// Backend-neutral structured-output contracts. A star declares output_schema =
// "<name>"; dispatchStar resolves the bytes here and hands them to the invoker,
// which enforces them via the backend's structured-output mechanism (Claude's
// --json-schema today; any coder that consumes JSON Schema in future). The same
// names key the consumer operators' typed validation, so producer and consumer
// can never drift. JSON Schema is the lingua franca: every major LLM
// structured-output mechanism (Anthropic --json-schema, OpenAI response_format,
// Gemini responseSchema) consumes it, so the contract is portable by
// construction. additionalProperties:false keeps the emitted object aligned with
// the consumers' DisallowUnknownFields decoding.

// phasesSchemaName identifies the architect → persist_phases contract. The
// reviewer and master-review schema names are defined alongside their consumers
// (reviewerSchemaName, masterReviewSchemaName) and reused as registry keys.
const phasesSchemaName = "phases-v1"

const phasesSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["phases"],
  "properties": {
    "phases": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "title", "body"],
        "properties": {
          "id": {"type": "string", "minLength": 1},
          "title": {"type": "string", "minLength": 1},
          "body": {"type": "string", "minLength": 1},
          "frontmatter_toml": {"type": "string"}
        }
      }
    }
  }
}`

const reviewerDecisionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict"],
  "properties": {
    "verdict": {"type": "string", "enum": ["approve", "request_changes"]},
    "comments": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["severity", "detail"],
        "properties": {
          "severity": {"type": "string", "enum": ["critical", "major", "minor"]},
          "detail": {"type": "string", "minLength": 1}
        }
      }
    }
  }
}`

const masterReviewDecisionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "score"],
  "properties": {
    "verdict": {"type": "string", "enum": ["approve", "request_changes", "abandon"]},
    "score": {"type": "integer", "minimum": 0, "maximum": 100},
    "reasons": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["category", "detail"],
        "properties": {
          "category": {"type": "string", "enum": ["correctness", "tests", "safety", "style", "scope"]},
          "detail": {"type": "string", "minLength": 1}
        }
      }
    },
    "suggestions": {"type": "array", "items": {"type": "string"}},
    "blocker": {"type": "string"}
  }
}`

// stageSchemas maps a registered schema name to its JSON Schema bytes.
var stageSchemas = map[string][]byte{
	phasesSchemaName:       []byte(phasesSchema),
	reviewerSchemaName:     []byte(reviewerDecisionSchema),
	masterReviewSchemaName: []byte(masterReviewDecisionSchema),
}

// SchemaByName returns the JSON Schema bytes registered under name. The bool is
// false for an unknown name, which dispatchStar surfaces as a clear authoring
// error rather than silently running unstructured.
func SchemaByName(name string) ([]byte, bool) {
	s, ok := stageSchemas[name]
	return s, ok
}
