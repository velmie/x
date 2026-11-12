package condexpr

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

type token struct {
	kind string
	text string
	pos  Position
	end  int
}

type lexer struct {
	s   string
	i   int
	pos Position
}

func (l *lexer) next() (token, error) {
	for l.i < len(l.s) {
		r, size := utf8.DecodeRuneInString(l.s[l.i:])
		if !unicode.IsSpace(r) {
			break
		}
		l.i += size
	}
	t := token{
		pos: l.position(),
	}
	if l.i == len(l.s) {
		return t, nil
	}
	c := l.s[l.i]
	if c == '\'' {
		return l.stringToken()
	}
	if strings.ContainsRune("()?,=!<>&|", rune(c)) {
		l.i++
		if l.i < len(l.s) {
			next := l.s[l.i]
			if strings.ContainsRune("=!<>", rune(c)) && next == '=' || (c == '&' || c == '|') && next == c {
				l.i++
			}
		}
		t.kind = l.s[t.pos.Offset:l.i]
		return t, nil
	}
	// Keep whitespace within paths so CompilePath validates token separation.
	for l.i < len(l.s) {
		r, size := utf8.DecodeRuneInString(l.s[l.i:])
		if r == utf8.RuneError && size == 1 {
			return t, l.fail("invalid UTF-8")
		}
		if strings.ContainsRune("()?,=!<>&|'", r) {
			break
		}
		l.i += size
	}
	t.kind = "atom"
	t.end = l.i
	t.text = strings.TrimSpace(l.s[t.pos.Offset:l.i])
	return t, nil
}

func (l *lexer) stringToken() (token, error) {
	t := token{
		kind: "string",
		pos:  l.position(),
	}
	l.i++
	var b strings.Builder
	for l.i < len(l.s) {
		r, size := utf8.DecodeRuneInString(l.s[l.i:])
		if r == utf8.RuneError && size == 1 {
			return t, l.fail("invalid UTF-8")
		}
		l.i += size
		if r == '\'' {
			t.text = b.String()
			return t, nil
		}
		if r == '\\' {
			if l.i == len(l.s) {
				return t, l.fail("incomplete escape")
			}
			c := l.s[l.i]
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
				return t, l.fail("invalid escape")
			}
			l.i++
		} else {
			b.WriteRune(r)
		}
	}
	return t, l.fail("unterminated string")
}

func (l *lexer) fail(message string) error {
	return &SyntaxError{
		Pos: l.position(),
		Err: errors.New(message),
	}
}

// Advance from the last position instead of rescanning the entire condition.
func (l *lexer) position() Position {
	if l.pos.Line == 0 {
		l.pos = Position{
			Line:   1,
			Column: 1,
		}
	}
	l.pos = relativePosition(l.pos, position(l.s[l.pos.Offset:l.i], l.i-l.pos.Offset))
	return l.pos
}
