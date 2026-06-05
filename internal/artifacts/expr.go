package artifacts

import (
	"fmt"
	"strconv"
	"strings"
)

// Expression is a pre-compiled expression evaluated against a runtime State.
// Constellation when/inputs/outputs strings are compiled to Expressions once at
// load time so evaluation never re-parses.
type Expression interface {
	// Eval evaluates the expression against state. A reference to a missing
	// State field yields nil (the zero value), never an error.
	Eval(state State) (any, error)
	// String renders the expression back to a source-like form.
	String() string
}

// State is the runtime evaluation context. Lookups use dot notation:
// Get("nodes.review.approved") walks nested maps and returns nil if any segment
// is absent, so an expression referencing a not-yet-produced value is falsy
// rather than an error.
type State map[string]any

// Get resolves a dot-separated path through nested maps, returning nil when any
// segment is missing or a non-map is traversed.
func (s State) Get(path string) any {
	var cur any = map[string]any(s)
	for _, seg := range strings.Split(path, ".") {
		m, ok := asMap(cur)
		if !ok {
			return nil
		}
		cur, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

// asMap normalizes the two map shapes the runtime may store (State and plain
// map[string]any) into a single lookup-able map.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case State:
		return map[string]any(m), true
	default:
		return nil, false
	}
}

// Parse compiles a bare expression string (an edge `when` guard) into an
// Expression AST. Errors carry the line:col where parsing failed.
func Parse(source string) (Expression, error) {
	toks, err := lex(source)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		t := p.peek()
		return nil, fmt.Errorf("unexpected %q at %d:%d", t.text, t.line, t.col)
	}
	return expr, nil
}

// ParseTemplate compiles an interpolation template (a node input or
// constellation output value). A string that is exactly "${expr}" evaluates to
// the raw, type-preserving value of expr; a mix of literal text and ${...}
// segments evaluates to the concatenation of their string forms; a string with
// no ${ is a constant string literal.
func ParseTemplate(source string) (Expression, error) {
	if !strings.Contains(source, "${") {
		return literalExpr{value: source}, nil
	}

	var parts []interpPart
	rest := source
	for {
		open := strings.Index(rest, "${")
		if open < 0 {
			if rest != "" {
				parts = append(parts, interpPart{text: rest})
			}
			break
		}
		if open > 0 {
			parts = append(parts, interpPart{text: rest[:open]})
		}
		rest = rest[open+2:]
		close := strings.Index(rest, "}")
		if close < 0 {
			return nil, fmt.Errorf("unterminated ${...} in %q", source)
		}
		inner, err := Parse(rest[:close])
		if err != nil {
			return nil, err
		}
		parts = append(parts, interpPart{expr: inner})
		rest = rest[close+1:]
	}

	if len(parts) == 1 && parts[0].expr != nil {
		return parts[0].expr, nil
	}
	return interpolationExpr{parts: parts, raw: source}, nil
}

// --- AST nodes ---

type literalExpr struct{ value any }

func (e literalExpr) Eval(State) (any, error) { return e.value, nil }
func (e literalExpr) String() string {
	if s, ok := e.value.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(e.value)
}

type varExpr struct{ path string }

func (e varExpr) Eval(s State) (any, error) { return s.Get(e.path), nil }
func (e varExpr) String() string            { return e.path }

type unaryExpr struct {
	op string
	x  Expression
}

func (e unaryExpr) Eval(s State) (any, error) {
	v, err := e.x.Eval(s)
	if err != nil {
		return nil, err
	}
	if e.op == "!" {
		return !truthy(v), nil
	}
	f, ok := toFloat(v)
	if !ok {
		return nil, fmt.Errorf("cannot negate non-number %v", v)
	}
	return -f, nil
}
func (e unaryExpr) String() string { return e.op + e.x.String() }

type binaryExpr struct {
	op   string
	l, r Expression
}

func (e binaryExpr) Eval(s State) (any, error) {
	// Short-circuit boolean operators.
	switch e.op {
	case "&&":
		lv, err := e.l.Eval(s)
		if err != nil {
			return nil, err
		}
		if !truthy(lv) {
			return false, nil
		}
		rv, err := e.r.Eval(s)
		return truthy(rv), err
	case "||":
		lv, err := e.l.Eval(s)
		if err != nil {
			return nil, err
		}
		if truthy(lv) {
			return true, nil
		}
		rv, err := e.r.Eval(s)
		return truthy(rv), err
	}

	lv, err := e.l.Eval(s)
	if err != nil {
		return nil, err
	}
	rv, err := e.r.Eval(s)
	if err != nil {
		return nil, err
	}
	return evalBinary(e.op, lv, rv)
}
func (e binaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", e.l.String(), e.op, e.r.String())
}

type ternaryExpr struct{ cond, then, els Expression }

func (e ternaryExpr) Eval(s State) (any, error) {
	cv, err := e.cond.Eval(s)
	if err != nil {
		return nil, err
	}
	if truthy(cv) {
		return e.then.Eval(s)
	}
	return e.els.Eval(s)
}
func (e ternaryExpr) String() string {
	return fmt.Sprintf("%s ? %s : %s", e.cond.String(), e.then.String(), e.els.String())
}

type callExpr struct {
	name string
	args []Expression
}

func (e callExpr) Eval(s State) (any, error) { return evalCall(e.name, e.args, s) }
func (e callExpr) String() string {
	parts := make([]string, len(e.args))
	for i, a := range e.args {
		parts[i] = a.String()
	}
	return fmt.Sprintf("%s(%s)", e.name, strings.Join(parts, ", "))
}

type interpPart struct {
	text string
	expr Expression
}

type interpolationExpr struct {
	parts []interpPart
	raw   string
}

func (e interpolationExpr) Eval(s State) (any, error) {
	var b strings.Builder
	for _, p := range e.parts {
		if p.expr == nil {
			b.WriteString(p.text)
			continue
		}
		v, err := p.expr.Eval(s)
		if err != nil {
			return nil, err
		}
		b.WriteString(stringify(v))
	}
	return b.String(), nil
}
func (e interpolationExpr) String() string { return e.raw }
