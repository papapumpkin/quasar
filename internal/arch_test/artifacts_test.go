package arch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/repos"
)

// TestExpressionLanguageMinimal asserts the constellation expression grammar
// stays small: only the documented operators are accepted, and stray symbols
// (which a richer DSL might allow) are rejected at parse time. This guards the
// constraint that no custom DSL beyond the tiny mini-language is introduced.
func TestExpressionLanguageMinimal(t *testing.T) {
	t.Parallel()

	valid := []string{
		"a == b", "a != b", "a < b", "a <= b", "a > b", "a >= b",
		"a && b", "a || b", "!a",
		"a + b", "a - b", "a * b", "a / b",
		"a ? b : c",
		"a.b.c",
		"true", "false", "1", "1.5", "\"s\"",
	}
	for _, src := range valid {
		if _, err := artifacts.Parse(src); err != nil {
			t.Errorf("Parse(%q) rejected a documented form: %v", src, err)
		}
	}

	invalid := []string{
		"a % b",  // modulo is not in the grammar
		"a & b",  // bitwise and
		"a | b",  // bitwise or
		"a @ b",  // stray symbol
		"a ** b", // power
		"a = b",  // assignment
		"a ?? b", // null-coalesce
	}
	for _, src := range invalid {
		if _, err := artifacts.Parse(src); err == nil {
			t.Errorf("Parse(%q) accepted an out-of-grammar construct", src)
		}
	}
}

// TestNoFunctionCallsBeyondStdlib asserts the only callable functions are the
// documented stdlib (len, has, empty). Any other call must be rejected so a
// constellation cannot reach arbitrary Go.
func TestNoFunctionCallsBeyondStdlib(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"len(x)", "has(m, \"k\")", "empty(x)"} {
		if _, err := artifacts.Parse(src); err != nil {
			t.Errorf("Parse(%q) rejected a stdlib call: %v", src, err)
		}
	}
	for _, src := range []string{"min(1,2)", "max(x)", "exec(\"rm\")", "printf(\"x\")"} {
		if _, err := artifacts.Parse(src); err == nil {
			t.Errorf("Parse(%q) accepted a non-stdlib function call", src)
		}
	}
}

// TestSchemaStrictness asserts that strict mode rejects artifact files carrying
// keys absent from the schema, which is what `quasar lint --strict` relies on.
func TestSchemaStrictness(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	dir := filepath.Join(repo, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.md"),
		[]byte("+++\nname = \"s\"\nunknown_key = 1\n+++\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := repos.NewResolver(&repos.Repo{Path: repo})
	if err != nil {
		t.Fatal(err)
	}

	loose := artifacts.New(res)
	if _, err := loose.LoadSkill("s"); err != nil {
		t.Errorf("non-strict load rejected a forward-compatible extra key: %v", err)
	}

	strict := artifacts.New(res)
	strict.Strict = true
	if _, err := strict.LoadSkill("s"); err == nil {
		t.Error("strict load accepted an unknown key")
	}
}
