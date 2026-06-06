package artifacts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// tokKind enumerates the lexical token classes of the expression language.
type tokKind int

const (
	tEOF tokKind = iota
	tInt
	tFloat
	tString
	tIdent
	tTrue
	tFalse
	tLParen
	tRParen
	tComma
	tDot
	tQuestion
	tColon
	tBang
	tAnd
	tOr
	tEq
	tNeq
	tLt
	tLte
	tGt
	tGte
	tPlus
	tMinus
	tStar
	tSlash
)

// token is a single lexed unit with its 1-based source position for errors.
type token struct {
	kind tokKind
	text string
	line int
	col  int
}

// lex scans source into tokens. It rejects any character outside the documented
// grammar, which is what keeps the language minimal: an unknown rune (e.g. '%'
// or '[') is a lex error, not a silently-ignored token.
func lex(source string) ([]token, error) {
	var toks []token
	runes := []rune(source)
	line, col := 1, 1
	i := 0

	advance := func(n int) {
		for k := 0; k < n; k++ {
			if runes[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		}
	}

	for i < len(runes) {
		c := runes[i]
		switch {
		case c == '\n' || unicode.IsSpace(c):
			advance(1)
		case c == '"' || c == '\'':
			tok, n, err := lexString(runes[i:], line, col)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			advance(n)
		case unicode.IsDigit(c):
			tok, n := lexNumber(runes[i:], line, col)
			toks = append(toks, tok)
			advance(n)
		case unicode.IsLetter(c) || c == '_':
			tok, n := lexIdent(runes[i:], line, col)
			toks = append(toks, tok)
			advance(n)
		default:
			tok, n, err := lexOperator(runes[i:], line, col)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			advance(n)
		}
	}

	toks = append(toks, token{kind: tEOF, text: "", line: line, col: col})
	return toks, nil
}

// lexString lexes a single- or double-quoted string literal. Both quote styles
// are accepted because constellation TOML embeds expressions like
// `decision == 'ship'`.
func lexString(rs []rune, line, col int) (token, int, error) {
	quote := rs[0]
	var b strings.Builder
	for j := 1; j < len(rs); j++ {
		if rs[j] == quote {
			return token{kind: tString, text: b.String(), line: line, col: col}, j + 1, nil
		}
		b.WriteRune(rs[j])
	}
	return token{}, 0, fmt.Errorf("unterminated string at %d:%d", line, col)
}

// lexNumber lexes an integer or float literal.
func lexNumber(rs []rune, line, col int) (token, int) {
	j := 0
	isFloat := false
	for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.') {
		if rs[j] == '.' {
			isFloat = true
		}
		j++
	}
	kind := tInt
	if isFloat {
		kind = tFloat
	}
	return token{kind: kind, text: string(rs[:j]), line: line, col: col}, j
}

// lexIdent lexes an identifier or boolean keyword.
func lexIdent(rs []rune, line, col int) (token, int) {
	j := 0
	for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
		j++
	}
	text := string(rs[:j])
	kind := tIdent
	switch text {
	case "true":
		kind = tTrue
	case "false":
		kind = tFalse
	}
	return token{kind: kind, text: text, line: line, col: col}, j
}

// twoCharOps maps two-character operators to their token kinds.
var twoCharOps = map[string]tokKind{
	"&&": tAnd, "||": tOr, "==": tEq, "!=": tNeq, "<=": tLte, ">=": tGte,
}

// oneCharOps maps single-character operators and punctuation to token kinds.
var oneCharOps = map[rune]tokKind{
	'(': tLParen, ')': tRParen, ',': tComma, '.': tDot, '?': tQuestion,
	':': tColon, '!': tBang, '<': tLt, '>': tGt, '+': tPlus, '-': tMinus,
	'*': tStar, '/': tSlash,
}

// lexOperator lexes an operator or punctuation token, preferring the
// two-character forms. Bare '&' or '|' (not doubled) are rejected.
func lexOperator(rs []rune, line, col int) (token, int, error) {
	if len(rs) >= 2 {
		if kind, ok := twoCharOps[string(rs[:2])]; ok {
			return token{kind: kind, text: string(rs[:2]), line: line, col: col}, 2, nil
		}
	}
	if kind, ok := oneCharOps[rs[0]]; ok {
		return token{kind: kind, text: string(rs[0]), line: line, col: col}, 1, nil
	}
	return token{}, 0, fmt.Errorf("unexpected character %q at %d:%d", string(rs[0]), line, col)
}

// stdlibFuncs is the closed set of callable function names. The parser rejects
// any other name followed by '(', enforcing "no function calls beyond stdlib".
var stdlibFuncs = map[string]bool{"len": true, "has": true, "empty": true}

// parser is a Pratt (precedence-climbing) recursive-descent parser over a
// pre-lexed token slice.
type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

// binPrec returns the binding power of an infix operator, or -1 for tokens that
// do not start an infix expression. Higher binds tighter.
func binPrec(k tokKind) int {
	switch k {
	case tQuestion:
		return 1
	case tOr:
		return 2
	case tAnd:
		return 3
	case tEq, tNeq:
		return 4
	case tLt, tLte, tGt, tGte:
		return 5
	case tPlus, tMinus:
		return 6
	case tStar, tSlash:
		return 7
	default:
		return -1
	}
}

// parseExpr parses an expression whose operators bind at least as tightly as
// minPrec, climbing precedence as it consumes infix operators.
func (p *parser) parseExpr(minPrec int) (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		op := p.peek()
		prec := binPrec(op.kind)
		if prec < 0 || prec < minPrec {
			return left, nil
		}

		if op.kind == tQuestion {
			left, err = p.parseTernary(left)
			if err != nil {
				return nil, err
			}
			continue
		}

		p.next()
		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op.text, l: left, r: right}
	}
}

// parseTernary parses the `? then : else` tail of a ternary, right-associative
// so a chained else (`a ? b : c ? d : e`) nests correctly.
func (p *parser) parseTernary(cond Expression) (Expression, error) {
	p.next() // consume '?'
	then, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if c := p.peek(); c.kind != tColon {
		return nil, fmt.Errorf("expected ':' in ternary at %d:%d", c.line, c.col)
	}
	p.next() // consume ':'
	els, err := p.parseExpr(binPrec(tQuestion))
	if err != nil {
		return nil, err
	}
	return ternaryExpr{cond: cond, then: then, els: els}, nil
}

// parseUnary parses a prefix !/- applied to a unary, or falls through to a
// primary.
func (p *parser) parseUnary() (Expression, error) {
	if t := p.peek(); t.kind == tBang || t.kind == tMinus {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: t.text, x: x}, nil
	}
	return p.parsePrimary()
}

// parsePrimary parses a literal, parenthesized expression, function call, or
// dotted variable reference.
func (p *parser) parsePrimary() (Expression, error) {
	t := p.next()
	switch t.kind {
	case tTrue:
		return literalExpr{value: true}, nil
	case tFalse:
		return literalExpr{value: false}, nil
	case tString:
		return literalExpr{value: t.text}, nil
	case tInt:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q at %d:%d", t.text, t.line, t.col)
		}
		return literalExpr{value: float64(n)}, nil
	case tFloat:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q at %d:%d", t.text, t.line, t.col)
		}
		return literalExpr{value: f}, nil
	case tLParen:
		return p.parseParen()
	case tIdent:
		return p.parseIdent(t)
	default:
		return nil, fmt.Errorf("unexpected %q at %d:%d", t.text, t.line, t.col)
	}
}

// parseParen parses a parenthesized sub-expression.
func (p *parser) parseParen() (Expression, error) {
	inner, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if c := p.peek(); c.kind != tRParen {
		return nil, fmt.Errorf("expected ')' at %d:%d", c.line, c.col)
	}
	p.next()
	return inner, nil
}

// parseIdent parses either a stdlib function call or a dotted variable path,
// beginning with the already-consumed identifier token.
func (p *parser) parseIdent(first token) (Expression, error) {
	if p.peek().kind == tLParen {
		return p.parseCall(first)
	}

	path := first.text
	for p.peek().kind == tDot {
		p.next() // consume '.'
		seg := p.next()
		if seg.kind != tIdent {
			return nil, fmt.Errorf("expected field name after '.' at %d:%d", seg.line, seg.col)
		}
		path += "." + seg.text
	}
	return varExpr{path: path}, nil
}

// parseCall parses a stdlib function call, rejecting any name outside the
// stdlib set.
func (p *parser) parseCall(name token) (Expression, error) {
	if !stdlibFuncs[name.text] {
		return nil, fmt.Errorf("unknown function %q at %d:%d (only len, has, empty are allowed)", name.text, name.line, name.col)
	}
	p.next() // consume '('

	var args []Expression
	if p.peek().kind != tRParen {
		for {
			arg, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.peek().kind != tComma {
				break
			}
			p.next() // consume ','
		}
	}
	if c := p.peek(); c.kind != tRParen {
		return nil, fmt.Errorf("expected ')' to close %s() at %d:%d", name.text, c.line, c.col)
	}
	p.next() // consume ')'
	return callExpr{name: name.text, args: args}, nil
}
