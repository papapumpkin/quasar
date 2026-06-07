package claude

import "fmt"

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

// TruncateResult caps result to p.MaxBytesPerResult bytes. It returns the
// possibly-shortened string and whether truncation occurred. The returned
// string is always <= MaxBytesPerResult: the marker length is reserved before
// the head/tail are sliced so the marker cannot push the output over the cap.
//
// When both KeepHead and KeepTail are set (the default), the surviving budget
// is split evenly between the two ends. When only one is set, that end keeps
// the entire budget. If neither is set, only the marker is returned.
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

	avail := p.MaxBytesPerResult - reserved
	if avail < 0 {
		avail = 0
	}

	headLen, tailLen := splitBudget(avail, p.KeepHead, p.KeepTail)
	head := result[:headLen]
	tail := result[total-tailLen:]

	dropped := total - headLen - tailLen
	return head + fmt.Sprintf(marker, dropped, total) + tail, true
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
