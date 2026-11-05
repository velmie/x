package validation

import (
	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"
)

// Violations converts native validation failures using root's configured field
// paths. root must have the validated root struct's type, as a value or pointer.
// Results retain the input order, Tag and Param; Message is empty and rejected
// values are omitted. Field paths, including map keys, and parameters must be
// safe to expose.
func Violations(root any, failures validator.ValidationErrors) []apperror.Violation {
	violations := make([]apperror.Violation, 0, len(failures))
	for _, failure := range failures {
		violations = append(
			violations,
			apperror.Violation{
				Field: FieldPath(root, failure),
				Rule:  failure.Tag(),
				Param: failure.Param(),
			},
		)
	}
	return violations
}
