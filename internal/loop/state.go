package loop

// Phase represents a stage in the coder-reviewer loop lifecycle.
type Phase int

const (
	PhaseIdle            Phase = iota // No work started.
	PhaseBeadCreated                  // Task bead created, ready to begin.
	PhaseCoding                       // Coder agent is running.
	PhaseCodeComplete                 // Coder finished, awaiting review.
	PhaseLinting                      // Running lint checks after coder pass.
	PhaseFiltering                    // Running pre-reviewer filter checks.
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
	case PhaseLinting:
		return "linting"
	case PhaseFiltering:
		return "filtering"
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

// FindingStatus represents the lifecycle state of a review finding.
type FindingStatus string

const (
	// FindingStatusFound indicates a newly discovered finding.
	FindingStatusFound FindingStatus = "found"
	// FindingStatusFixed indicates the finding was resolved.
	FindingStatusFixed FindingStatus = "fixed"
	// FindingStatusStillPresent indicates the finding persists from a prior cycle.
	FindingStatusStillPresent FindingStatus = "still_present"
	// FindingStatusRegressed indicates a previously fixed finding has reappeared.
	FindingStatusRegressed FindingStatus = "regressed"
)

// ReviewFinding represents a single issue identified by the reviewer.
type ReviewFinding struct {
	ID          string // deterministic hash for cross-cycle tracking
	Severity    string
	Description string
	Cycle       int           // cycle in which this finding was created (set during accumulation)
	Status      FindingStatus // lifecycle status (set during verification)
}

// FindingVerification represents the reviewer's assessment of a prior finding.
type FindingVerification struct {
	FindingID string        // matches ReviewFinding.ID
	Status    FindingStatus // fixed, still_present, regressed
	Comment   string        // reviewer's explanation
}

// CycleState tracks the mutable state of a coder-reviewer loop across cycles.
type CycleState struct {
	TaskBeadID          string
	TaskTitle           string
	Phase               Phase
	Cycle               int
	MaxCycles           int
	TotalCostUSD        float64
	MaxBudgetUSD        float64
	CoderOutput         string
	LintOutput          string // lint command output from the most recent lint pass
	FilterOutput        string // output from pre-reviewer filter on failure
	FilterCheckName     string // name of the failing filter check (empty if passed)
	ReviewOutput        string
	Findings            []ReviewFinding       // current cycle's findings (reset each cycle)
	Verifications       []FindingVerification // current cycle's verification results
	AllFindings         []ReviewFinding       // accumulated findings across all cycles
	ChildBeadIDs        []string              // accumulated child bead IDs across all cycles
	Refactored          bool                  // true when a mid-run phase edit was applied
	OriginalDescription string                // task description before the refactor
	RefactorDescription string                // the new description from the user edit
	BaseCommitSHA       string                // HEAD before first cycle (captured at task start)
	CycleCommits        []string              // commit SHA per cycle (index = cycle-1)
	lastCycleSHA        string                // transient: last commit SHA for the current cycle (sealed into CycleCommits at cycle end)
	bridgedDiscoveryIDs map[int64]bool        // tracks fabric discovery IDs already bridged to hails, preventing duplicates across cycles
}
