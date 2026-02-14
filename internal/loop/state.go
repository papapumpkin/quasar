package loop

type Phase int

const (
	PhaseIdle Phase = iota
	PhaseBeadCreated
	PhaseCoding
	PhaseCodeComplete
	PhaseReviewing
	PhaseReviewComplete
	PhaseResolvingIssues
	PhaseApproved
	PhaseError
)

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

type ReviewFinding struct {
	Severity    string
	Description string
}

type CycleState struct {
	TaskBeadID    string
	TaskTitle     string
	Phase         Phase
	Cycle         int
	MaxCycles     int
	TotalCostUSD  float64
	MaxBudgetUSD  float64
	CoderOutput   string
	ReviewOutput  string
	Findings      []ReviewFinding
	ChildBeadIDs  []string
}
