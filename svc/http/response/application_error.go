// Package response provides organization HTTP response envelopes and writers.
package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/velmie/x/svc/apperror"
)

const jsonMediaType = "application/json"

// ErrorOptions controls the public wording of an application error response.
type ErrorOptions struct {
	// Message overrides a common error title. It must be safe to expose to clients.
	// An empty value uses the application message or a category-based default.
	Message string
}

// FromApplicationError maps a non-nil failure to an HTTP status and envelope.
// It selects the first *apperror.Error in errors.As traversal order.
//
// For a common message, a nonempty option Message overrides the public message.
// Otherwise supported non-internal errors use their nonempty application Message,
// or their category name with underscores replaced by spaces. Internal, unknown
// and unclassified errors use "internal server error".
//
// Violations of a supported non-internal error produce field entries instead of
// a common entry. The caller retains the original error and owns logging.
func FromApplicationError(err error, opts ErrorOptions) (status int, body Errors) {
	var app *apperror.Error
	if !errors.As(err, &app) {
		return internalError(opts)
	}
	status = applicationStatus(app.Category)
	if status == http.StatusInternalServerError {
		return internalError(opts)
	}
	if len(app.Violations) != 0 {
		items := make([]*HTTPError, 0, len(app.Violations))
		for _, violation := range app.Violations {
			item := &HTTPError{
				Code:   strings.ToUpper(violation.Rule),
				Title:  violation.Message,
				Source: violation.Field,
				Target: TargetField,
			}
			if violation.Param != "" {
				item.Meta = map[string]any{violation.Rule: violation.Param}
			}
			items = append(items, item)
		}
		return status, Error(items...)
	}
	title := app.Message
	if title == "" {
		title = strings.ReplaceAll(app.Category.String(), "_", " ")
	}
	if opts.Message != "" {
		title = opts.Message
	}
	return status, Error(&HTTPError{
		Code:   app.Code,
		Title:  title,
		Target: TargetCommon,
	})
}

// WriteErrors writes an errors envelope and returns an encoding or write failure.
// It sets the JSON content type and writes status before encoding. A HEAD request
// receives the same headers and status but no body. No logging or retry is done.
func WriteErrors(w http.ResponseWriter, r *http.Request, status int, body Errors) error {
	w.Header().Set("Content-Type", jsonMediaType)
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return nil
	}
	return json.NewEncoder(w).Encode(body)
}

func applicationStatus(category apperror.Category) int {
	switch category {
	case apperror.Invalid:
		return http.StatusBadRequest
	case apperror.Forbidden:
		return http.StatusForbidden
	case apperror.NotFound:
		return http.StatusNotFound
	case apperror.Conflict, apperror.Precondition:
		return http.StatusConflict
	case apperror.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func internalError(opts ErrorOptions) (int, Errors) {
	title := opts.Message
	if title == "" {
		title = "internal server error"
	}
	return http.StatusInternalServerError, Error(&HTTPError{
		Code:   ErrCodeInternalServerError,
		Title:  title,
		Target: TargetCommon,
	})
}
