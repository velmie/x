package validation

import (
	"errors"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"
)

// Error reports field violations and retains the original cause.
type Error struct {
	Violations []apperror.Violation
	Cause      error
}

// Error describes the violations and their cause.
func (e *Error) Error() string {
	details := make([]string, 0, len(e.Violations))
	for _, violation := range e.Violations {
		detail := violation.Field + ": " + violation.Rule
		if violation.Param != "" {
			detail += "=" + violation.Param
		}
		details = append(details, detail)
	}
	text := strings.Join(details, ", ")
	if e.Cause != nil {
		if text != "" {
			text += ": "
		}
		text += strings.ReplaceAll(e.Cause.Error(), "\n", "; ")
	}
	return text
}

// Unwrap returns the original cause.
func (e *Error) Unwrap() error { return e.Cause }

// Add appends one violation and joins any non-nil cause.
func (e *Error) Add(field, rule, param string, cause error) {
	e.Violations = append(e.Violations, apperror.Violation{
		Field: field,
		Rule:  rule,
		Param: param,
	})
	if cause != nil {
		e.Cause = errors.Join(e.Cause, cause)
	}
}

// AddStruct adds native validator violations and retains any error as a cause.
func (e *Error) AddStruct(root any, err error) {
	if err == nil {
		return
	}
	if failures, ok := err.(validator.ValidationErrors); ok {
		e.Violations = append(e.Violations, Violations(root, failures)...)
	}
	e.Cause = errors.Join(e.Cause, err)
}

// Err returns the collector when it contains violations or a cause, otherwise nil.
func (e *Error) Err() error {
	if len(e.Violations) == 0 && e.Cause == nil {
		return nil
	}
	return e
}

// FromStruct converts validator failures using root's configured field names.
// Non-validation errors remain available as causes.
func FromStruct(root any, cause error) error {
	var failures Error
	failures.AddStruct(root, cause)
	return failures.Err()
}

// Invalid reports one plain-Go validation failure and its optional cause.
func Invalid(field, rule, param string, cause error) error {
	var failures Error
	failures.Add(field, rule, param, cause)
	return failures.Err()
}

// At returns a copy with prefix prepended to each field of an Error.
// Other errors, including nil, pass through unchanged.
func At(prefix string, err error) error {
	failure, ok := err.(*Error)
	if !ok {
		return err
	}
	violations := make([]apperror.Violation, len(failure.Violations))
	copy(violations, failure.Violations)
	for i := range violations {
		violations[i].Field = prefix + "." + violations[i].Field
	}
	return &Error{
		Violations: violations,
		Cause:      failure.Cause,
	}
}
