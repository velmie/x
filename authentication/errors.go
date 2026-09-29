package authentication

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
	// ErrKeySourceNotStarted means a managed source has not completed startup.
	ErrKeySourceNotStarted = Error("key source not started")
	// ErrKeySourceStarting means another Start call is already loading initial keys.
	ErrKeySourceStarting = Error("key source starting")
	// ErrKeySourceStopped means a managed source is stopped or its lifetime ended.
	ErrKeySourceStopped = Error("key source stopped")
)

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}
