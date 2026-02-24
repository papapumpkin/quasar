package nebula

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// PlanEngine combines DAG analysis with static entanglement contracts
// to produce a pre-execution plan that shows the full contract graph,
// identifies risks, and gates execution.
type PlanEngine struct {
	Scanner *fabric.StaticScanner
}

// ExecutionPlan is the output of the plan engine — a complete picture
// of what will happen during apply.
type ExecutionPlan struct {
	Name        string                 `json:"name"`
	Waves       []dag.Wave             `json:"waves"`
	Tracks      []dag.Track            `json:"tracks"`
	Contracts   []fabric.PhaseContract `json:"contracts"`
	Report      *fabric.ContractReport `json:"report"`
	ImpactOrder []string               `json:"impact_order"`
	Risks       []PlanRisk             `json:"risks"`
	Stats       PlanStats              `json:"stats"`
}

// PlanRisk describes a potential issue detected during plan analysis.
type PlanRisk struct {
	Severity string `json:"severity"` // "error", "warning", "info"
	PhaseID  string `json:"phase_id"`
	Message  string `json:"message"`
}

// PlanStats summarizes the execution plan.
type PlanStats struct {
	TotalPhases        int     `json:"total_phases"`
	TotalWaves         int     `json:"total_waves"`
	TotalTracks        int     `json:"total_tracks"`
	ParallelFactor     int     `json:"parallel_factor"`
	FulfilledContracts int     `json:"fulfilled_contracts"`
	MissingContracts   int     `json:"missing_contracts"`
	Conflicts          int     `json:"conflicts"`
	EstimatedCost      float64 `json:"estimated_cost"`
}

// PlanChange describes a difference between two execution plans.
type PlanChange struct {
	Kind    string `json:"kind"`    // "added", "removed", "changed"
	Subject string `json:"subject"` // phase ID or contract description
	Detail  string `json:"detail"`
}

// Plan runs static analysis and produces an ExecutionPlan without
// executing any phases. It builds the DAG, computes waves, tracks, and
// impact scores, scans phases for contracts, resolves contracts, and
// aggregates risks.
func (pe *PlanEngine) Plan(n *Nebula) (*ExecutionPlan, error) {
	// Step 1: Build DAG and scheduler for waves, tracks, impact scores.
	sched, err := NewScheduler(n.Phases)
	if err != nil {
		return nil, fmt.Errorf("building scheduler: %w", err)
	}

	d := sched.Analyzer().DAG()
	waves, waveErr := d.ComputeWaves()
	if waveErr != nil {
		return nil, fmt.Errorf("computing waves: %w", waveErr)
	}
	tracks := sched.Tracks()
	scores := sched.ImpactScores()

	// Step 2: Build impact-sorted phase order.
	impactOrder := buildImpactOrder(n.Phases, scores)

	// Step 3: Run static scanner to extract contracts.
	inputs := phasesToInputs(n.Phases)
	contracts, scanErr := pe.Scanner.Scan(inputs)
	if scanErr != nil {
		return nil, fmt.Errorf("scanning phases: %w", scanErr)
	}

	// Step 4: Resolve contracts.
	deps := buildDepsMap(n.Phases)
	report := fabric.ResolveContracts(contracts, deps)

	// Step 5: Aggregate risks.
	risks := aggregateRisks(n, report, waves, tracks)

	// Step 6: Compute stats.
	stats := computeStats(n, waves, tracks, report)

	return &ExecutionPlan{
		Name:        n.Manifest.Nebula.Name,
		Waves:       waves,
		Tracks:      tracks,
		Contracts:   contracts,
		Report:      report,
		ImpactOrder: impactOrder,
		Risks:       risks,
		Stats:       stats,
	}, nil
}

// Save writes the plan to a JSON file.
func (ep *ExecutionPlan) Save(path string) error {
	data, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling plan: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing plan file: %w", err)
	}
	return nil
}

// LoadPlan reads a previously saved plan from a JSON file.
func LoadPlan(path string) (*ExecutionPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plan file: %w", err)
	}
	var ep ExecutionPlan
	if err := json.Unmarshal(data, &ep); err != nil {
		return nil, fmt.Errorf("unmarshaling plan: %w", err)
	}
	return &ep, nil
}

// Diff compares two plans and returns human-readable changes describing
// what moved between old and new.
func Diff(old, new *ExecutionPlan) []PlanChange {
	var changes []PlanChange

	// Detect added/removed phases.
	oldPhases := phaseSet(old)
	newPhases := phaseSet(new)

	for id := range newPhases {
		if !oldPhases[id] {
			changes = append(changes, PlanChange{
				Kind:    "added",
				Subject: id,
				Detail:  "phase added to plan",
			})
		}
	}
	for id := range oldPhases {
		if !newPhases[id] {
			changes = append(changes, PlanChange{
				Kind:    "removed",
				Subject: id,
				Detail:  "phase removed from plan",
			})
		}
	}

	// Detect wave count changes.
	if old.Stats.TotalWaves != new.Stats.TotalWaves {
		changes = append(changes, PlanChange{
			Kind:    "changed",
			Subject: "waves",
			Detail:  fmt.Sprintf("wave count changed from %d to %d", old.Stats.TotalWaves, new.Stats.TotalWaves),
		})
	}

	// Detect track count changes.
	if old.Stats.TotalTracks != new.Stats.TotalTracks {
		changes = append(changes, PlanChange{
			Kind:    "changed",
			Subject: "tracks",
			Detail:  fmt.Sprintf("track count changed from %d to %d", old.Stats.TotalTracks, new.Stats.TotalTracks),
		})
	}

	// Detect contract fulfillment changes.
	if old.Stats.FulfilledContracts != new.Stats.FulfilledContracts {
		changes = append(changes, PlanChange{
			Kind:    "changed",
			Subject: "contracts",
			Detail: fmt.Sprintf("fulfilled contracts changed from %d to %d",
				old.Stats.FulfilledContracts, new.Stats.FulfilledContracts),
		})
	}

	// Detect missing contract changes.
	if old.Stats.MissingContracts != new.Stats.MissingContracts {
		changes = append(changes, PlanChange{
			Kind:    "changed",
			Subject: "missing_contracts",
			Detail: fmt.Sprintf("missing contracts changed from %d to %d",
				old.Stats.MissingContracts, new.Stats.MissingContracts),
		})
	}

	// Detect conflict count changes.
	if old.Stats.Conflicts != new.Stats.Conflicts {
		changes = append(changes, PlanChange{
			Kind:    "changed",
			Subject: "conflicts",
			Detail: fmt.Sprintf("conflict count changed from %d to %d",
				old.Stats.Conflicts, new.Stats.Conflicts),
		})
	}

	// Detect risk count changes.
	oldRisks := countRisksBySeverity(old.Risks)
	newRisks := countRisksBySeverity(new.Risks)
	for _, sev := range []string{"error", "warning", "info"} {
		if oldRisks[sev] != newRisks[sev] {
			changes = append(changes, PlanChange{
				Kind:    "changed",
				Subject: "risks/" + sev,
				Detail: fmt.Sprintf("%s risks changed from %d to %d",
					sev, oldRisks[sev], newRisks[sev]),
			})
		}
	}

	// Sort changes for stable output.
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].Subject < changes[j].Subject
	})

	return changes
}

// buildImpactOrder returns phase IDs sorted by impact score descending.
func buildImpactOrder(phases []PhaseSpec, scores map[string]float64) []string {
	ids := make([]string, 0, len(phases))
	for _, p := range phases {
		ids = append(ids, p.ID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return scores[ids[i]] > scores[ids[j]]
	})
	return ids
}

// phasesToInputs converts nebula PhaseSpecs to fabric PhaseInputs for
// the static scanner.
func phasesToInputs(phases []PhaseSpec) []fabric.PhaseInput {
	inputs := make([]fabric.PhaseInput, len(phases))
	for i, p := range phases {
		inputs[i] = fabric.PhaseInput{
			ID:        p.ID,
			Body:      p.Body,
			Scope:     p.Scope,
			DependsOn: p.DependsOn,
		}
	}
	return inputs
}

// buildDepsMap constructs a dependency map from phase specs for use by
// contract resolution.
func buildDepsMap(phases []PhaseSpec) map[string][]string {
	deps := make(map[string][]string, len(phases))
	for _, p := range phases {
		deps[p.ID] = p.DependsOn
	}
	return deps
}

// aggregateRisks builds the risk list from contract report and structural analysis.
func aggregateRisks(n *Nebula, report *fabric.ContractReport, waves []dag.Wave, tracks []dag.Track) []PlanRisk {
	var risks []PlanRisk

	// Missing contracts → error.
	for _, entry := range report.Missing {
		risks = append(risks, PlanRisk{
			Severity: "error",
			PhaseID:  entry.Consumer,
			Message:  fmt.Sprintf("missing producer for %s %q", entry.Entanglement.Kind, entry.Entanglement.Name),
		})
	}

	// Scope overlaps without allow_scope_overlap → error.
	specByID := PhasesByID(n.Phases)
	for i := 0; i < len(n.Phases); i++ {
		for j := i + 1; j < len(n.Phases); j++ {
			a, b := n.Phases[i], n.Phases[j]
			if a.AllowScopeOverlap || b.AllowScopeOverlap {
				continue
			}
			if patA, patB, overlaps := scopesOverlap(a.Scope, b.Scope); overlaps {
				risks = append(risks, PlanRisk{
					Severity: "error",
					PhaseID:  a.ID,
					Message:  fmt.Sprintf("scope overlap with %q: pattern %q conflicts with %q", b.ID, patA, patB),
				})
			}
		}
	}

	// Symbol conflicts → error.
	for _, entry := range report.Conflicts {
		risks = append(risks, PlanRisk{
			Severity: "error",
			PhaseID:  entry.Producer,
			Message:  fmt.Sprintf("symbol %q produced by multiple phases", entry.Entanglement.Name),
		})
	}

	// Single track with max_workers > 1 → warning.
	maxWorkers := n.Manifest.Execution.MaxWorkers
	if maxWorkers > 1 && len(tracks) == 1 {
		risks = append(risks, PlanRisk{
			Severity: "warning",
			PhaseID:  "",
			Message:  fmt.Sprintf("single execution track but max_workers=%d; parallelism will be limited", maxWorkers),
		})
	}

	// Phases with no produces and no consumes → info (leaf node).
	phaseHasContracts := make(map[string]bool, len(n.Phases))
	for _, entry := range report.Fulfilled {
		phaseHasContracts[entry.Consumer] = true
		phaseHasContracts[entry.Producer] = true
	}
	for _, entry := range report.Missing {
		phaseHasContracts[entry.Consumer] = true
	}
	for _, entry := range report.Conflicts {
		phaseHasContracts[entry.Producer] = true
	}

	// Check each leaf wave for isolated phases.
	if len(waves) > 0 {
		lastWave := waves[len(waves)-1]
		for _, id := range lastWave.NodeIDs {
			if !phaseHasContracts[id] && len(specByID[id].DependsOn) == 0 {
				risks = append(risks, PlanRisk{
					Severity: "info",
					PhaseID:  id,
					Message:  "phase has no produces and no consumes (isolated leaf node)",
				})
			}
		}
	}

	return risks
}

// computeStats generates summary statistics from the plan components.
func computeStats(n *Nebula, waves []dag.Wave, tracks []dag.Track, report *fabric.ContractReport) PlanStats {
	// Compute parallel factor as max width across all waves.
	parallelFactor := 0
	for _, w := range waves {
		if len(w.NodeIDs) > parallelFactor {
			parallelFactor = len(w.NodeIDs)
		}
	}

	// Estimate cost from per-phase budget limits.
	var estimatedCost float64
	for _, p := range n.Phases {
		if p.MaxBudgetUSD > 0 {
			estimatedCost += p.MaxBudgetUSD
		}
	}
	if estimatedCost == 0 && n.Manifest.Execution.MaxBudgetUSD > 0 {
		estimatedCost = n.Manifest.Execution.MaxBudgetUSD
	}

	return PlanStats{
		TotalPhases:        len(n.Phases),
		TotalWaves:         len(waves),
		TotalTracks:        len(tracks),
		ParallelFactor:     parallelFactor,
		FulfilledContracts: len(report.Fulfilled),
		MissingContracts:   len(report.Missing),
		Conflicts:          len(report.Conflicts),
		EstimatedCost:      estimatedCost,
	}
}

// phaseSet builds a set of phase IDs from the plan's contracts.
func phaseSet(ep *ExecutionPlan) map[string]bool {
	set := make(map[string]bool, len(ep.Contracts))
	for _, c := range ep.Contracts {
		set[c.PhaseID] = true
	}
	return set
}

// countRisksBySeverity returns a map from severity to count.
func countRisksBySeverity(risks []PlanRisk) map[string]int {
	counts := make(map[string]int)
	for _, r := range risks {
		counts[r.Severity]++
	}
	return counts
}
