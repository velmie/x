package brokerx

import (
	"errors"

	"github.com/velmie/broker"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/validation"
)

// FieldErrors converts broker field errors into validation violations, retaining
// the original error. A cause may provide Param() string for a rule parameter.
// Errors without field errors pass through unchanged.
func FieldErrors(err error) error {
	var violations []apperror.Violation
	var visit func(error)
	visit = func(cause error) {
		switch value := cause.(type) {
		case *broker.FieldError:
			var param string
			var constraint interface{ Param() string }
			if errors.As(value.Cause, &constraint) {
				param = constraint.Param()
			}
			violations = append(violations, apperror.Violation{
				Field: value.Field,
				Rule:  value.Reason,
				Param: param,
			})
		case interface{ Unwrap() []error }:
			for _, child := range value.Unwrap() {
				visit(child)
			}
		case interface{ Unwrap() error }:
			visit(value.Unwrap())
		}
	}
	visit(err)
	if len(violations) == 0 {
		return err
	}
	return &validation.Error{
		Violations: violations,
		Cause:      err,
	}
}
