package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// architectStep tracks the current stage of the architect overlay.
type architectStep int

const (
	// stepInput shows the text input for the user's description.
	stepInput architectStep = iota
	// stepWorking shows a spinner while the architect agent runs.
	stepWorking
	// stepPreview shows the generated phase with a dependency picker.
	stepPreview
)

// DepEntry represents a phase in the dependency picker.
type DepEntry struct {
	ID       string
	Status   PhaseStatus
	Selected bool
}

// ArchitectOverlay manages the interactive phase creation/refactor flow.
type ArchitectOverlay struct {
	Step    architectStep
	Mode    string // "create" or "refactor"
	PhaseID string // for refactor: which phase

	// Input step.
	TextArea textarea.Model

	// Working step.
	Spinner    spinner.Model
	CancelFunc context.CancelFunc // cancels the architect goroutine

	// Preview step.
	Result      *nebula.ArchitectResult
	Deps        []DepEntry
	DepCursor   int
	CycleWarn   string // warning if a dependency toggle would create a cycle
	Width       int
	Height      int
	AllPhaseIDs []string // all phase IDs in the nebula (for cycle detection)
}

// NewArchitectOverlay creates an overlay in create or refactor mode.
func NewArchitectOverlay(mode, phaseID string, phases []PhaseEntry) *ArchitectOverlay {
	ta := textarea.New()
	ta.CharLimit = 2000
	ta.SetWidth(50)
	ta.SetHeight(5)
	ta.Focus()

	if mode == "refactor" {
		ta.Placeholder = "What should change about this phase?"
	} else {
		ta.Placeholder = "Describe what this phase should do..."
	}

	s := spinner.New()
	s.Spinner = spinner.Dot

	ids := make([]string, len(phases))
	for i, p := range phases {
		ids[i] = p.ID
	}

	return &ArchitectOverlay{
		Step:        stepInput,
		Mode:        mode,
		PhaseID:     phaseID,
		TextArea:    ta,
		Spinner:     s,
		AllPhaseIDs: ids,
	}
}

// SetResult transitions the overlay to the preview step with the architect's output.
func (a *ArchitectOverlay) SetResult(result *nebula.ArchitectResult, phases []PhaseEntry) {
	a.Step = stepPreview
	a.Result = result

	// Build the dependency picker from existing phases.
	suggested := make(map[string]bool)
	for _, d := range result.PhaseSpec.DependsOn {
		suggested[d] = true
	}

	a.Deps = make([]DepEntry, len(phases))
	for i, p := range phases {
		a.Deps[i] = DepEntry{
			ID:       p.ID,
			Status:   p.Status,
			Selected: suggested[p.ID],
		}
	}
	a.DepCursor = 0
	a.CycleWarn = ""
}

// StartWorking transitions the overlay to the working/spinner step.
func (a *ArchitectOverlay) StartWorking() {
	a.Step = stepWorking
}

// ToggleDep toggles the dependency at the cursor position.
// It returns a cycle warning message if the toggle would create a cycle.
func (a *ArchitectOverlay) ToggleDep(newPhaseID string, buildGraph func([]string) bool) {
	if len(a.Deps) == 0 {
		return
	}

	dep := &a.Deps[a.DepCursor]
	dep.Selected = !dep.Selected
	a.CycleWarn = ""

	if dep.Selected && buildGraph != nil {
		// Collect selected deps.
		selected := a.SelectedDeps()
		if buildGraph(selected) {
			a.CycleWarn = fmt.Sprintf("⚠ adding %s would create a dependency cycle", dep.ID)
			dep.Selected = false
		}
	}
}

// SelectedDeps returns the currently selected dependency IDs.
func (a *ArchitectOverlay) SelectedDeps() []string {
	var deps []string
	for _, d := range a.Deps {
		if d.Selected {
			deps = append(deps, d.ID)
		}
	}
	return deps
}

// MoveDepUp moves the dependency cursor up.
func (a *ArchitectOverlay) MoveDepUp() {
	if a.DepCursor > 0 {
		a.DepCursor--
	}
}

// MoveDepDown moves the dependency cursor down.
func (a *ArchitectOverlay) MoveDepDown() {
	if a.DepCursor < len(a.Deps)-1 {
		a.DepCursor++
	}
}

// InputValue returns the current text input value.
func (a *ArchitectOverlay) InputValue() string {
	return strings.TrimSpace(a.TextArea.Value())
}

// View renders the overlay based on the current step.
func (a *ArchitectOverlay) View(width, height int) string {
	switch a.Step {
	case stepInput:
		return a.renderInput(width, height)
	case stepWorking:
		return a.renderWorking(width, height)
	case stepPreview:
		return a.renderPreview(width, height)
	}
	return ""
}

// renderInput renders the text input overlay.
func (a *ArchitectOverlay) renderInput(width, height int) string {
	title := "New Phase"
	if a.Mode == "refactor" {
		title = fmt.Sprintf("Edit Phase: %s", a.PhaseID)
	}

	prompt := "Describe what this phase should do:"
	if a.Mode == "refactor" {
		prompt = "What should change about this phase?"
	}

	boxWidth := min(60, width-4)
	if boxWidth < 30 {
		boxWidth = 30
	}
	a.TextArea.SetWidth(boxWidth - 4)

	var b strings.Builder
	b.WriteString(styleArchitectTitle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(styleArchitectLabel.Render(prompt))
	b.WriteString("\n")
	b.WriteString(a.TextArea.View())
	b.WriteString("\n\n")
	b.WriteString(styleArchitectHint.Render("enter:generate  esc:cancel"))

	box := styleArchitectBox.Width(boxWidth).Render(b.String())
	return centerOverlay(box, width, height)
}

// renderWorking renders the spinner overlay.
func (a *ArchitectOverlay) renderWorking(width, height int) string {
	msg := fmt.Sprintf("%s Generating phase...", a.Spinner.View())
	if a.Mode == "refactor" {
		msg = fmt.Sprintf("%s Refactoring phase...", a.Spinner.View())
	}

	boxWidth := min(50, width-4)
	box := styleArchitectBox.Width(boxWidth).Render(
		styleArchitectTitle.Render(msg),
	)
	return centerOverlay(box, width, height)
}

// renderPreview renders the phase preview with dependency picker.
func (a *ArchitectOverlay) renderPreview(width, height int) string {
	if a.Result == nil {
		return ""
	}

	boxWidth := min(65, width-4)
	if boxWidth < 35 {
		boxWidth = 35
	}

	var b strings.Builder

	// Title.
	title := fmt.Sprintf("Preview: %s", a.Result.PhaseSpec.ID)
	b.WriteString(styleArchitectTitle.Render(title))
	b.WriteString("\n\n")

	// Phase metadata.
	b.WriteString(styleArchitectLabel.Render("Title: "))
	b.WriteString(a.Result.PhaseSpec.Title)
	b.WriteString("\n")
	b.WriteString(styleArchitectLabel.Render("Type: "))
	b.WriteString(a.Result.PhaseSpec.Type)
	b.WriteString("  ")
	b.WriteString(styleArchitectLabel.Render("Priority: "))
	b.WriteString(fmt.Sprintf("%d", a.Result.PhaseSpec.Priority))
	b.WriteString("\n")

	// Errors from validation.
	if len(a.Result.Errors) > 0 {
		b.WriteString("\n")
		for _, e := range a.Result.Errors {
			b.WriteString(styleArchitectWarn.Render("⚠ " + e))
			b.WriteString("\n")
		}
	}

	// Dependency picker.
	if len(a.Deps) > 0 {
		b.WriteString("\n")
		b.WriteString(styleArchitectLabel.Render("Dependencies (↑↓ space to toggle):"))
		b.WriteString("\n")
		for i, dep := range a.Deps {
			check := "[ ]"
			if dep.Selected {
				check = "[✓]"
			}
			status := statusLabel(dep.Status)

			line := fmt.Sprintf("%s %-25s (%s)", check, dep.ID, status)
			if i == a.DepCursor {
				line = styleArchitectCursor.Render(line)
			} else {
				line = styleArchitectDep.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Cycle warning.
	if a.CycleWarn != "" {
		b.WriteString(styleArchitectWarn.Render(a.CycleWarn))
		b.WriteString("\n")
	}

	// Description preview (truncated).
	body := a.Result.PhaseSpec.Body
	if body == "" {
		body = a.Result.Body
	}
	if body != "" {
		b.WriteString("\n")
		b.WriteString(styleArchitectSep.Width(boxWidth - 4).Render("─── Description "))
		b.WriteString("\n")
		lines := strings.Split(body, "\n")
		maxLines := 6
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, "...")
		}
		for _, line := range lines {
			b.WriteString(styleArchitectBody.Render(line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleArchitectHint.Render("enter:confirm  esc:discard"))

	box := styleArchitectBox.Width(boxWidth).Render(b.String())
	return centerOverlay(box, width, height)
}

// statusLabel returns a human-readable label for a phase status.
func statusLabel(s PhaseStatus) string {
	switch s {
	case PhaseWaiting:
		return "waiting"
	case PhaseWorking:
		return "working"
	case PhaseDone:
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseGate:
		return "gate"
	case PhaseSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// WouldCreateCycle checks if adding the given dependencies for newPhaseID
// would create a cycle in the nebula DAG.
func WouldCreateCycle(phases []PhaseEntry, newPhaseID string, deps []string) bool {
	// Build a graph from the current phases.
	specs := make([]nebula.PhaseSpec, len(phases))
	for i, p := range phases {
		specs[i] = nebula.PhaseSpec{
			ID:        p.ID,
			DependsOn: phaseEntryDeps(phases, p.ID),
		}
	}
	g := nebula.NewGraph(specs)

	// Add the new phase node with its proposed dependencies.
	g.AddNode(newPhaseID)
	for _, dep := range deps {
		g.AddEdge(newPhaseID, dep)
	}

	// If a topological sort fails, there's a cycle.
	_, err := g.Sort()
	return err != nil
}

// phaseEntryDeps extracts the DependsOn slice from the PhaseInfo
// stored during MsgNebulaInit. We reconstruct from the stored phase data.
func phaseEntryDeps(phases []PhaseEntry, id string) []string {
	for _, p := range phases {
		if p.ID == id {
			return p.DependsOn
		}
	}
	return nil
}

// Architect overlay styles.
var (
	styleArchitectBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(1, 2)

	styleArchitectTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	styleArchitectLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorWhite)

	styleArchitectHint = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleArchitectCursor = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	styleArchitectDep = lipgloss.NewStyle().
				Foreground(colorWhite)

	styleArchitectWarn = lipgloss.NewStyle().
				Foreground(colorBudgetWarn)

	styleArchitectSep = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleArchitectBody = lipgloss.NewStyle().
				Foreground(colorMuted)
)
