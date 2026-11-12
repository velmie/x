package condexpr

import (
	"errors"
	"strings"
	"unicode"

	"github.com/velmie/x/pathexpr"
)

const maxDepth = 128

type node struct {
	op       string
	args     []*node
	value    any
	path     pathexpr.Path
	text     string
	optional bool
	pos      Position
}

type parser struct {
	lex   lexer
	t     token
	err   error
	depth int
	paths []string
}

func (p *parser) next() {
	if p.err == nil {
		p.t, p.err = p.lex.next()
	}
}

func (p *parser) fail(message string) {
	if p.err == nil {
		p.err = &SyntaxError{
			Pos: p.t.pos,
			Err: errors.New(message),
		}
	}
}

func (p *parser) expect(kind string) {
	if p.t.kind != kind {
		p.fail("expected " + kind)
		return
	}
	p.next()
}

func (p *parser) enter() bool {
	if p.err != nil {
		return false
	}
	if p.depth == maxDepth {
		p.fail("nesting exceeds 128 levels")
		return false
	}
	p.depth++
	return true
}

func (p *parser) or() *node  { return p.chain("||", p.and) }
func (p *parser) and() *node { return p.chain("&&", p.not) }

func (p *parser) chain(op string, child func() *node) *node {
	first := child()
	if p.err != nil || p.t.kind != op {
		return first
	}
	n := &node{
		op:   op,
		args: []*node{first},
	}
	for p.err == nil && p.t.kind == op {
		p.next()
		n.args = append(n.args, child())
	}
	return n
}

func (p *parser) not() *node {
	if p.err != nil {
		return nil
	}
	if p.t.kind == "!" || p.t.kind == "(" {
		if !p.enter() {
			return nil
		}
		defer func() { p.depth-- }()
	}
	if p.t.kind == "!" {
		p.next()
		return &node{
			op:   "!",
			args: []*node{p.not()},
		}
	}
	if p.t.kind == "(" {
		p.next()
		n := p.or()
		p.expect(")")
		return n
	}
	// Only a call in boolean position can produce a boolean without comparison.
	if p.t.kind == "atom" && isBoolCall(p.t.text) {
		t := p.t
		p.next()
		if p.t.kind == "(" {
			return p.call(t, 2)
		}
		return p.comparison(p.atom(t))
	}
	return p.comparison(p.operand())
}

func (p *parser) comparison(left *node) *node {
	if p.err != nil {
		return nil
	}
	op := p.t.kind
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
	default:
		p.fail("expected comparison operator")
		return nil
	}
	n := &node{
		op:  op,
		pos: p.t.pos,
	}
	p.next()
	n.args = []*node{left, p.operand()}
	return n
}

func (p *parser) operand() *node {
	if p.err != nil {
		return nil
	}
	t := p.t
	if t.kind != "atom" && t.kind != "string" {
		p.fail("expected operand")
		return nil
	}
	p.next()
	if t.kind == "atom" && t.text == "len" && p.t.kind == "(" {
		return p.call(t, 1)
	}
	return p.atom(t)
}

func (p *parser) call(t token, arity int) *node {
	if !p.enter() {
		return nil
	}
	defer func() { p.depth-- }()
	n := &node{
		op:  t.text,
		pos: t.pos,
	}
	p.expect("(")
	for i := 0; i < arity; i++ {
		if i > 0 {
			p.expect(",")
		}
		n.args = append(n.args, p.operand())
	}
	p.expect(")")
	return n
}

func (p *parser) atom(t token) *node {
	n := &node{
		op:  "literal",
		pos: t.pos,
	}
	if t.kind == "string" {
		n.value = t.text
		return n
	}
	switch t.text {
	case "true":
		n.value = true
	case "false":
		n.value = false
	case "null":
	default:
		if numberPattern.MatchString(t.text) {
			value, err := parseNumber(t.text)
			if err != nil {
				p.err = &SyntaxError{
					Pos: t.pos,
					Err: err,
				}
				return nil
			}
			n.value = value
			return n
		}
		path, err := pathexpr.CompilePath(p.lex.s[t.pos.Offset:t.end])
		if err != nil {
			p.err = err
			var syntax *pathexpr.SyntaxError
			if errors.As(err, &syntax) {
				p.err = &SyntaxError{
					Pos: relativePosition(n.pos, syntax.Pos),
					Err: syntax.Err,
				}
			}
			return nil
		}
		n.op = "path"
		n.path = path
		n.text = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, t.text)
		p.paths = append(p.paths, n.text)
		if p.t.kind == "?" {
			n.optional = true
			p.next()
		}
	}
	return n
}

func isBoolCall(s string) bool { return s == "contains" || s == "starts_with" || s == "ends_with" }
