package transaction

import (
	"errors"

	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
)

// OnlyCancellation reports whether all independent causes match an allowed
// cancellation. A classified rejection or uncertain commit remains a failure.
func OnlyCancellation(err error, allowed ...error) bool {
	var panicErr *sqltx.PanicError
	if errors.As(err, &panicErr) {
		return false
	}
	if errors.Is(err, sqltx.ErrCommit) {
		return false
	}
	switch value := err.(type) {
	case *apperror.Error:
		return false
	case interface{ Unwrap() []error }:
		causes := value.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !OnlyCancellation(cause, allowed...) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return OnlyCancellation(value.Unwrap(), allowed...)
	default:
		for _, cause := range allowed {
			if cause != nil && errors.Is(err, cause) {
				return true
			}
		}
		return false
	}
}
