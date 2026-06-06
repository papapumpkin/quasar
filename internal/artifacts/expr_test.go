package artifacts

import "testing"

func TestParseAndEval(t *testing.T) {
	t.Parallel()

	state := State{
		"cycle": 2,
		"review": map[string]any{
			"approved":       false,
			"findings_count": 3,
		},
		"nebula": map[string]any{
			"execution": map[string]any{"max_review_cycles": 5},
		},
		"items": []any{"a", "b"},
	}

	cases := []struct {
		name string
		src  string
		want any
	}{
		{"bool literal", "true", true},
		{"int compare", "cycle > 1", true},
		{"dot access preserves stored type", "review.findings_count", 3},
		{"and short circuit", "review.findings_count > 0 && cycle < nebula.execution.max_review_cycles", true},
		{"or", "review.approved || cycle == 2", true},
		{"not", "!review.approved", true},
		{"arithmetic", "1 + 2 * 3", float64(7)},
		{"ternary", "review.approved ? 1 : 0", float64(0)},
		{"equality string", "\"ship\" == \"ship\"", true},
		{"len stdlib", "len(items)", float64(2)},
		{"empty stdlib", "empty(items)", false},
		{"has stdlib", "has(review, \"approved\")", true},
		{"missing field is nil-falsy", "nodes.review.approved", nil},
		{"missing field compares false", "nodes.review.findings_count > 0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got, err := expr.Eval(state)
			if err != nil {
				t.Fatalf("Eval(%q): %v", tc.src, err)
			}
			if got != tc.want {
				t.Errorf("Eval(%q) = %v (%T), want %v (%T)", tc.src, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestParseRejectsUnknownFunction(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"min(1, 2)", "max(a)", "foo()"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) accepted an unknown function", src)
		}
	}
}

func TestParseRejectsBadTokens(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"a @ b", "a % b", "a & b"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) accepted an out-of-grammar token", src)
		}
	}
}

func TestParseTemplate(t *testing.T) {
	t.Parallel()
	state := State{"nebula": map[string]any{"name": "demo"}}

	// Bare ${...} preserves the underlying value's type.
	raw, err := ParseTemplate("${nebula.name}")
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	if v, _ := raw.Eval(state); v != "demo" {
		t.Errorf("bare template = %v", v)
	}

	// Mixed text + interpolation stringifies.
	mixed, err := ParseTemplate("repo:${nebula.name}!")
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	if v, _ := mixed.Eval(state); v != "repo:demo!" {
		t.Errorf("mixed template = %v", v)
	}

	// No ${ is a constant string.
	lit, err := ParseTemplate("constant")
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	if v, _ := lit.Eval(nil); v != "constant" {
		t.Errorf("constant template = %v", v)
	}
}
