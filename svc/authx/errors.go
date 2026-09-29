package authx

import (
	"context"
	"errors"
	"fmt"

	"github.com/velmie/x/authentication"
)

const (
	// ErrBadToken is used in order to indicate problems with a given token
	// such as parsing errors, malformed token etc.
	ErrBadToken = Error("bad token")
	// ErrNotAuthenticated is used when token is well-formed but not valid for any reason
	// e.g. expired, invalidated etc.
	ErrNotAuthenticated = Error("not authenticated")
)

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}

// ErrorAdapter is a wrapper for Method which generalizes errors
type ErrorAdapter struct {
	m Method
}

func NewErrorAdapter(m Method) *ErrorAdapter {
	return &ErrorAdapter{m}
}

func (a *ErrorAdapter) Authenticate(ctx context.Context, token string) (authentication.Entity, error) {
	entity, err := a.m.Authenticate(ctx, token)
	if err != nil {
		if errors.Is(err, authentication.ErrBadToken) {
			return nil, &adaptedError{category: ErrBadToken, cause: err}
		}
		if errors.Is(err, authentication.ErrNotAuthenticated) {
			return nil, &adaptedError{category: ErrNotAuthenticated, cause: err}
		}
		if errors.Is(err, authentication.ErrTokenUnverifiable) {
			return nil, &adaptedError{category: ErrNotAuthenticated, cause: err}
		}
		return nil, err
	}

	return entity, nil
}

// adaptedError keeps the adapter category as the single-unwrapping result
// while retaining the original error for errors.Is and errors.As.
type adaptedError struct {
	category Error
	cause    error
}

func (e *adaptedError) Error() string {
	return fmt.Sprintf("%s: %s", e.category, e.cause)
}

func (e *adaptedError) Unwrap() error { return e.category }

func (e *adaptedError) Is(target error) bool {
	return errors.Is(e.category, target) || errors.Is(e.cause, target)
}

func (e *adaptedError) As(target any) bool {
	return errors.As(e.category, target) || errors.As(e.cause, target)
}
