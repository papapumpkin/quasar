package claude

import (
	"fmt"
	"unicode/utf8"

	"github.com/papapumpkin/quasar/internal/agent"
)

// defaultMaxResultBytes is the default per-result truncation cap for tool
// output reaching a coder. 16 KB keeps almost every real file read intact
// while clipping pathological reads (whole sourcemaps, fixture JSON,
// transcripts) that would otherwise dominate the context window.
const defaultMaxResultBytes = 16 * 1024

// defaultMarker is the separator inserted between the surviving head and tail
// of a truncated result. It carries two int verbs: the number of bytes
// dropped and the original total size, so the agent can decide whether to
// re-read a specific range.
const defaultMarker = "\n... (truncated %d bytes, %d total) ...\n"

// TruncationPolicy bounds the size of a single tool result before it reaches
// the agent. KeepHead/KeepTail select which ends of an oversized result are
// preserved; the dropped middle is replaced by Marker.
type TruncationPolicy struct {
	// MaxBytesPerResult is the maximum size of the returned (post-truncation)
	// string in bytes. A value <= 0 disables truncation entirely.
	MaxBytesPerResult int
	// KeepHead preserves the leading bytes of an oversized result.
	KeepHead bool
	// KeepTail preserves the trailing bytes of an oversized result.
	KeepTail bool
	// Marker is a fmt format string with two int verbs (dropped bytes, total
	// bytes) inserted in place of the removed middle section.
	Marker string
}

// DefaultTruncationPolicy returns the coder-default policy: a 16 KB cap that
// preserves both head and tail with the standard marker between them.
func DefaultTruncationPolicy() TruncationPolicy {
	return TruncationPolicy{
		MaxBytesPerResult: defaultMaxResultBytes,
		KeepHead:          true,
		KeepTail:          true,
		Marker:            defaultMarker,
	}
}

// truncationPolicyFor derives the result-truncation policy from an agent's
// effective context budget. The byte cap is the budget's ToolResultMaxBytes
// (per-role default when the agent supplies no explicit budget); a non-positive
// cap disables truncation in TruncateResult.
//
// Mechanism note (see also TruncateResult): because Quasar drives the coder via
// headless `claude -p`, the Go layer never observes intermediate Read/Grep
// results — only the final invocation result. Truncation therefore bounds the
// inter-agent handoff (one agent's output → the next agent's input context), not
// in-loop per-read bloat. In-loop read size is governed by the budget hook's
// count caps (MaxReadsBeforeEdit/MaxTotalReads) plus the model's own behavior,
// never by per-read size clipping.
//
// A result consumed as a whole structured payload (ResultIsStructured — the
// reviewer/master-reviewer JSON decision) bypasses truncation: a head+tail
// marker spliced into the middle of a JSON document would make it unparseable
// and hard-fail the downstream reviewer_decision builtin, which is a worse
// outcome than a larger handoff.
func truncationPolicyFor(a agent.Agent) TruncationPolicy {
	p := DefaultTruncationPolicy()
	budget := a.EffectiveContextBudget()
	if budget.ResultIsStructured {
		p.MaxBytesPerResult = 0 // disable truncation for whole-payload results
		return p
	}
	p.MaxBytesPerResult = budget.ToolResultMaxBytes
	return p
}

// TruncateResult caps result to p.MaxBytesPerResult bytes. It returns the
// possibly-shortened string and whether truncation occurred. The returned
// string is always <= MaxBytesPerResult: the marker length is reserved before
// the head/tail are sliced so the marker cannot push the output over the cap.
//
// When both KeepHead and KeepTail are set (the default), the surviving budget
// is split evenly between the two ends. When only one is set, that end keeps
// the entire budget. If neither is set, only the marker is returned.
//
// Head/tail cut points are snapped to UTF-8 rune boundaries so a truncated
// result never contains a partial multi-byte rune. Snapping only ever shrinks
// a slice, so the <= MaxBytesPerResult guarantee is preserved.
func TruncateResult(result string, p TruncationPolicy) (string, bool) {
	total := len(result)
	if p.MaxBytesPerResult <= 0 || total <= p.MaxBytesPerResult {
		return result, false
	}

	marker := p.Marker
	if marker == "" {
		marker = defaultMarker
	}
	// Reserve marker space using the widest possible numbers (total appears in
	// both verbs and is the largest value either can take) so the rendered
	// marker can only be shorter than reserved — never longer.
	reserved := len(fmt.Sprintf(marker, total, total))

	// Pathologically small cap: the marker alone does not fit. Returning the
	// full marker would exceed the cap, so hard-clamp it (on a rune boundary)
	// to honor the <= MaxBytesPerResult contract.
	if reserved >= p.MaxBytesPerResult {
		final := fmt.Sprintf(marker, total, total)
		return final[:headBoundary(final, p.MaxBytesPerResult)], true
	}

	avail := p.MaxBytesPerResult - reserved
	headLen, tailLen := splitBudget(avail, p.KeepHead, p.KeepTail)

	headEnd := headBoundary(result, headLen)
	tailStart := tailBoundary(result, total-tailLen)
	if tailStart < headEnd {
		tailStart = headEnd // guard against head/tail overlap on tiny inputs
	}
	head := result[:headEnd]
	tail := result[tailStart:]

	dropped := total - len(head) - len(tail)
	return head + fmt.Sprintf(marker, dropped, total) + tail, true
}

// headBoundary returns the largest index <= n that lies on a UTF-8 rune
// boundary, so s[:headBoundary(s, n)] never ends mid-rune. Indices beyond
// len(s) clamp to len(s).
func headBoundary(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	if n < 0 {
		return 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// tailBoundary returns the smallest index >= start that lies on a UTF-8 rune
// boundary, so s[tailBoundary(s, start):] never begins mid-rune.
func tailBoundary(s string, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(s) {
		return len(s)
	}
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return start
}

// splitBudget divides avail bytes between the head and tail according to which
// ends the policy keeps. With both ends kept the split is even (the head takes
// the odd byte).
func splitBudget(avail int, keepHead, keepTail bool) (head, tail int) {
	switch {
	case keepHead && keepTail:
		head = avail - avail/2
		tail = avail / 2
	case keepHead:
		head = avail
	case keepTail:
		tail = avail
	}
	return head, tail
}
