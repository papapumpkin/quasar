package loop

// Phase represents a stage in the coder-reviewer loop lifecycle.
type Phase int

const (
	PhaseIdle            Phase = iota // No work started.
	PhaseBeadCreated                  // Task bead created, ready to begin.
	PhaseCoding                       // Coder agent is running.
	PhaseCodeComplete                 // Coder finished, awaiting review.
	PhaseReviewing                    // Reviewer agent is running.
	PhaseReviewComplete               // Reviewer finished.
	PhaseResolvingIssues              // Issues found, sending back to coder.
	PhaseApproved                     // Reviewer approved the changes.
	PhaseError                        // An error occurred.
)

// String returns the snake_case name of the phase.
func (p Phase) String() string {
	switch p {
	case PhaseIdle:
		return "idle"
	case PhaseBeadCreated:
		return "bead_created"
	case PhaseCoding:
		return "coding"
	case PhaseCodeComplete:
		return "code_complete"
	case PhaseReviewing:
		return "reviewing"
	case PhaseReviewComplete:
		return "review_complete"
	case PhaseResolvingIssues:
		return "resolving_issues"
	case PhaseApproved:
		return "approved"
	case PhaseError:
		return "error"
	default:
		return "unknown"
	}
}

// ReviewFinding represents a single issue identified by the reviewer.
type ReviewFinding struct {
	Severity    string
	Description string
	Cycle       int // cycle in which this finding was created (set during accumulation)
}

// CycleState tracks the mutable state of a coder-reviewer loop across cycles.
type CycleState struct {
	TaskBeadID   string
	TaskTitle    string
	Phase        Phase
	Cycle        int
	MaxCycles    int
	TotalCostUSD float64
	MaxBudgetUSD float64
	CoderOutput  string
	ReviewOutput string
	Findings     []ReviewFinding // current cycle's findings (reset each cycle)
	AllFindings  []ReviewFinding // accumulated findings across all cycles
	ChildBeadIDs []string        // accumulated child bead IDs across all cycles
}
