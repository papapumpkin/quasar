// Package constellations implements the constellation runtime: the engine that
// walks a constellation DAG against a nebula, dispatching each node (star,
// constellation, phase_iterator, builtin), evaluating edge `when:` guards, and
// persisting state after every transition for crash-safe resume.
//
// The runtime is deliberately thin: it wires together primitives that already
// exist elsewhere. Edge guards are evaluated with the artifacts expression
// mini-language; the LLM seam is agent.Invoker; commits go through
// gitops.Client (which threads the repo's [pre_commit] config); nebula state is
// read from fabric.NebulaStore; and run rows persist via
// fabric.ConstellationRunStore.
package constellations

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// State is the runtime evaluation context for a constellation run. It is what
// edge `when:` expressions evaluate against. State accumulates as nodes
// complete: each node's outputs land under Nodes[nodeID]. State is serialized
// to the dag_state_toml column after every transition so an interrupted run can
// be resumed exactly where it stopped.
type State struct {
	// Inputs are constellation-level inputs. The multi-repo model uses the
	// nebula as the universal input, so this is usually empty; it is retained
	// for forward compatibility and parity with the expression namespace.
	Inputs map[string]any `toml:"inputs,omitempty"`
	// Nodes holds each completed node's outputs, keyed by node ID. Expressions
	// reference these as e.g. `nodes.review.approved`.
	Nodes map[string]map[string]any `toml:"nodes"`
	// Nebula is a denormalized snapshot of the nebula row this run targets.
	Nebula NebulaSnapshot `toml:"nebula"`
	// Cycle counts master-review cycles (or DAG iteration count).
	Cycle int `toml:"cycle"`
	// Meta carries run-wide bookkeeping surfaced to expressions and the TUI.
	Meta MetaSnapshot `toml:"meta"`
}

// NebulaSnapshot is the subset of a nebula row the runtime exposes to
// expressions. It is a snapshot taken at Fire time (plus phase status updates as
// the run progresses), not a live view, so resume is deterministic.
type NebulaSnapshot struct {
	ID      string          `toml:"id"`
	Name    string          `toml:"name"`
	Source  string          `toml:"source"`
	Status  string          `toml:"status"`
	Context string          `toml:"context"`
	Phases  []PhaseSnapshot `toml:"phases"`
}

// PhaseSnapshot is the per-phase projection a phase_iterator fans out over and
// that expressions inspect (e.g. `nebula.phases`).
type PhaseSnapshot struct {
	ID     string `toml:"id"`
	Seq    int    `toml:"seq"`
	Title  string `toml:"title"`
	Status string `toml:"status"`
}

// MetaSnapshot is run-wide bookkeeping. TotalCostUSD accumulates across every
// star invocation; expressions can gate on it (e.g. budget guards). The
// canonical run ID lives on the constellation_runs row (a string), so it is not
// duplicated here.
type MetaSnapshot struct {
	TotalCostUSD float64 `toml:"total_cost_usd"`
	RunStartedAt int64   `toml:"run_started_at"`
}

// NewState builds the initial run State from a nebula snapshot. Nodes starts
// empty and is populated as the DAG walk progresses.
func NewState(neb NebulaSnapshot, startedAt int64) *State {
	return &State{
		Inputs: map[string]any{},
		Nodes:  map[string]map[string]any{},
		Nebula: neb,
		Meta:   MetaSnapshot{RunStartedAt: startedAt},
	}
}

// SnapshotNebula projects a fully loaded nebula into the denormalized form the
// runtime evaluates against.
func SnapshotNebula(n *fabric.Nebula) NebulaSnapshot {
	phases := make([]PhaseSnapshot, 0, len(n.Phases))
	for _, p := range n.Phases {
		phases = append(phases, PhaseSnapshot{
			ID:     p.ID,
			Seq:    p.Seq,
			Title:  p.Title,
			Status: p.Status,
		})
	}
	return NebulaSnapshot{
		ID:      n.ID,
		Name:    n.Name,
		Source:  n.SourceName,
		Status:  n.Status,
		Context: n.ContextTOML,
		Phases:  phases,
	}
}

// RecordNode stores a completed node's outputs under its ID, so later edge
// guards and node inputs can reference them. A nil output map is normalized to
// an empty map so `nodes.<id>` is a present-but-empty namespace rather than
// nil.
func (s *State) RecordNode(nodeID string, output map[string]any) {
	if s.Nodes == nil {
		s.Nodes = map[string]map[string]any{}
	}
	if output == nil {
		output = map[string]any{}
	}
	s.Nodes[nodeID] = output
}

// ExprState renders State into the flat map the artifacts expression evaluator
// walks with dot-notation. Keys mirror the expression namespace: `nodes.*`,
// `nebula.*`, `inputs.*`, `cycle`, `meta.*`. Missing lookups yield nil (falsy),
// never an error, matching artifacts.State semantics.
func (s *State) ExprState() artifacts.State {
	nodes := make(map[string]any, len(s.Nodes))
	for id, out := range s.Nodes {
		nodes[id] = map[string]any(out)
	}
	phases := make([]any, 0, len(s.Nebula.Phases))
	for _, p := range s.Nebula.Phases {
		phases = append(phases, map[string]any{
			"id":     p.ID,
			"seq":    p.Seq,
			"title":  p.Title,
			"status": p.Status,
		})
	}
	return artifacts.State{
		"inputs": mapOrEmpty(s.Inputs),
		"nodes":  nodes,
		"nebula": map[string]any{
			"id":      s.Nebula.ID,
			"name":    s.Nebula.Name,
			"source":  s.Nebula.Source,
			"status":  s.Nebula.Status,
			"context": s.Nebula.Context,
			"phases":  phases,
		},
		"cycle": s.Cycle,
		"meta": map[string]any{
			"total_cost_usd": s.Meta.TotalCostUSD,
			"run_started_at": s.Meta.RunStartedAt,
		},
	}
}

// mapOrEmpty returns m, or an empty map when m is nil, so expression namespaces
// are always traversable.
func mapOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// MarshalState produces the dag_state_toml column value for resume. It round-
// trips with UnmarshalState.
func MarshalState(s *State) (string, error) {
	b, err := toml.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal constellation state: %w", err)
	}
	return string(b), nil
}

// UnmarshalState restores a State from a dag_state_toml column value.
func UnmarshalState(data string) (*State, error) {
	var s State
	if err := toml.Unmarshal([]byte(data), &s); err != nil {
		return nil, fmt.Errorf("unmarshal constellation state: %w", err)
	}
	if s.Nodes == nil {
		s.Nodes = map[string]map[string]any{}
	}
	if s.Inputs == nil {
		s.Inputs = map[string]any{}
	}
	return &s, nil
}
