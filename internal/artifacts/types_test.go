package artifacts

import "testing"

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		TermDone:          true,
		TermFailed:        true,
		TermAwaitingHuman: true,
		TermPaused:        true,
		"review":          false,
		"":                false,
		"_unknown":        false,
	}
	for target, want := range cases {
		if got := IsTerminal(target); got != want {
			t.Errorf("IsTerminal(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestNodeTypeConstants(t *testing.T) {
	t.Parallel()
	// The four documented node types must keep their on-disk string spellings,
	// since constellation TOML files reference them by value.
	pairs := map[NodeType]string{
		NodeStar:          "star",
		NodeConstellation: "constellation",
		NodePhaseIterator: "phase_iterator",
		NodeBuiltin:       "builtin",
	}
	for nt, want := range pairs {
		if string(nt) != want {
			t.Errorf("%v = %q, want %q", nt, string(nt), want)
		}
	}
}
