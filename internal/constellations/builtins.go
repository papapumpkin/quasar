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
