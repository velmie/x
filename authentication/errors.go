package authentication

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}

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
)
