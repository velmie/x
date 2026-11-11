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
	// Offset counts bytes from the start of the source.
	Offset int
	// Line is one-based; CRLF counts as one line break.
	Line int
	// Column is one-based and counts Unicode code points.
	Column int
}

// SyntaxError identifies a syntax failure and its location.
type SyntaxError struct {
	// Pos identifies the failure location.
	Pos Position
	// Err is the underlying syntax failure.
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
	// Op is the attempted operation.
	Op string
	// Path is the requested canonical path.
	Path string
	// At is the failing prefix of Path.
	At string
	// Pos identifies the failing path component.
	Pos Position
	// Err is the underlying failure.
	Err error
}

// Error describes the failed operation and cause.
func (e *EvalError) Error() string {
	return fmt.Sprintf("%s path %q at %q (line %d, column %d): %v", e.Op, e.Path, e.At, e.Pos.Line, e.Pos.Column, e.Err)
}

// Unwrap returns the cause.
func (e *EvalError) Unwrap() error { return e.Err }

// TypeError describes a container type mismatch.
type TypeError struct {
	// Want describes the required type or container.
	Want string
	// Got is the actual type, or nil for a nil value.
	Got reflect.Type
}

// Error describes the expected and actual types.
func (e *TypeError) Error() string { return fmt.Sprintf("expected %s, got %v", e.Want, e.Got) }
