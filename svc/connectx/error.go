// Package connectx maps application failures and authentication rejections to
// native Connect behavior without owning application authorization.
package connectx

import (
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/apperror"
)

// ErrorOptions controls the public wording of an application error.
type ErrorOptions struct {
	// Message overrides the public message and must be safe for clients.
	// An empty value uses the application message or a category-based default.
	Message string
}

// Code maps an application category to its Connect code. Internal and unknown
// categories map to CodeInternal.
func Code(category apperror.Category) connect.Code {
	switch category {
	case apperror.Invalid:
		return connect.CodeInvalidArgument
	case apperror.Forbidden:
		return connect.CodePermissionDenied
	case apperror.NotFound:
		return connect.CodeNotFound
	case apperror.Conflict:
		return connect.CodeAborted
	case apperror.Precondition:
		return connect.CodeFailedPrecondition
	case apperror.Unavailable:
		return connect.CodeUnavailable
	default:
		return connect.CodeInternal
	}
}

// FromApplicationError converts a non-nil failure to a Connect error. It selects
// the first *apperror.Error in errors.As order, or CodeInternal if unclassified.
//
// For a common message, a nonempty option Message overrides the public message.
// Otherwise supported non-internal errors use their nonempty application Message,
// or their category name with underscores replaced by spaces. Internal, unknown
// and unclassified errors use "internal server error".
//
// No application data is sent as a detail.
// The returned error unwraps to err for diagnostics; the caller owns logging.
func FromApplicationError(err error, opts ErrorOptions) *connect.Error {
	var app *apperror.Error
	code := connect.CodeInternal
	message := "internal server error"
	if errors.As(err, &app) {
		code = Code(app.Category)
		if code != connect.CodeInternal {
			message = app.Message
			if message == "" {
				message = strings.ReplaceAll(app.Category.String(), "_", " ")
			}
		}
	}
	if opts.Message != "" {
		message = opts.Message
	}
	return connect.NewError(code, &publicError{message: message, cause: err})
}

type publicError struct {
	message string
	cause   error
}

func (e *publicError) Error() string { return e.message }
func (e *publicError) Unwrap() error { return e.cause }
