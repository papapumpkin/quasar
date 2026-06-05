package fleet

// This file was originally an empty debug stub. It now holds the render gutter
// invariant test. (It could not be renamed/removed in the sandbox that
// generated it; the content gives it a clear purpose.)

import (
	"testing"
	"unicode/utf8"
)

// TestGutterWidthsMatch locks the invariant that the selected and unselected
// line gutters have identical rune width. pad() aligns columns by rune count,
// so unequal gutters would shift the selected card relative to its neighbours
// and would also break the "inactive selection renders byte-identically to a
// cursorless render" property the golden fixtures depend on.
func TestGutterWidthsMatch(t *testing.T) {
	sel := utf8.RuneCountInString(selectGutter)
	unsel := utf8.RuneCountInString(unselGutter)
	if sel != unsel {
		t.Fatalf("gutter rune widths differ: selectGutter=%d (%q) unselGutter=%d (%q)",
			sel, selectGutter, unsel, unselGutter)
	}
	if unsel == 0 {
		t.Fatal("gutters must be non-empty so cards are indented under repo headers")
	}
}
