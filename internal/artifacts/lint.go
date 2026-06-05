package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Severity classifies a lint Diagnostic. Errors always fail the lint; warnings
// fail only under --strict.
type Severity string

// Diagnostic severities.
const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Diagnostic is a single lint finding. Message carries the human-readable
// description and, where a source position is known, an embedded file:line:col
// prefix produced by the loader.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// LintOptions configures a lint pass.
type LintOptions struct {
	// SensorTypeKnown reports whether a sensor type name is registered with the
	// process sensor registry. When nil, sensor-type validation is skipped (the
	// artifacts package does not depend on the sensors registry itself).
	SensorTypeKnown func(string) bool
}

// Lint discovers and validates every artifact reachable from this loader's repo
// (per-repo overrides plus embedded defaults) and returns the findings. It does
// not stop at the first error: callers get the full picture in one pass.
//
// It checks schema validity (via the loaders), expression compilation, star
// references to unknown skills, constellation nodes referencing unknown stars or
// sub-constellations, edges referencing unknown nodes, closed loops with no path
// to a terminal node, inline-token guardrail violations, and (when configured)
// sensor instances referencing unknown types.
func (l *Loader) Lint(opts LintOptions) []Diagnostic {
	var diags []Diagnostic

	// Skills and stars. LoadStar resolves skills, so an unknown skill reference
	// surfaces here as a load error.
	for _, name := range l.names(dirSkills, extMD) {
		if _, err := l.LoadSkill(name); err != nil {
			diags = append(diags, errDiag(err))
		}
	}
	starNames := l.names(dirStars, extMD)
	starSet := toSet(starNames)
	for _, name := range starNames {
		if _, err := l.LoadStar(name); err != nil {
			diags = append(diags, errDiag(err))
		}
	}

	// Constellations.
	consNames := l.names(dirConstellations, extTOML)
	consSet := toSet(consNames)
	for _, name := range consNames {
		c, err := l.LoadConstellation(name)
		if err != nil {
			diags = append(diags, errDiag(err))
			continue
		}
		diags = append(diags, lintConstellation(c, starSet, consSet)...)
	}

	// Sensor instances (per-repo only; no embedded defaults).
	sensors, err := l.LoadAllSensorInstances()
	if err != nil {
		diags = append(diags, errDiag(err))
	}
	for _, s := range sensors {
		diags = append(diags, lintSensor(s, consSet, opts)...)
	}

	return diags
}

// lintConstellation validates a single loaded constellation's graph.
func lintConstellation(c *Constellation, starSet, consSet map[string]bool) []Diagnostic {
	var diags []Diagnostic
	nodeIDs := make(map[string]bool, len(c.Nodes))
	for _, n := range c.Nodes {
		nodeIDs[n.ID] = true
	}

	for _, n := range c.Nodes {
		switch n.Type {
		case NodeStar:
			if !starSet[n.Star] {
				diags = append(diags, errf(c.SourcePath, "node %q references unknown star %q", n.ID, n.Star))
			}
		case NodeConstellation:
			if !consSet[n.Ref] {
				diags = append(diags, errf(c.SourcePath, "node %q references unknown sub-constellation %q", n.ID, n.Ref))
			}
		case NodePhaseIterator:
			if ref := staticString(n.Inputs["sub_constellation"]); ref != "" && !consSet[ref] {
				diags = append(diags, errf(c.SourcePath, "node %q iterates unknown sub-constellation %q", n.ID, ref))
			}
		}
	}

	for _, e := range c.Edges {
		if !nodeIDs[e.From] {
			diags = append(diags, errf(c.SourcePath, "edge from unknown node %q", e.From))
		}
		if !IsTerminal(e.To) && !nodeIDs[e.To] {
			diags = append(diags, errf(c.SourcePath, "edge to unknown node %q", e.To))
		}
	}

	for _, id := range stuckNodes(c) {
		diags = append(diags, errf(c.SourcePath, "node %q is in a closed loop with no path to a terminal node", id))
	}

	return diags
}

// lintSensor validates a loaded sensor instance against the registry and the
// constellation set its triggers reference.
func lintSensor(s *SensorInstance, consSet map[string]bool, opts LintOptions) []Diagnostic {
	var diags []Diagnostic
	if opts.SensorTypeKnown != nil && !opts.SensorTypeKnown(s.Type) {
		diags = append(diags, errf(s.SourcePath, "sensor %q references unknown type %q", s.Name, s.Type))
	}
	for _, t := range s.Triggers {
		if t.Constellation != "" && !consSet[t.Constellation] {
			diags = append(diags, errf(s.SourcePath, "sensor %q trigger references unknown constellation %q", s.Name, t.Constellation))
		}
	}
	return diags
}

// stuckNodes returns the IDs of nodes that have outgoing edges yet cannot reach
// any terminal pseudo-node — i.e. they sit in a closed loop. A bounded loop like
// the coder-reviewer cycle is fine because at least one edge out of the loop
// targets a terminal, so every node in it can terminate.
func stuckNodes(c *Constellation) []string {
	succ := make(map[string][]string)
	canTerminate := make(map[string]bool)
	for _, e := range c.Edges {
		succ[e.From] = append(succ[e.From], e.To)
		if IsTerminal(e.To) {
			canTerminate[e.From] = true
		}
	}

	for changed := true; changed; {
		changed = false
		for from, tos := range succ {
			if canTerminate[from] {
				continue
			}
			for _, to := range tos {
				if !IsTerminal(to) && canTerminate[to] {
					canTerminate[from] = true
					changed = true
					break
				}
			}
		}
	}

	var stuck []string
	for _, n := range c.Nodes {
		// Only nodes that actually transition (have outgoing edges) and still
		// cannot terminate are trapped in a loop.
		if len(succ[n.ID]) > 0 && !canTerminate[n.ID] {
			stuck = append(stuck, n.ID)
		}
	}
	sort.Strings(stuck)
	return stuck
}

// names returns the sorted, de-duplicated set of artifact base names available
// in the given directory across the embedded defaults and the per-repo override
// directory.
func (l *Loader) names(dir, ext string) []string {
	set := make(map[string]bool)

	embedded := path.Join(embeddedRoot, dir)
	if entries, err := fs.ReadDir(l.builtins, embedded); err == nil {
		for _, e := range entries {
			if !e.IsDir() && path.Ext(e.Name()) == ext {
				set[baseName(e.Name(), ext)] = true
			}
		}
	}

	repoDir := filepath.Join(l.resolver.RepoPath(), dir)
	if entries, err := os.ReadDir(repoDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ext {
				set[baseName(e.Name(), ext)] = true
			}
		}
	}

	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// staticString returns the value of a template Expression when it is a constant
// string (no ${...} interpolation), else the empty string. Used to statically
// check sub-constellation references without a runtime State.
func staticString(e Expression) string {
	if e == nil {
		return ""
	}
	lit, ok := e.(literalExpr)
	if !ok {
		return ""
	}
	s, _ := lit.value.(string)
	return s
}

// baseName strips the extension from a file name.
func baseName(file, ext string) string {
	return file[:len(file)-len(ext)]
}

// toSet builds a membership set from a slice.
func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, s := range items {
		set[s] = true
	}
	return set
}

// errDiag wraps a load error as an error-severity Diagnostic.
func errDiag(err error) Diagnostic {
	return Diagnostic{Severity: SevError, Message: err.Error()}
}

// errf builds an error-severity Diagnostic prefixed with the artifact source.
func errf(src, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SevError, Message: src + ": " + fmt.Sprintf(format, args...)}
}
