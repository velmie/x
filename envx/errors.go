package envx

import (
	"fmt"
	"strings"
)

// ErrorCode defines string error
type ErrorCode string

// ErrorCode returns error message
func (e ErrorCode) Error() string {
	return string(e)
}

const (
	// ErrRequired indicates that required value is missing
	ErrRequired = ErrorCode("value is required")
	// ErrEmpty indicates that value is empty
	ErrEmpty = ErrorCode("value is empty")
	// ErrInvalidValue indicates that given value is not valid
	ErrInvalidValue = ErrorCode("invalid value")
)

// Error provides error details
type Error struct {
	VarName string
	Reason  string
	Cause   error
}

func (e Error) Error() string {
	sb := new(strings.Builder)
	sb.WriteString(fmt.Sprintf("variable %q", e.VarName))
	if e.Reason != "" {
		sb.WriteString(" " + e.Reason)
	}
	if e.Cause != nil {
		sb.WriteString(": " + e.Cause.Error())
	}
	return sb.String()
}

func (e Error) Unwrap() error {
	return e.Cause
}
