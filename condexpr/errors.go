package condexpr

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/velmie/x/pathexpr"
)

// Position identifies a byte offset and one-based line and rune column.
// CRLF counts as one line break.
type Position = pathexpr.Position

// SyntaxError identifies a syntax failure and its location in the condition.
type SyntaxError = pathexpr.SyntaxError

// EvalError identifies a failed operation without including input values.
type EvalError struct {
	// Op is "path" for path reads or number validation of a selected value,
	// "==", "!=", "<", "<=", ">" or ">=" for comparisons, or
	// "len", "contains", "starts_with" or "ends_with" for function calls.
	Op string
	// Paths lists the operation's canonical paths in source order.
	Paths []string
	// Pos identifies the operation, or the failing path component.
	Pos Position
	// Err is the underlying failure. Path failures preserve pathexpr errors.
	Err error
}

// Error describes the failed operation and cause.
func (e *EvalError) Error() string {
	var path *pathexpr.EvalError
	if e.Op == "path" && errors.As(e.Err, &path) {
		return path.Error()
	}
	operation := "operator " + e.Op
	if e.Op == "path" {
		operation = "path value"
	} else if e.Op == "len" || isBoolCall(e.Op) {
		operation = "function " + e.Op
	}
	message := fmt.Sprintf("%s (line %d, column %d): %v", operation, e.Pos.Line, e.Pos.Column, e.Err)
	if len(e.Paths) != 0 {
		var paths []string
		for _, path := range e.Paths {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
		message += " (paths: " + strings.Join(paths, ", ") + ")"
	}
	return message
}

// Unwrap returns the cause.
func (e *EvalError) Unwrap() error { return e.Err }

func position(s string, offset int) Position {
	pos := Position{
		Offset: offset,
		Line:   1,
		Column: 1,
	}
	for i := 0; i < offset; {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '\r':
			pos.Line++
			pos.Column = 1
			if i < offset && s[i] == '\n' {
				i++
			}
		case '\n':
			pos.Line++
			pos.Column = 1
		default:
			pos.Column++
		}
	}
	return pos
}

func relativePosition(start, local Position) Position {
	local.Offset += start.Offset
	if local.Line == 1 {
		local.Column += start.Column - 1
	}
	local.Line += start.Line - 1
	return local
}
