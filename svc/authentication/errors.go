package authentication

import (
	"context"
	"errors"
	"fmt"

	"github.com/velmie/x/authentication"
)

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}

const (
	// ErrBadToken is used in order to indicate problems with a given token
	// such as parsing errors, malformed token etc.
	ErrBadToken = Error("bad token")
	// ErrNotAuthenticated is used when token is well-formed but not valid for any reason
	// e.g. expired, invalidated etc.
	ErrNotAuthenticated = Error("not authenticated")
)

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
			return nil, fmt.Errorf("%w: %s", ErrBadToken, err)
		}
		if errors.Is(err, authentication.ErrNotAuthenticated) {
			return nil, fmt.Errorf("%w: %s", ErrNotAuthenticated, err)
		}
		if errors.Is(err, authentication.ErrTokenUnverifiable) {
			return nil, fmt.Errorf("%w: %s", ErrNotAuthenticated, err)
		}
		return nil, err
	}

	return entity, nil
}
