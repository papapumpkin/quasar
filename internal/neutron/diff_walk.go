package neutron

import (
	"regexp"
	"sort"
	"strings"
)

// topLevelDecl matches a Go top-level declaration at the start of a source line:
// a func / type / var / const keyword followed by the declared name. An optional
// method receiver — func (b *Budget) Name — is skipped so the captured name is
// the symbol, not the receiver. The match is anchored at column zero of the
// source line (after the unified-diff +/- marker), so indented (nested)
// declarations are deliberately ignored.
var topLevelDecl = regexp.MustCompile(`^(func|type|var|const)\s+(?:\([^)]*\)\s*)?(\w+)`)

// DetectDeletions walks a unified diff and returns the names of top-level
// func/type/var/const symbols that the diff removes. A symbol counts as deleted
// only when a removed ("-") line declares it AND no added ("+") line anywhere in
// the diff declares the same name — that guards against a rename or signature
// change being mistaken for a deletion. Names are returned sorted and unique.
//
// Detection is intentionally coarse: a rename the regex fails to pair (e.g. the
// new name differs) degrades to a false-positive deletion, which the lifecycle
// treats as advisory. The caller (neutron) emits Deprecate for each name.
func DetectDeletions(diff string) []string {
	added := make(map[string]bool)
	removed := make(map[string]bool)

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			// File headers, not content changes.
			continue
		case strings.HasPrefix(line, "+"):
			if name := declName(line[1:]); name != "" {
				added[name] = true
			}
		case strings.HasPrefix(line, "-"):
			if name := declName(line[1:]); name != "" {
				removed[name] = true
			}
		}
	}

	var out []string
	for name := range removed {
		if !added[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// declName returns the declared symbol name if src is a top-level declaration,
// or "" otherwise. src is the source content with the diff marker already
// stripped.
func declName(src string) string {
	m := topLevelDecl.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	return m[2]
}

// Touched is a symbol an added diff line declares, paired with the declaration
// text the runtime records as its in-flight signature.
type Touched struct {
	Name      string
	Signature string
}

// DetectTouchedSymbols walks a unified diff and returns the top-level
// func/type/var/const symbols that an added ("+") line declares, each paired
// with the trimmed declaration text as its signature. The runtime feeds these to
// MarkInFlight after a green build so a sibling's pre-flight coordination note
// shows the freshest signature draft. The first added line for a name wins;
// file headers ("+++") are skipped.
func DetectTouchedSymbols(diff string) []Touched {
	var out []Touched
	seen := make(map[string]bool)
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		src := line[1:]
		name := declName(src)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Touched{Name: name, Signature: signatureOf(src)})
	}
	return out
}

// signatureOf normalizes a declaration line into a signature: trimmed of
// surrounding whitespace and any trailing block-opening brace, so
// "func Poll() error {" becomes "func Poll() error".
func signatureOf(src string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(src), "{ \t"))
}
