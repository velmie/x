// Package transaction maps transaction failures at service boundaries.
package transaction

import (
	"errors"

	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
)

// NormalizePanic gives recovered transaction panics precedence over their causes.
func NormalizePanic(err error) (error, bool) {
	var panicErr *sqltx.PanicError
	if !errors.As(err, &panicErr) {
		return err, false
	}
	if app, ok := err.(*apperror.Error); ok && app.Category == apperror.Internal {
		return err, true
	}
	return &apperror.Error{
		Category: apperror.Internal,
		Message:  "transaction panic",
		Cause:    err,
	}, true
}
