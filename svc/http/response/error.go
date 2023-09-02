package response

const (
	// ErrCodeNotFound is the error code corresponding to the ErrNotFound error.
	ErrCodeNotFound = "NOT_FOUND"
	// ErrCodeInvalidRequestParameter is the error code used when a request parameter is invalid.
	ErrCodeInvalidRequestParameter = "INVALID_REQUEST_PARAMETER"
	// ErrCodeRequiredRequestParameter is the error code used when a request parameter is required but missing.
	ErrCodeRequiredRequestParameter = "REQUIRED_REQUEST_PARAMETER"
	// ErrCodeNotAllowed is the error code used when the operation is not allowed, often due to insufficient permissions.
	ErrCodeNotAllowed = "NOT_ALLOWED"
	// ErrCodeExpired is the error code used when the resource being accessed has expired.
	ErrCodeExpired = "EXPIRED"
	// ErrCodeExhausted is the error code used when a resource limit has been exhausted.
	ErrCodeExhausted = "EXHAUSTED"
	// ErrCodeRateLimitExceeded is the error code used when too many requests have been made in a given amount of time.
	ErrCodeRateLimitExceeded = "RATE_LIMIT_EXCEEDED"
	// ErrCodeUnauthorized is the error code used when a request lacks valid authentication credentials.
	ErrCodeUnauthorized = "UNAUTHORIZED"
	// ErrCodeInternalServerError is the error code used when an unhandled error occurs on the server.
	ErrCodeInternalServerError = "INTERNAL_SERVER_ERROR"
)

// Constants used for specifying the error target
const (
	// TargetCommon is used to indicate that the error is related to the whole request
	TargetCommon = "common"
	// TargetField is used to indicate that the error is related to a specific field
	TargetField = "field"
)

// Error creates an errors response
func Error(errs ...*HTTPError) Errors {
	return Errors{Errors: errs}
}

// Errors represents a set of errors in the response payload
type Errors struct {
	Errors []*HTTPError `json:"errors"`
}

// HTTPError is a custom error type designed to handle HTTP errors
type HTTPError struct {
	// Code is a unique code identifying the type of error
	Code string `json:"code"`
	// Title is an optional title for the error
	Title string `json:"title,omitempty"`
	// Source is an optional attribute pointing to the source of the error
	Source string `json:"source,omitempty"`
	// Meta is a map containing additional optional metadata related to the error
	Meta map[string]any `json:"meta,omitempty"`
	// Target is the scope of the error, either related to a certain field or the whole request
	Target string `json:"target"`
	// StatusCode could optionally carry out HTTP status code information for the error handler
	StatusCode int `json:"-"`
}

// Error method for the HTTPError type
func (e *HTTPError) Error() string {
	message := e.Code
	if e.Source != "" {
		message += " (" + e.Source + ")"
	}
	if e.Title != "" {
		message += ": " + e.Title
	}
	return message
}
