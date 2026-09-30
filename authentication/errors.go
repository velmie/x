package authentication

import (
	"errors"
	"fmt"
)

const (
	// ErrBadToken is used to indicate problems with a given token,
	// such as parsing errors, a malformed token, etc.
	ErrBadToken = Error("bad token")
	// ErrNotAuthenticated is used when the token is well-formed but invalid for any reason,
	// e.g., expired, invalidated, etc.
	ErrNotAuthenticated = Error("not authenticated")
	// ErrTokenUnverifiable is used when the token is unverifiable for any reason
	// for example, the token is signed using unknown key
	ErrTokenUnverifiable = Error("token is unverifiable")
	// ErrKeyNotFound is used when the key is not found
	ErrKeyNotFound = Error("key not found")
	// ErrKeySetExpired means the last valid JWKS snapshot exceeds the configured age.
	ErrKeySetExpired = Error("key set expired")
	// ErrJWKSRateLimited means the shared refresh budget is exhausted.
	ErrJWKSRateLimited = Error("rate limit exceeded")
	// ErrJWKSKeyRejected means the verification policy rejected a key or key set.
	ErrJWKSKeyRejected = Error("JWKS key rejected")
	// ErrKeySourceNotStarted means a managed source has not completed startup.
	ErrKeySourceNotStarted = Error("key source not started")
	// ErrKeySourceStarting means another Start call is already loading initial keys.
	ErrKeySourceStarting = Error("key source starting")
	// ErrKeySourceStopped means a managed source is stopped, its lifetime ended,
	// or any source was permanently closed by Shutdown.
	ErrKeySourceStopped = Error("key source stopped")
)

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}

// classifiedError preserves the historical single-unwrapping category
// while exposing the original error to errors.Is and errors.As.
type classifiedError struct {
	category Error
	cause    error
}

func (e *classifiedError) Error() string {
	return fmt.Sprintf("%s: %s", e.category, e.cause)
}

func (e *classifiedError) Unwrap() error { return e.category }

func (e *classifiedError) Is(target error) bool {
	return errors.Is(e.category, target) || errors.Is(e.cause, target)
}

func (e *classifiedError) As(target any) bool {
	return errors.As(e.category, target) || errors.As(e.cause, target)
}
