package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
)

// PlanView shows the execution plan for a selected nebula before apply.
// It renders the DAG graph, contract summary, risk list, and stats,
// with action buttons to proceed or cancel.
type PlanView struct {
	Plan      *nebula.ExecutionPlan
	Changes   []nebula.PlanChange // diff vs. previous plan (nil if no prior)
	NebulaDir string
	viewport  viewport.Model
	selected  PlanAction // currently highlighted action button
	width     int
	height    int
	ready     bool // whether viewport dimensions have been set
	loading   bool // true while the plan is being computed
}

// NewPlanView creates a PlanView in loading state (spinner shown).
func NewPlanView() PlanView {
	return PlanView{
		loading:  true,
		selected: PlanActionApply,
	}
}

// SetPlan populates the view with a computed plan and re-renders content.
func (pv *PlanView) SetPlan(plan *nebula.ExecutionPlan, changes []nebula.PlanChange, nebulaDir string) {
	pv.Plan = plan
	pv.Changes = changes
	pv.NebulaDir = nebulaDir
	pv.loading = false
	pv.refresh()
}

// SetSize updates the viewport dimensions and re-renders.
func (pv *PlanView) SetSize(width, height int) {
	pv.width = width
	pv.height = height
	if !pv.ready {
		pv.viewport = viewport.New(width, height)
		pv.ready = true
	} else {
		pv.viewport.Width = width
		pv.viewport.Height = height
	}
	pv.refresh()
}

// SelectedAction returns the currently highlighted action.
func (pv *PlanView) SelectedAction() PlanAction {
	return pv.selected
}

// MoveLeft cycles the selected action to the left.
func (pv *PlanView) MoveLeft() {
	if pv.selected > PlanActionApply {
		pv.selected--
	} else {
		pv.selected = PlanActionSave
	}
}

// MoveRight cycles the selected action to the right.
func (pv *PlanView) MoveRight() {
	if pv.selected < PlanActionSave {
		pv.selected++
	} else {
		pv.selected = PlanActionApply
	}
}

// ScrollUp scrolls the viewport up.
func (pv *PlanView) ScrollUp() {
	pv.viewport.ScrollUp(1)
}

// ScrollDown scrolls the viewport down.
func (pv *PlanView) ScrollDown() {
	pv.viewport.ScrollDown(1)
}

// Update delegates key messages to the viewport for scrolling.
func (pv *PlanView) Update(msg tea.KeyMsg) {
	pv.viewport, _ = pv.viewport.Update(msg)
}

// ErrorRiskCount returns the number of error-severity risks.
func (pv *PlanView) ErrorRiskCount() int {
	if pv.Plan == nil {
		return 0
	}
	n := 0
	for _, r := range pv.Plan.Risks {
		if r.Severity == "error" {
			n++
		}
	}
	return n
}

// View renders the plan preview.
func (pv PlanView) View() string {
	if pv.loading {
		return styleDetailDim.Render("  Analyzing contracts…")
	}
	if pv.Plan == nil {
		return styleDetailDim.Render("  (no plan available)")
	}
	return pv.viewport.View()
}

// refresh rebuilds the viewport content from the current plan state.
func (pv *PlanView) refresh() {
	if pv.Plan == nil || !pv.ready {
		return
	}

	var b strings.Builder
	w := pv.width
	if w < 40 {
		w = 80
	}

	// Header line with action buttons.
	b.WriteString(pv.renderHeader(w))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("═", w))
	b.WriteString("\n\n")

	// DAG graph section.
	b.WriteString(pv.renderGraphSection(w))
	b.WriteString("\n")

	// Contracts section.
	b.WriteString(pv.renderContractsSection(w))
	b.WriteString("\n")

	// Risks section.
	b.WriteString(pv.renderRisksSection())
	b.WriteString("\n")

	// Stats section.
	b.WriteString(pv.renderStatsSection())
	b.WriteString("\n")

	// Diff section (if there are changes from a previous plan).
	if len(pv.Changes) > 0 {
		b.WriteString(pv.renderDiffSection())
		b.WriteString("\n")
	}

	pv.viewport.SetContent(b.String())
}

// renderHeader renders the title and action buttons.
func (pv *PlanView) renderHeader(width int) string {
	title := stylePlanTitle.Render("Observatory: " + pv.Plan.Name)

	// Build action buttons.
	applyLabel := "Apply"
	errors := pv.ErrorRiskCount()
	if errors > 0 {
		applyLabel = fmt.Sprintf("Apply (%d risks)", errors)
	}

	applyBtn := pv.renderButton(applyLabel, PlanActionApply, errors > 0)
	cancelBtn := pv.renderButton("Cancel", PlanActionCancel, false)
	saveBtn := pv.renderButton("Save", PlanActionSave, false)

	buttons := applyBtn + " " + saveBtn + " " + cancelBtn

	// Right-align buttons.
	titleWidth := lipgloss.Width(title)
	buttonsWidth := lipgloss.Width(buttons)
	gap := width - titleWidth - buttonsWidth
	if gap < 2 {
		gap = 2
	}
	return title + strings.Repeat(" ", gap) + buttons
}

// renderButton renders a single action button with selection highlight.
func (pv *PlanView) renderButton(label string, action PlanAction, warn bool) string {
	text := "[" + label + "]"
	if pv.selected == action {
		if warn {
			return stylePlanButtonWarn.Render(text)
		}
		return stylePlanButtonActive.Render(text)
	}
	return stylePlanButtonInactive.Render(text)
}

// renderGraphSection renders the embedded DAG visualization.
func (pv *PlanView) renderGraphSection(width int) string {
	var b strings.Builder
	b.WriteString(stylePlanSectionHeader.Render("Execution Graph"))
	b.WriteString("\n")

	if len(pv.Plan.Waves) == 0 {
		b.WriteString(styleDetailDim.Render("  (no phases)"))
		return b.String()
	}

	// Build title and deps maps for the DAG renderer.
	titles := make(map[string]string)
	deps := make(map[string][]string)
	for _, c := range pv.Plan.Contracts {
		titles[c.PhaseID] = c.PhaseID
	}
	// Also add wave nodes (contracts may not cover all phases).
	for _, w := range pv.Plan.Waves {
		for _, id := range w.NodeIDs {
			if _, ok := titles[id]; !ok {
				titles[id] = id
			}
		}
	}

	// Build deps from the DAG structure — infer from wave ordering.
	// We don't have the raw deps in the plan, so reconstruct from waves.
	// Phases in wave N+1 depend on phases in wave N if there's a contract link.
	depSet := pv.buildDepsFromContracts()
	for id, d := range depSet {
		deps[id] = d
	}

	renderer := &ui.DAGRenderer{
		Width:    width - 4,
		UseColor: true,
	}

	dagStr := renderer.Render(pv.Plan.Waves, deps, titles)
	for _, line := range strings.Split(dagStr, "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// buildDepsFromContracts reconstructs dependency relationships from
// the contract report — a consumer depends on its producer.
func (pv *PlanView) buildDepsFromContracts() map[string][]string {
	deps := make(map[string][]string)
	if pv.Plan.Report == nil {
		return deps
	}
	seen := make(map[string]map[string]bool)
	for _, entry := range pv.Plan.Report.Fulfilled {
		if entry.Consumer != "" && entry.Producer != "" && entry.Consumer != entry.Producer {
			if seen[entry.Consumer] == nil {
				seen[entry.Consumer] = make(map[string]bool)
			}
			if !seen[entry.Consumer][entry.Producer] {
				seen[entry.Consumer][entry.Producer] = true
				deps[entry.Consumer] = append(deps[entry.Consumer], entry.Producer)
			}
		}
	}
	return deps
}

// renderContractsSection renders the contract summary.
func (pv *PlanView) renderContractsSection(_ int) string {
	var b strings.Builder
	b.WriteString(stylePlanSectionHeader.Render("Contracts"))

	report := pv.Plan.Report
	if report == nil {
		b.WriteString(styleDetailDim.Render(" (no contract data)"))
		b.WriteString("\n")
		return b.String()
	}

	fulfilled := len(report.Fulfilled)
	missing := len(report.Missing)
	conflicts := len(report.Conflicts)

	summary := fmt.Sprintf(" (%d fulfilled, %d missing, %d conflicts)",
		fulfilled, missing, conflicts)

	if missing > 0 || conflicts > 0 {
		b.WriteString(stylePlanRiskError.Render(summary))
	} else {
		b.WriteString(stylePlanRiskInfo.Render(summary))
	}
	b.WriteString("\n")

	// Show per-phase contract details.
	for _, c := range pv.Plan.Contracts {
		if len(c.Produces) == 0 && len(c.Consumes) == 0 {
			continue
		}
		b.WriteString("  ")
		b.WriteString(stylePhaseID.Render(c.PhaseID))

		if len(c.Produces) > 0 {
			names := entanglementNames(c.Produces)
			b.WriteString(" PRODUCES: ")
			b.WriteString(stylePlanRiskInfo.Render(strings.Join(names, ", ")))
		}
		if len(c.Consumes) > 0 {
			names := entanglementNamesWithStatus(c.Consumes, report)
			b.WriteString(" CONSUMES: ")
			b.WriteString(strings.Join(names, ", "))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderRisksSection renders the risks list.
func (pv *PlanView) renderRisksSection() string {
	var b strings.Builder
	b.WriteString(stylePlanSectionHeader.Render("Risks"))

	if len(pv.Plan.Risks) == 0 {
		b.WriteString(stylePlanRiskInfo.Render(" (none)"))
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString("\n")

	for _, r := range pv.Plan.Risks {
		b.WriteString("  ")
		switch r.Severity {
		case "error":
			b.WriteString(stylePlanRiskError.Render("[error]"))
		case "warning":
			b.WriteString(stylePlanRiskWarn.Render("[warn]"))
		default:
			b.WriteString(stylePlanRiskInfo.Render("[info]"))
		}
		b.WriteString(" ")
		msg := r.Message
		if r.PhaseID != "" {
			msg = r.PhaseID + " — " + msg
		}
		if r.Severity == "error" {
			b.WriteString(stylePlanRiskError.Render(msg))
		} else {
			b.WriteString(msg)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderStatsSection renders execution statistics.
func (pv *PlanView) renderStatsSection() string {
	s := pv.Plan.Stats
	line := fmt.Sprintf("Stats: %d phases | %d waves | %d tracks | Budget: $%.2f",
		s.TotalPhases, s.TotalWaves, s.TotalTracks, s.EstimatedCost)
	return stylePlanSectionHeader.Render("Stats") + "\n  " + line + "\n"
}

// renderDiffSection renders changes since the last plan.
func (pv *PlanView) renderDiffSection() string {
	var b strings.Builder
	b.WriteString(stylePlanSectionHeader.Render("Changes since last plan"))
	b.WriteString("\n")

	for _, c := range pv.Changes {
		b.WriteString("  ")
		switch c.Kind {
		case "added":
			b.WriteString(stylePlanDiffAdd.Render("+ " + c.Subject + ": " + c.Detail))
		case "removed":
			b.WriteString(stylePlanDiffRemove.Render("- " + c.Subject + ": " + c.Detail))
		case "changed":
			b.WriteString(stylePlanDiffChange.Render("~ " + c.Subject + ": " + c.Detail))
		default:
			b.WriteString("  " + c.Subject + ": " + c.Detail)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// entanglementNames extracts display names from entanglements.
func entanglementNames(ents []fabric.Entanglement) []string {
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name
	}
	return names
}

// entanglementNamesWithStatus annotates consumed entanglements with fulfillment status.
func entanglementNamesWithStatus(ents []fabric.Entanglement, report *fabric.ContractReport) []string {
	// Build a set of fulfilled entanglement names.
	fulfilled := make(map[string]bool)
	if report != nil {
		for _, entry := range report.Fulfilled {
			fulfilled[entry.Entanglement.Name] = true
		}
	}

	names := make([]string, len(ents))
	for i, e := range ents {
		if fulfilled[e.Name] {
			names[i] = stylePlanRiskInfo.Render(e.Name + " [fulfilled]")
		} else {
			names[i] = stylePlanRiskError.Render(e.Name + " [missing]")
		}
	}
	return names
}

// SavePlan writes the execution plan to a JSON file in the nebula directory.
func SavePlan(plan *nebula.ExecutionPlan, nebulaDir string) (string, error) {
	path := filepath.Join(nebulaDir, plan.Name+".plan.json")
	if err := plan.Save(path); err != nil {
		return "", fmt.Errorf("saving plan: %w", err)
	}
	return path, nil
}

// LoadPreviousPlan attempts to load a previous plan from the nebula directory.
// Returns nil if no previous plan exists.
func LoadPreviousPlan(nebulaDir, nebulaName string) *nebula.ExecutionPlan {
	path := filepath.Join(nebulaDir, nebulaName+".plan.json")
	plan, err := nebula.LoadPlan(path)
	if err != nil {
		return nil
	}
	return plan
}

// PlanFooterBindings returns key bindings for the plan preview view.
func PlanFooterBindings(km KeyMap) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev")),
		key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		km.Back,
		km.Quit,
	}
}

// Plan view styles — defined locally since they are only used here.
var (
	stylePlanTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite)

	stylePlanSectionHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	stylePlanButtonActive = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBrightWhite).
				Background(colorNebulaDeep).
				Padding(0, 1)

	stylePlanButtonWarn = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBrightWhite).
				Background(colorDanger).
				Padding(0, 1)

	stylePlanButtonInactive = lipgloss.NewStyle().
				Foreground(colorMutedLight).
				Padding(0, 1)

	stylePlanRiskError = lipgloss.NewStyle().
				Foreground(colorDanger)

	stylePlanRiskWarn = lipgloss.NewStyle().
				Foreground(colorAccent)

	stylePlanRiskInfo = lipgloss.NewStyle().
				Foreground(colorSuccess)

	stylePlanDiffAdd = lipgloss.NewStyle().
				Foreground(colorSuccess)

	stylePlanDiffRemove = lipgloss.NewStyle().
				Foreground(colorDanger)

	stylePlanDiffChange = lipgloss.NewStyle().
				Foreground(colorAccent)
)

// Ensure dag import is used — the type is referenced transitively via ExecutionPlan.Waves.
var _ = []dag.Wave(nil)
