package sqltx

import "errors"

var (
	// ErrBegin identifies a failure to begin a transaction. The returned error
	// also unwraps to the original cause.
	ErrBegin = errors.New("sqltx: transaction begin error")

	// ErrCommit identifies a failure returned by Commit. It does not establish
	// that the transaction rolled back or that retrying the operation is safe.
	// The returned error also unwraps to the original cause.
	ErrCommit = errors.New("sqltx: transaction commit error")
)

type phaseError struct {
	phase error
	cause error
}

func (e *phaseError) Error() string        { return e.phase.Error() + ": " + e.cause.Error() }
func (e *phaseError) Is(target error) bool { return target == e.phase }
func (e *phaseError) Unwrap() error        { return e.cause }
