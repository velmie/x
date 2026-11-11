package pathexpr

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrNotFound identifies a missing key or out-of-range index.
var ErrNotFound = errors.New("value not found")

// Position identifies a byte offset and one-based line and rune column.
type Position struct {
	Offset int
	Line   int
	Column int
}

// SyntaxError identifies a syntax failure and its location.
type SyntaxError struct {
	Pos Position
	Err error
}

// Error describes the syntax failure.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %v", e.Pos.Line, e.Pos.Column, e.Err)
}

// Unwrap returns the cause.
func (e *SyntaxError) Unwrap() error { return e.Err }

// EvalError identifies an operation, requested path, and failing location.
type EvalError struct {
	Op   string
	Path string
	At   string
	Pos  Position
	Err  error
}

// Error describes the failed operation and cause.
func (e *EvalError) Error() string {
	return fmt.Sprintf("%s path %q at %q (line %d, column %d): %v", e.Op, e.Path, e.At, e.Pos.Line, e.Pos.Column, e.Err)
}

// Unwrap returns the cause.
func (e *EvalError) Unwrap() error { return e.Err }

// TypeError describes a container type mismatch.
type TypeError struct {
	Want string
	Got  reflect.Type
}

// Error describes the expected and actual types.
func (e *TypeError) Error() string { return fmt.Sprintf("expected %s, got %v", e.Want, e.Got) }
