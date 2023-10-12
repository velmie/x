package errorsx

import (
	"fmt"
	"net/http"

	"github.com/velmie/x/svc/http/response"
)

// PermissionError represents an error due to a lack of permissions.
type PermissionError struct {
	ResourceName string // The name of the resource that the operation was attempted on.
	ResourceID   string // The unique identifier of the resource.
	Action       string // The action that was attempted, e.g., "read", "write".
	SubjectID    string // The identifier of the subject who attempted the action, e.g. user ID.
	Cause        error  // The underlying error that led to this PermissionError.
}

// Error satisfies the error interface for PermissionError.
func (p *PermissionError) Error() string {
	sub := p.SubjectID
	if sub == "" {
		sub = "<not-specified>"
	}
	message := fmt.Sprintf(
		"subject %s does not have permission to %s resource %s",
		sub,
		p.Action,
		p.ResourceName,
	)
	if p.Cause != nil {
		message = message + ": " + p.Cause.Error()
	}
	return message
}

func (*PermissionError) HTTPError() *response.HTTPError {
	return &response.HTTPError{
		Code:       response.ErrCodeForbidden,
		Target:     response.TargetCommon,
		StatusCode: http.StatusForbidden,
	}
}

func (p *PermissionError) Unwrap() error {
	return p.Cause
}
