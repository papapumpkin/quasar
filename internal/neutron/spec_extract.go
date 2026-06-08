package neutron

import (
	"regexp"
	"strings"
)

// Declaration is a candidate producer symbol pulled from a phase spec. Kind is
// the Go keyword that introduced it ("func", "type", "var", "const"); the
// runtime maps it onto a fabric entanglement kind when it calls Declare.
type Declaration struct {
	Kind string
	Name string
}

// declInSpec matches a Go declaration anywhere within a span of spec prose or a
// fenced code block: a func/type/var/const keyword (optionally with a method
// receiver) followed by the symbol name. Unlike the diff walker it is not
// column-anchored, because spec text indents code snippets arbitrarily.
var declInSpec = regexp.MustCompile(`\b(func|type|var|const)\s+(?:\([^)]*\)\s*)?([A-Z]\w*)`)

// extractedSections lists the markdown headers whose bodies neutron scans for
// producer-symbol declarations. The spec's "## Files" and "## Solution"
// sections are where an architect names what the phase will produce or modify.
var extractedSections = []string{"## files", "## solution"}

// ExtractDeclarations scans the "## Files" and "## Solution" sections of a phase
// spec and returns the producer symbols declared there (e.g. `type Sensor
// interface`, `func (b *Budget) CheckBefore`). Only exported names (leading
// uppercase) are reported, since unexported symbols never become cross-phase
// entanglements. Duplicates are collapsed, preserving first-seen order.
//
// False positives are acceptable: the lifecycle's later stages reconcile, and
// the pre-flight coordination check that consumes these is advisory.
func ExtractDeclarations(spec string) []Declaration {
	var out []Declaration
	seen := make(map[string]bool)

	for _, body := range sectionBodies(spec, extractedSections) {
		for _, m := range declInSpec.FindAllStringSubmatch(body, -1) {
			kind, name := m[1], m[2]
			key := kind + " " + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Declaration{Kind: kind, Name: name})
		}
	}
	return out
}

// sectionBodies returns the text under each named markdown header (matched
// case-insensitively), stopping at the next "## " header or end of document.
func sectionBodies(spec string, headers []string) []string {
	lines := strings.Split(spec, "\n")
	var bodies []string

	for i := 0; i < len(lines); i++ {
		if !isWantedHeader(lines[i], headers) {
			continue
		}
		var b strings.Builder
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				break
			}
			b.WriteString(lines[j])
			b.WriteByte('\n')
		}
		bodies = append(bodies, b.String())
	}
	return bodies
}

// isWantedHeader reports whether line is one of the wanted "## ..." headers,
// compared case-insensitively after trimming trailing whitespace.
func isWantedHeader(line string, headers []string) bool {
	norm := strings.ToLower(strings.TrimRight(line, " \t"))
	for _, h := range headers {
		if norm == h {
			return true
		}
	}
	return false
}
