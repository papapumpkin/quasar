package loop

import "fmt"

// Tool-budget defaults. They are conservative: a coder rarely needs more than
// a handful of reads before it has enough context to start editing, and the
// hard total-reads cap is a backstop against runaway exploration rather than a
// binding limit on legitimate work.
const (
	defaultMaxReadsBeforeEdit = 8
	defaultMaxGrepsBeforeEdit = 6
	defaultMaxTotalReads      = 30
)

// ToolCall identifies a single tool invocation the loop is about to forward to
// the agent. Only the tool name is needed to apply budget policy.
type ToolCall struct {
	Name string
}

// Budget tracks per-invocation tool-call counts and nudges (or, at the hard
// cap, blocks) a coder that thrashes on Read/Grep without committing to an
// Edit. It is not safe for concurrent use; each agent invocation owns one.
type Budget struct {
	// MaxReadsBeforeEdit is the soft limit on Reads issued since the last Edit.
	MaxReadsBeforeEdit int
	// MaxGrepsBeforeEdit is the soft limit on Greps issued since the last Edit.
	MaxGrepsBeforeEdit int
	// MaxTotalReads is the hard cap on total Reads for the whole invocation.
	// A Read past this cap is rejected (proceed=false).
	MaxTotalReads int
	// SoftAdvisory, when true, emits an advisory for soft-limit breaches but
	// still allows the call. When false, soft breaches also block the call.
	SoftAdvisory bool

	readsSinceEdit int
	grepsSinceEdit int
	totalReads     int
	advisedReads   bool
	advisedGreps   bool
}

// DefaultBudget returns the conservative coder-default budget with soft
// advisories enabled.
func DefaultBudget() Budget {
	return Budget{
		MaxReadsBeforeEdit: defaultMaxReadsBeforeEdit,
		MaxGrepsBeforeEdit: defaultMaxGrepsBeforeEdit,
		MaxTotalReads:      defaultMaxTotalReads,
		SoftAdvisory:       true,
	}
}

// OnToolCall records a tool call and applies budget policy. It returns whether
// the call should proceed and, when a limit is crossed, an advisory string to
// inject into the next assistant turn as a <system-reminder>-style note.
//
// An Edit or Write resets the per-edit Read/Grep counters (and re-arms their
// advisories). Reads are additionally subject to the MaxTotalReads hard cap,
// which always blocks regardless of SoftAdvisory.
func (b *Budget) OnToolCall(call ToolCall) (proceed bool, advisory string) {
	switch call.Name {
	case "Read":
		return b.onRead()
	case "Grep":
		return b.onGrep()
	case "Edit", "Write", "NotebookEdit":
		b.resetSinceEdit()
		return true, ""
	default:
		return true, ""
	}
}

// onRead applies the total-reads hard cap and the reads-before-edit soft limit.
func (b *Budget) onRead() (bool, string) {
	if b.MaxTotalReads > 0 && b.totalReads >= b.MaxTotalReads {
		return false, fmt.Sprintf(
			"<system-reminder>Read budget exhausted: %d Reads reached the hard cap of %d. "+
				"No further Reads are permitted — commit to an edit plan with the context you have.</system-reminder>",
			b.totalReads, b.MaxTotalReads)
	}
	b.totalReads++
	b.readsSinceEdit++

	if b.MaxReadsBeforeEdit > 0 && b.readsSinceEdit > b.MaxReadsBeforeEdit && !b.advisedReads {
		b.advisedReads = true
		advisory := fmt.Sprintf(
			"<system-reminder>You have used %d Reads without an Edit. "+
				"Please commit to an edit plan.</system-reminder>",
			b.readsSinceEdit)
		return b.SoftAdvisory, advisory
	}
	return true, ""
}

// onGrep applies the greps-before-edit soft limit.
func (b *Budget) onGrep() (bool, string) {
	b.grepsSinceEdit++
	if b.MaxGrepsBeforeEdit > 0 && b.grepsSinceEdit > b.MaxGrepsBeforeEdit && !b.advisedGreps {
		b.advisedGreps = true
		advisory := fmt.Sprintf(
			"<system-reminder>You have used %d Greps without an Edit. "+
				"Please commit to an edit plan.</system-reminder>",
			b.grepsSinceEdit)
		return b.SoftAdvisory, advisory
	}
	return true, ""
}

// resetSinceEdit clears the per-edit counters and re-arms advisories after an
// edit lands.
func (b *Budget) resetSinceEdit() {
	b.readsSinceEdit = 0
	b.grepsSinceEdit = 0
	b.advisedReads = false
	b.advisedGreps = false
}
