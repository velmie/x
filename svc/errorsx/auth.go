package errorsx

import (
	"net/http"

	"github.com/velmie/x/svc/http/response"
)

// AuthenticationError represents an error due to failed authentication.
type AuthenticationError struct {
	Reason string // A brief description of why the authentication failed.
	Cause  error  // The underlying error which caused this error
}

// Error satisfies the error interface for AuthenticationError.
func (a *AuthenticationError) Error() string {
	message := "authentication failed"
	if a.Reason != "" {
		message = message + ": " + a.Reason
	}
	if a.Cause != nil {
		message = message + ": " + a.Cause.Error()
	}
	return message
}

func (*AuthenticationError) HTTPError() *response.HTTPError {
	return &response.HTTPError{
		Code:       response.ErrCodeUnauthorized,
		Target:     response.TargetCommon,
		StatusCode: http.StatusUnauthorized,
	}
}

func (a *AuthenticationError) Unwrap() error {
	return a.Cause
}
