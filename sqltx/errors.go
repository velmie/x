package sqltx

import (
	"errors"
	"fmt"
)

var (
	// ErrBegin identifies a failure to begin a transaction. The returned error
	// also unwraps to the original cause.
	ErrBegin = errors.New("sqltx: transaction begin error")

	// ErrCommit identifies a failure returned by Commit. It does not establish
	// that the transaction rolled back or that retrying the operation is safe.
	// The returned error also unwraps to the original cause.
	ErrCommit = errors.New("sqltx: transaction commit error")
)

// PanicError reports a panic recovered from a transaction callback.
// Rollback is attempted before the error is returned. A rollback failure is
// reported to the configured logger.
// Error retains the recovered value and stack in the historical text format.
type PanicError struct {
	Value any    // recovered value, unchanged
	Stack []byte // stack of the panicking goroutine, truncated to 4096 bytes
}

// Error returns the recovered value and stack in the existing log format.
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic recovered:\n%v\n%s", e.Value, e.Stack)
}

// Unwrap returns the recovered value if it implements error, or nil otherwise.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

type phaseError struct {
	phase error
	cause error
}

func (e *phaseError) Error() string        { return e.phase.Error() + ": " + e.cause.Error() }
func (e *phaseError) Is(target error) bool { return target == e.phase }
func (e *phaseError) Unwrap() error        { return e.cause }
