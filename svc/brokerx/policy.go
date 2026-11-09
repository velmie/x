// Package brokerx applies application error policy to broker handlers.
package brokerx

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/transaction"
)

// ErrorPolicy supplies the retry interval and an optional business exception.
type ErrorPolicy struct {
	RetryDelay time.Duration
	Retryable  func(error) bool
}

// HandleErrors declares redelivery and rejection and classifies processing errors.
func HandleErrors(policy ErrorPolicy) broker.HandlerMiddleware {
	return broker.HandlerMiddleware{
		Requirements: []broker.Requirement{broker.RedeliveryRequirement{}, broker.RejectRequirement{}},
		Wrap: func(next broker.HandlerFunc) broker.HandlerFunc {
			return func(ctx context.Context, delivery broker.Delivery) (broker.Disposition, error) {
				result, err := next(ctx, delivery)
				if err != nil {
					err, _ = transaction.NormalizePanic(err)
				}
				var handlerPanic *broker.PanicError
				panicked := errors.As(err, &handlerPanic)
				var txPanic *sqltx.PanicError
				panicked = panicked || errors.As(err, &txPanic)
				transactionFailure := errors.Is(err, sqltx.ErrCommit) || errors.Is(err, sqltx.ErrBegin)
				if ctx.Err() != nil {
					if err == nil || (!panicked && !transactionFailure && transaction.OnlyCancellation(err, context.Canceled, context.DeadlineExceeded)) {
						return nil, ctx.Err()
					}
					return nil, errors.Join(err, ctx.Err())
				}
				if err == nil {
					return result, nil
				}
				if !panicked && !transactionFailure && terminal(err, policy.Retryable) {
					return broker.Reject{Cause: err}, nil
				}
				return broker.RetryAfter{Delay: policy.RetryDelay, Cause: err}, nil
			}
		},
	}
}

// RecoverWithAttributes converts callback panics to processing errors and retains
// approved attributes for logging callback failures.
func RecoverWithAttributes[T any](callback func(context.Context, T) error, attributes func(T) []slog.Attr) func(context.Context, T) error {
	return func(ctx context.Context, value T) (err error) {
		returned := false
		defer func() {
			if !returned {
				err = &broker.PanicError{Value: recover(), Stack: debug.Stack()}
			}
			if err != nil {
				err = &attributedError{cause: err, attributes: attributes(value)}
			}
		}()
		err = callback(ctx, value)
		returned = true
		return err
	}
}

// ProcessingLogConfig supplies application error severity and attributes.
func ProcessingLogConfig() broker.ProcessingLogConfig {
	return broker.ProcessingLogConfig{
		Level: func(result broker.Disposition, err error) slog.Level {
			if _, ok := result.(broker.Reject); ok && err == nil {
				return slog.LevelInfo
			}
			return slog.LevelError
		},
		Attributes: func(_ context.Context, _ broker.Delivery, result broker.Disposition, err error) []slog.Attr {
			if err == nil {
				switch value := result.(type) {
				case broker.Reject:
					err = value.Cause
				case broker.RetryAfter:
					err = value.Cause
				}
			}
			var attrs []slog.Attr
			switch {
			case errors.Is(err, sqltx.ErrCommit):
				attrs = append(attrs, slog.String("transaction_phase", "commit"), slog.String("transaction_outcome", "unknown"))
			case errors.Is(err, sqltx.ErrBegin):
				attrs = append(attrs, slog.String("transaction_phase", "begin"))
			}
			var app *apperror.Error
			if errors.As(err, &app) {
				attrs = append(attrs, slog.String("category", app.Category.String()))
				if app.Code != "" {
					attrs = append(attrs, slog.String("code", app.Code))
				}
				for _, field := range app.Violations {
					attrs = append(attrs, slog.String("field", field.Field), slog.String("rule", field.Rule), slog.String("param", field.Param))
				}
			}
			var field *broker.FieldError
			if errors.As(err, &field) {
				attrs = append(attrs, slog.String("field", field.Field), slog.String("rule", field.Reason))
				var constraint interface{ Param() string }
				if errors.As(field.Cause, &constraint) {
					attrs = append(attrs, slog.String("param", constraint.Param()))
				}
			}
			var annotated *attributedError
			if errors.As(err, &annotated) {
				attrs = append(attrs, annotated.attributes...)
			}
			return attrs
		},
	}
}

type attributedError struct {
	cause      error
	attributes []slog.Attr
}

func (e *attributedError) Error() string { return e.cause.Error() }
func (e *attributedError) Unwrap() error { return e.cause }

func terminal(err error, retryable func(error) bool) bool {
	switch e := err.(type) {
	case *broker.DecodeError:
		return true
	case *broker.StageError:
		if e.Stage == broker.StageMetadata {
			return true
		}
	}
	var app *apperror.Error
	if !errors.As(err, &app) || (retryable != nil && retryable(err)) {
		return false
	}
	switch app.Category {
	case apperror.Invalid, apperror.Forbidden, apperror.NotFound, apperror.Conflict, apperror.Precondition:
		return true
	default:
		return false
	}
}
