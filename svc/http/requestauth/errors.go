package requestauth

// Error defines string error
type Error string

// Error returns error message
func (e Error) Error() string {
	return string(e)
}

const (
	ErrWrongType    = Error("claim is of the wrong type")
	ErrRequired     = Error("required claim is missing")
	ErrVerification = Error("verification failed")
	ErrMissingToken = Error("token is missing")
	ErrInvalidToken = Error("token is invalid")
)
