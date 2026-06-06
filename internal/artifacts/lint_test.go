package artifacts

import (
	"strings"
	"testing"
)

// findDiag reports whether any diagnostic message contains substr.
func findDiag(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func TestLintCleanDefaults(t *testing.T) {
	t.Parallel()
	l, _ := newTestLoader(t)
	diags := l.Lint(LintOptions{})
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for embedded defaults, got: %+v", diags)
	}
}

func TestLintUnknownStar(t *testing.T) {
	t.Parallel()
	l, repo := newTestLoader(t)
	writeFile(t, repo, "constellations/c.toml", `name = "c"
[[nodes]]
id = "n"
type = "star"
star = "ghost"
[[edges]]
from = "n"
to = "_done"
`)
	diags := l.Lint(LintOptions{})
	if !findDiag(diags, "unknown star \"ghost\"") {
		t.Errorf("expected unknown-star diagnostic, got: %+v", diags)
	}
}

func TestLintUnknownEdgeTarget(t *testing.T) {
	t.Parallel()
	l, repo := newTestLoader(t)
	writeFile(t, repo, "constellations/c.toml", `name = "c"
[[nodes]]
id = "n"
type = "builtin"
op = "noop"
[[edges]]
from = "n"
to = "missing"
`)
	diags := l.Lint(LintOptions{})
	if !findDiag(diags, "edge to unknown node \"missing\"") {
		t.Errorf("expected unknown-edge diagnostic, got: %+v", diags)
	}
}

func TestLintClosedLoop(t *testing.T) {
	t.Parallel()
	l, repo := newTestLoader(t)
	// a -> b -> a with no edge to any terminal: a true closed loop.
	writeFile(t, repo, "constellations/loop.toml", `name = "loop"
[[nodes]]
id = "a"
type = "builtin"
op = "x"
[[nodes]]
id = "b"
type = "builtin"
op = "y"
[[edges]]
from = "a"
to = "b"
[[edges]]
from = "b"
to = "a"
`)
	diags := l.Lint(LintOptions{})
	if !findDiag(diags, "closed loop") {
		t.Errorf("expected closed-loop diagnostic, got: %+v", diags)
	}
}

func TestLintUnknownSensorType(t *testing.T) {
	t.Parallel()
	l, repo := newTestLoader(t)
	writeFile(t, repo, "sensors/s.toml", "name=\"s\"\ntype=\"nope\"\n[config]\n")
	diags := l.Lint(LintOptions{
		SensorTypeKnown: func(string) bool { return false },
	})
	if !findDiag(diags, "unknown type \"nope\"") {
		t.Errorf("expected unknown-sensor-type diagnostic, got: %+v", diags)
	}
}

func TestLintStrictUnknownField(t *testing.T) {
	t.Parallel()
	l, repo := newTestLoader(t)
	writeFile(t, repo, "skills/x.md", "+++\nname = \"x\"\nbogus_key = true\n+++\nbody")
	l.Strict = true
	diags := l.Lint(LintOptions{})
	if len(diags) == 0 {
		t.Fatal("expected a strict-mode diagnostic for the unknown field")
	}
}
