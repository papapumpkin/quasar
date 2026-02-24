package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// ExecutionPlanRender prints a full execution plan to stderr with ANSI
// color formatting. It renders waves, tracks, contracts, risks, and stats
// in the terraform-style human-readable format.
func (p *Printer) ExecutionPlanRender(ep *nebula.ExecutionPlan, noColor bool) {
	c := planColors(noColor)

	// Header.
	fmt.Fprintf(os.Stderr, "%sObservatory: %s%s\n", c.bold+c.cyan, ep.Name, c.reset)
	fmt.Fprintf(os.Stderr, "%s==============================%s\n", c.dim, c.reset)

	// Execution Graph — waves.
	fmt.Fprintf(os.Stderr, "\n%sExecution Graph:%s\n", c.bold, c.reset)
	for _, w := range ep.Waves {
		fmt.Fprintf(os.Stderr, "  Wave %d: %s\n", w.Number, strings.Join(w.NodeIDs, ", "))
	}

	// Tracks.
	fmt.Fprintf(os.Stderr, "\n%sTracks:%s %d (max parallelism: %d)\n",
		c.bold, c.reset, ep.Stats.TotalTracks, ep.Stats.ParallelFactor)
	for _, tr := range ep.Tracks {
		fmt.Fprintf(os.Stderr, "  Track %d: %s\n", tr.ID, strings.Join(tr.NodeIDs, " -> "))
	}

	// Contracts.
	planRenderContracts(ep, c)

	// Risks.
	planRenderRisks(ep.Risks, c)

	// Stats.
	planRenderStats(ep.Stats, c)

	fmt.Fprintln(os.Stderr)
}

// ExecutionPlanDiff prints a diff between two execution plans to stderr.
func (p *Printer) ExecutionPlanDiff(planName string, changes []nebula.PlanChange, noColor bool) {
	c := planColors(noColor)

	fmt.Fprintf(os.Stderr, "%sPlan diff: %s%s\n", c.bold+c.cyan, planName, c.reset)
	if len(changes) == 0 {
		fmt.Fprintf(os.Stderr, "  %s(no changes)%s\n", c.dim, c.reset)
		return
	}
	for _, ch := range changes {
		var symbol, clr string
		switch ch.Kind {
		case "added":
			symbol, clr = "+", c.green
		case "removed":
			symbol, clr = "-", c.red
		case "changed":
			symbol, clr = "~", c.yellow
		default:
			symbol, clr = "?", c.dim
		}
		fmt.Fprintf(os.Stderr, "  %s%s %s%s — %s\n", clr, symbol, ch.Subject, c.reset, ch.Detail)
	}
}

// ExecutionPlanSaved prints a confirmation that a plan file was written.
// It respects the noColor flag to suppress ANSI escape codes.
func (p *Printer) ExecutionPlanSaved(path string, noColor bool) {
	c := planColors(noColor)
	fmt.Fprintf(os.Stderr, "\n%sPlan saved to %s%s\n", c.green, path, c.reset)
}

// planClr holds an optional ANSI color palette that can be disabled.
type planClr struct {
	bold   string
	dim    string
	reset  string
	red    string
	green  string
	yellow string
	cyan   string
}

// planColors returns ANSI codes unless noColor is true.
func planColors(noColor bool) planClr {
	if noColor {
		return planClr{}
	}
	return planClr{
		bold:   bold,
		dim:    dim,
		reset:  reset,
		red:    red,
		green:  green,
		yellow: yellow,
		cyan:   cyan,
	}
}

// planRenderContracts prints the contract section of the plan.
func planRenderContracts(ep *nebula.ExecutionPlan, c planClr) {
	if len(ep.Contracts) == 0 {
		return
	}

	// Build a lookup from (consumer, entanglement name) -> producer
	// using the contract report fulfilled entries.
	type consumeKey struct {
		consumer string
		name     string
	}
	fulfilledMap := make(map[consumeKey]string)
	if ep.Report != nil {
		for _, entry := range ep.Report.Fulfilled {
			fulfilledMap[consumeKey{consumer: entry.Consumer, name: entry.Entanglement.Name}] = entry.Producer
		}
	}

	fmt.Fprintf(os.Stderr, "\n%sContracts:%s\n", c.bold, c.reset)
	for _, pc := range ep.Contracts {
		if len(pc.Produces) > 0 {
			fmt.Fprintf(os.Stderr, "  %s%s PRODUCES:%s\n", c.cyan, pc.PhaseID, c.reset)
			for _, e := range pc.Produces {
				fmt.Fprintf(os.Stderr, "    %s %s%s\n", e.Kind, e.Name, entanglementPkg(e))
			}
		}
		if len(pc.Consumes) > 0 {
			fmt.Fprintf(os.Stderr, "  %s%s CONSUMES:%s\n", c.yellow, pc.PhaseID, c.reset)
			for _, e := range pc.Consumes {
				producer, ok := fulfilledMap[consumeKey{consumer: pc.PhaseID, name: e.Name}]
				if ok {
					fmt.Fprintf(os.Stderr, "    %s %s%s <- %s [%sfulfilled%s]\n",
						e.Kind, e.Name, entanglementPkg(e), producer, c.green, c.reset)
				} else {
					fmt.Fprintf(os.Stderr, "    %s %s%s [%smissing%s]\n",
						e.Kind, e.Name, entanglementPkg(e), c.red, c.reset)
				}
			}
		}
	}
}

// entanglementPkg formats the package annotation for an entanglement.
func entanglementPkg(e fabric.Entanglement) string {
	if e.Package != "" {
		return " (" + e.Package + ")"
	}
	return ""
}

// planRenderRisks prints the risk section of the plan.
func planRenderRisks(risks []nebula.PlanRisk, c planClr) {
	if len(risks) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "\n%sRisks:%s\n", c.bold, c.reset)
	for _, r := range risks {
		var clr string
		switch r.Severity {
		case "error":
			clr = c.red
		case "warning":
			clr = c.yellow
		case "info":
			clr = c.dim
		}
		phase := ""
		if r.PhaseID != "" {
			phase = " " + r.PhaseID + ":"
		}
		fmt.Fprintf(os.Stderr, "  %s[%s]%s%s %s\n", clr, r.Severity, c.reset, phase, r.Message)
	}
}

// planRenderStats prints the summary statistics section of the plan.
func planRenderStats(stats nebula.PlanStats, c planClr) {
	fmt.Fprintf(os.Stderr, "\n%sStats:%s\n", c.bold, c.reset)
	fmt.Fprintf(os.Stderr, "  Phases: %d | Waves: %d | Tracks: %d | Parallel factor: %d\n",
		stats.TotalPhases, stats.TotalWaves, stats.TotalTracks, stats.ParallelFactor)
	fmt.Fprintf(os.Stderr, "  Contracts: %d fulfilled, %d missing, %d conflicts\n",
		stats.FulfilledContracts, stats.MissingContracts, stats.Conflicts)
	if stats.EstimatedCost > 0 {
		fmt.Fprintf(os.Stderr, "  Budget cap: $%.2f\n", stats.EstimatedCost)
	}
}
