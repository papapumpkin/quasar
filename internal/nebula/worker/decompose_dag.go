// Package nebula provides decomposition graph surgery for replacing a struggling
// phase with multiple sub-phases in the live DAG.
package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/dag"
)

// DecomposeOp describes a decomposition operation to be applied to the DAG.
type DecomposeOp struct {
	OriginalPhaseID string
	SubPhases       []SubPhaseEntry
}

// SubPhaseEntry holds the information needed to insert a sub-phase into the DAG.
type SubPhaseEntry struct {
	Spec     PhaseSpec // parsed phase spec from the architect
	Body     string    // markdown body for the phase file
	Filename string    // filename for the phase file (e.g., "03a-original-id-part-1.md")
}

// ApplyDecomposition performs atomic graph surgery on the DAG:
//  1. Records the original phase's predecessors (DepsFor) and direct successors.
//  2. Removes the original phase node from the DAG.
//  3. Adds each sub-phase as a new node.
//  4. Wires each sub-phase to depend on the original phase's predecessors.
//  5. Wires the original phase's direct successors to depend on ALL sub-phases.
//  6. Wires inter-sub-phase dependencies declared in SubPhaseEntry.Spec.DependsOn.
//  7. Validates no cycles were introduced (DAG.AddEdge returns dag.ErrCycle).
//
// The function returns the list of sub-phase IDs that were added.
// On error, partial mutations may have occurred — the caller should treat the DAG as corrupt.
func ApplyDecomposition(d *dag.DAG, op DecomposeOp) ([]string, error) {
	if len(op.SubPhases) == 0 {
		return nil, fmt.Errorf("decompose %s: no sub-phases provided", op.OriginalPhaseID)
	}

	// Validate the original phase exists.
	if d.Node(op.OriginalPhaseID) == nil {
		return nil, fmt.Errorf("decompose %s: %w", op.OriginalPhaseID, dag.ErrNodeNotFound)
	}

	// Check for duplicate sub-phase IDs.
	seen := make(map[string]bool, len(op.SubPhases))
	for _, sp := range op.SubPhases {
		if seen[sp.Spec.ID] {
			return nil, fmt.Errorf("decompose %s: duplicate sub-phase ID %q",
				op.OriginalPhaseID, sp.Spec.ID)
		}
		if d.Node(sp.Spec.ID) != nil && sp.Spec.ID != op.OriginalPhaseID {
			return nil, fmt.Errorf("decompose %s: sub-phase ID %q already exists in DAG",
				op.OriginalPhaseID, sp.Spec.ID)
		}
		seen[sp.Spec.ID] = true
	}

	// Step 1: Capture the original phase's predecessors and direct successors
	// before removing it.
	predecessors := d.DepsFor(op.OriginalPhaseID)
	successors := directDependents(d, op.OriginalPhaseID)

	// Step 2: Remove the original phase from the DAG.
	if err := d.Remove(op.OriginalPhaseID); err != nil {
		return nil, fmt.Errorf("decompose %s: removing original: %w", op.OriginalPhaseID, err)
	}

	// Step 3: Add each sub-phase as a new node.
	subIDs := make([]string, 0, len(op.SubPhases))
	for _, sp := range op.SubPhases {
		if err := d.AddNode(sp.Spec.ID, sp.Spec.Priority); err != nil {
			return nil, fmt.Errorf("decompose %s: adding sub-phase %s: %w",
				op.OriginalPhaseID, sp.Spec.ID, err)
		}
		subIDs = append(subIDs, sp.Spec.ID)
	}

	// Step 4: Wire each sub-phase to depend on the original phase's predecessors.
	for _, subID := range subIDs {
		for _, pred := range predecessors {
			if err := d.AddEdge(subID, pred); err != nil {
				return nil, fmt.Errorf("decompose %s: wiring predecessor %s → %s: %w",
					op.OriginalPhaseID, subID, pred, err)
			}
		}
	}

	// Step 5: Wire the original phase's successors to depend on ALL sub-phases.
	for _, succ := range successors {
		for _, subID := range subIDs {
			if err := d.AddEdge(succ, subID); err != nil {
				return nil, fmt.Errorf("decompose %s: wiring successor %s → %s: %w",
					op.OriginalPhaseID, succ, subID, err)
			}
		}
	}

	// Step 6: Wire inter-sub-phase dependencies from DependsOn fields.
	for _, sp := range op.SubPhases {
		for _, dep := range sp.Spec.DependsOn {
			// Only wire dependencies between sub-phases themselves.
			if !seen[dep] {
				continue
			}
			if err := d.AddEdge(sp.Spec.ID, dep); err != nil {
				return nil, fmt.Errorf("decompose %s: inter-sub-phase edge %s → %s: %w",
					op.OriginalPhaseID, sp.Spec.ID, dep, err)
			}
		}
	}

	return subIDs, nil
}

// ApplyDecompositionToNebula updates the Nebula's in-memory phase registry and writes
// the sub-phase files to disk. It calls ApplyDecomposition for the DAG mutation and
// then updates PhasesByID and Phases to reflect the new sub-phases.
// It also removes the original phase's entry from the phases index.
func ApplyDecompositionToNebula(neb *Nebula, d *dag.DAG, op DecomposeOp, phasesByID map[string]*PhaseSpec) ([]string, error) {
	subIDs, err := ApplyDecomposition(d, op)
	if err != nil {
		return nil, err
	}

	// Write each sub-phase file to disk.
	for _, sp := range op.SubPhases {
		spec := sp.Spec
		spec.Body = sp.Body

		data, marshalErr := MarshalPhaseFile(spec)
		if marshalErr != nil {
			return subIDs, fmt.Errorf("decompose %s: marshaling sub-phase %s: %w",
				op.OriginalPhaseID, sp.Spec.ID, marshalErr)
		}

		path := filepath.Join(neb.Dir, sp.Filename)
		if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
			return subIDs, fmt.Errorf("decompose %s: writing sub-phase file %s: %w",
				op.OriginalPhaseID, sp.Filename, writeErr)
		}
	}

	// Add sub-phases to the nebula's in-memory phase list and index.
	for _, sp := range op.SubPhases {
		spec := sp.Spec
		spec.Body = sp.Body
		spec.SourceFile = sp.Filename
		neb.Phases = append(neb.Phases, spec)
		phasesByID[spec.ID] = &neb.Phases[len(neb.Phases)-1]
	}

	// Remove the original phase from the index (leave the file on disk for traceability).
	delete(phasesByID, op.OriginalPhaseID)

	// Annotate the original phase file with decomposed = true in frontmatter.
	annotateDecomposed(neb, op.OriginalPhaseID)

	return subIDs, nil
}

// directDependents returns the IDs of nodes that directly depend on the given node.
// Results are sorted alphabetically for deterministic output.
func directDependents(d *dag.DAG, id string) []string {
	var dependents []string
	for _, nodeID := range d.Nodes() {
		if nodeID == id {
			continue
		}
		for _, dep := range d.DepsFor(nodeID) {
			if dep == id {
				dependents = append(dependents, nodeID)
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents
}

// annotateDecomposed reads the original phase file and prepends a decomposed = true
// field to its frontmatter. If the file cannot be found or read, the error is
// silently ignored (non-critical for correctness).
func annotateDecomposed(neb *Nebula, phaseID string) {
	// Find the original phase's source file.
	var sourceFile string
	for i := range neb.Phases {
		if neb.Phases[i].ID == phaseID {
			sourceFile = neb.Phases[i].SourceFile
			break
		}
	}
	if sourceFile == "" {
		return
	}

	path := filepath.Join(neb.Dir, sourceFile)
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}

	s := string(content)
	// Insert decomposed = true after the opening +++ line.
	const marker = "+++\n"
	idx := strings.Index(s, marker)
	if idx < 0 {
		return
	}
	annotated := s[:idx+len(marker)] + "decomposed = true\n" + s[idx+len(marker):]
	_ = os.WriteFile(path, []byte(annotated), 0o644)
}
