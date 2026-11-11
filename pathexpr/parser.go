package pathexpr

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var numberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type parser struct {
	s string
	i int
}

func newParser(s string) (*parser, error) {
	if !utf8.ValidString(s) {
		for i := 0; i < len(s); {
			_, n := utf8.DecodeRuneInString(s[i:])
			if n == 1 && s[i] >= utf8.RuneSelf {
				p := &parser{
					s: s,
					i: i,
				}
				return nil, p.fail("invalid UTF-8")
			}
			i += n
		}
	}
	return &parser{
		s: s,
	}, nil
}
func (p *parser) position() Position {
	line, col := 1, 1
	for i := 0; i < p.i; {
		r, n := utf8.DecodeRuneInString(p.s[i:])
		i += n
		switch r {
		case '\r':
			line++
			col = 1
			if i < p.i && p.s[i] == '\n' {
				i++
			}
		case '\n':
			line++
			col = 1
		default:
			col++
		}
	}
	return Position{
		Offset: p.i,
		Line:   line,
		Column: col,
	}
}
func (p *parser) fail(message string) error {
	return &SyntaxError{
		Pos: p.position(),
		Err: errors.New(message),
	}
}
func (p *parser) space() {
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if !unicode.IsSpace(r) {
			return
		}
		p.i += n
	}
}
func (p *parser) take(c byte) bool {
	p.space()
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}
func (p *parser) key() (string, error) {
	p.space()
	start := p.i
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if unicode.IsSpace(r) || strings.ContainsRune(".,:{}[]?'\"\\()", r) {
			break
		}
		p.i += n
	}
	if start == p.i {
		return "", p.fail("expected key")
	}
	return p.s[start:p.i], nil
}
func (p *parser) expression() (node, error) {
	p.space()
	if p.i == len(p.s) {
		return nil, p.fail("expected expression")
	}
	switch p.s[p.i] {
	case '{':
		return p.object()
	case '\'':
		return p.stringLiteral()
	}
	start := p.i
	path, err := p.path()
	if err != nil {
		return nil, err
	}
	if len(path.steps) == 1 && !path.steps[0].indexed {
		switch path.text {
		case "true":
			return literalNode{
				value: true,
			}, nil
		case "false":
			return literalNode{
				value: false,
			}, nil
		}
	}
	if numberPattern.MatchString(strings.TrimSpace(p.s[start:p.i])) {
		return literalNode{
			value: json.Number(path.text),
		}, nil
	}
	return path, nil
}
func (p *parser) stringLiteral() (node, error) {
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		p.i += n
		if r == '\'' {
			return literalNode{
				value: b.String(),
			}, nil
		}
		if r == '\\' {
			if p.i == len(p.s) {
				return nil, p.fail("incomplete escape")
			}
			escaped := p.i
			c := p.s[p.i]
			p.i++
			switch c {
			case '\\', '\'':
				b.WriteByte(c)
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				p.i = escaped
				return nil, p.fail("invalid escape")
			}
			continue
		}
		b.WriteRune(r)
	}
	return nil, p.fail("unterminated string")
}
func (p *parser) object() (node, error) {
	p.i++
	result := objectNode{}
	seen := make(map[string]bool)
	if p.take('}') {
		return result, nil
	}
	for {
		p.space()
		start := p.i
		pos := p.position()
		key, err := p.key()
		if err == nil && p.take(':') {
			var n node
			n, err = p.expression()
			if err != nil {
				return nil, err
			}
			result.fields = append(result.fields, field{
				key:   key,
				value: n,
				pos:   pos,
			})
		} else {
			p.i = start
			n, e := p.path()
			if e != nil {
				return nil, e
			}
			if n.text == "true" || n.text == "false" || numberPattern.MatchString(strings.TrimSpace(p.s[start:p.i])) {
				return nil, p.fail("literal shorthand is invalid")
			}
			key = n.text
			result.fields = append(result.fields, field{
				key:   key,
				value: n,
				pos:   pos,
			})
		}
		if p.take('?') {
			last := &result.fields[len(result.fields)-1]
			last.omit = omitMissing
			if p.i < len(p.s) && p.s[p.i] == '?' {
				p.i++
				last.omit = omitEmpty
			}
		}
		if seen[key] {
			p.i = start
			return nil, p.fail("duplicate output key")
		}
		seen[key] = true
		if p.take('}') {
			return result, nil
		}
		if !p.take(',') {
			return nil, p.fail("expected comma or closing brace")
		}
		p.space()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			return nil, p.fail("trailing comma")
		}
	}
}
func (p *parser) path() (pathNode, error) {
	p.space()
	result := pathNode{}
	var b strings.Builder
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		result.text = "."
		return result, nil
	}
	first := true
	for {
		p.space()
		pos := p.position()
		if p.i < len(p.s) && p.s[p.i] == '[' {
			p.i++
			p.space()
			start := p.i
			for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
				p.i++
			}
			digits := p.s[start:p.i]
			if digits == "" || len(digits) > 1 && digits[0] == '0' {
				return pathNode{}, p.fail("invalid index")
			}
			n, err := strconv.ParseUint(digits, 10, 64)
			if err != nil {
				return pathNode{}, p.fail("index out of range")
			}
			if !p.take(']') {
				return pathNode{}, p.fail("expected closing bracket")
			}
			fmt.Fprintf(&b, "[%s]", digits)
			result.steps = append(result.steps, step{
				index:   n,
				indexed: true,
				pos:     pos,
				prefix:  b.String(),
			})
		} else {
			if !first {
				if !p.take('.') {
					break
				}
				p.space()
				pos = p.position()
			}
			key, err := p.key()
			if err != nil {
				return pathNode{}, err
			}
			if !first {
				b.WriteByte('.')
			}
			b.WriteString(key)
			result.steps = append(result.steps, step{
				key:    key,
				pos:    pos,
				prefix: b.String(),
			})
		}
		first = false
		p.space()
		if p.i == len(p.s) || p.s[p.i] != '.' && p.s[p.i] != '[' {
			break
		}
	}
	result.text = b.String()
	return result, nil
}
