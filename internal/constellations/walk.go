package constellations

import (
	"fmt"
	"os"
	"strings"

	"github.com/papapumpkin/quasar/internal/artifacts"
)

// entryNode returns the constellation's start node: the first node that is not
// the target of any edge. If every node is targeted (a cycle with no clear
// entry), the first declared node is used. An empty constellation is an error.
func entryNode(con *artifacts.Constellation) (*artifacts.ConstellationNode, error) {
	if len(con.Nodes) == 0 {
		return nil, fmt.Errorf("constellations: %q has no nodes", con.Name)
	}
	targeted := make(map[string]bool, len(con.Edges))
	for _, e := range con.Edges {
		targeted[e.To] = true
	}
	for i := range con.Nodes {
		if !targeted[con.Nodes[i].ID] {
			return &con.Nodes[i], nil
		}
	}
	return &con.Nodes[0], nil
}

// findNode returns the node with the given ID, or nil if absent.
func findNode(con *artifacts.Constellation, id string) *artifacts.ConstellationNode {
	for i := range con.Nodes {
		if con.Nodes[i].ID == id {
			return &con.Nodes[i]
		}
	}
	return nil
}

// nodeIndex returns the declaration index of the node with the given ID, or -1
// when no node has it (e.g. a reserved terminal target, which is not a node).
func nodeIndex(con *artifacts.Constellation, id string) int {
	for i := range con.Nodes {
		if con.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}

// isBackEdge reports whether a transition from -> to re-enters the DAG at an
// already-declared node — a loop iteration. A reserved terminal target (_done,
// _failed, …) is not a node and is never a back-edge.
//
// Back-edges are the sole signal the runtime uses to advance a run's cycle
// counter, so the declarative cycle cap (meta.max_cycles) is enforced without
// any hardcoded Go constant: an author places a loop's entry node earlier than
// the node that routes back to it, and each return trip counts as one cycle. A
// self-loop (from == to) also counts. Unknown endpoints yield false rather than
// silently miscounting.
//
// LIMITATION — this infers loop topology positionally (target declared at or
// before the source). It is correct for the single master-review loop today, but
// a legitimate forward edge into an earlier-declared join node (e.g. a shared
// cleanup) would be miscounted as a back-edge and inflate the run's single
// st.Cycle. TODO: when a second loop or a phase_iterator loop lands, make
// back-edges explicit (an edge attribute such as `loop = true`, or a marked loop
// head) so cycle counting is intentional rather than positional.
func isBackEdge(con *artifacts.Constellation, from, to string) bool {
	if artifacts.IsTerminal(to) {
		return false
	}
	fromIdx, toIdx := nodeIndex(con, from), nodeIndex(con, to)
	if fromIdx < 0 || toIdx < 0 {
		return false
	}
	return toIdx <= fromIdx
}

// nextTarget evaluates the outgoing edges of `from` in declaration order and
// returns the `to` of the first whose `when` guard is truthy (a nil guard is
// unconditional). A node with no outgoing edges terminates the run (_done). A
// node whose every guard is falsy returns ErrNoEdgeMatched — a dead end.
func nextTarget(con *artifacts.Constellation, from string, st artifacts.State) (string, error) {
	hasEdge := false
	for _, e := range con.Edges {
		if e.From != from {
			continue
		}
		hasEdge = true
		if e.When == nil {
			return e.To, nil
		}
		v, err := e.When.Eval(st)
		if err != nil {
			return "", fmt.Errorf("constellations: eval edge %s->%s when: %w", e.From, e.To, err)
		}
		if truthyValue(v) {
			return e.To, nil
		}
	}
	if !hasEdge {
		return artifacts.TermDone, nil
	}
	return "", fmt.Errorf("%w: node %q", ErrNoEdgeMatched, from)
}

// evalInputs renders a node's input expression templates against State into a
// plain argument map handed to operators and star prompts.
func evalInputs(node *artifacts.ConstellationNode, st artifacts.State) (map[string]any, error) {
	args := make(map[string]any, len(node.Inputs))
	for k, expr := range node.Inputs {
		v, err := expr.Eval(st)
		if err != nil {
			return nil, fmt.Errorf("constellations: eval input %q of node %q: %w", k, node.ID, err)
		}
		args[k] = v
	}
	return args, nil
}

// userPrompt derives the LLM user prompt for a star node. An explicit `prompt`
// input wins; otherwise the nebula's context is used as the working brief.
func userPrompt(args map[string]any, st *State) string {
	if p, ok := args["prompt"].(string); ok && strings.TrimSpace(p) != "" {
		return p
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Nebula: %s\n\n", st.Nebula.Name)
	if st.Nebula.Context != "" {
		b.WriteString(st.Nebula.Context)
		b.WriteString("\n")
	}
	return b.String()
}

// snapshotSource returns the constellation's on-disk TOML bytes for the run
// snapshot. When the source path is unavailable (e.g. an in-memory test
// constellation), it returns nil — the snapshot is for audit/versioning and is
// not required for execution.
func snapshotSource(con *artifacts.Constellation) []byte {
	if con.SourcePath == "" {
		return nil
	}
	data, err := os.ReadFile(con.SourcePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: snapshot %q: %v\n", con.SourcePath, err)
		return nil
	}
	return data
}

// truthyValue mirrors the expression evaluator's truthiness so edge guards and
// raw boolean inputs agree on what counts as true.
func truthyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	default:
		return true
	}
}

// isTerminalState reports whether a run state is terminal (no further steps).
func isTerminalState(state string) bool {
	switch state {
	case StateDone, StateFailed, StateAwaitingHuman, StatePaused, "crashed", "killed":
		return true
	default:
		return false
	}
}

// terminalState maps a reserved edge target onto the persisted run state.
func terminalState(target string) string {
	switch target {
	case artifacts.TermDone:
		return StateDone
	case artifacts.TermFailed:
		return StateFailed
	case artifacts.TermAwaitingHuman:
		return StateAwaitingHuman
	case artifacts.TermPaused:
		return StatePaused
	default:
		return StateDone
	}
}
