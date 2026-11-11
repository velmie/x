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
	// RetryDelay must be greater than zero; zero causes immediate redelivery.
	RetryDelay time.Duration
	Retryable  func(error) bool
}

// HandleErrors declares redelivery and rejection and classifies processing errors.
// Place broker.RecoverPanics inside HandleErrors so it can classify panics.
func HandleErrors(policy ErrorPolicy) broker.HandlerMiddleware {
	return broker.HandlerMiddleware{
		Requirements: []broker.Requirement{broker.RedeliveryRequirement{}, broker.RejectRequirement{}},
		Wrap:         wrapErrors(policy.RetryDelay, policy.Retryable, true),
	}
}

// RetryErrors retries every processing error and declares only redelivery.
// Delay must be nonnegative and supported by the adapter. Zero retries immediately.
// Place broker.RecoverPanics inside this middleware. A canceled delivery context
// suppresses settlement, preserving independent failures alongside cancellation.
// On NATS, retry at an exhausted finite MaxDeliver stops the consumer. Unlimited
// delivery can retry permanent failures indefinitely and JetStream has no automatic DLQ.
func RetryErrors(delay time.Duration) broker.HandlerMiddleware {
	return broker.HandlerMiddleware{
		Requirements: []broker.Requirement{broker.RedeliveryRequirement{}},
		Wrap:         wrapErrors(delay, nil, false),
	}
}

// RecoverWithAttributes converts callback panics to processing errors and retains
// approved attributes for logging callback failures.
func RecoverWithAttributes[T any](callback func(context.Context, T) error, attributes func(T) []slog.Attr) func(context.Context, T) error {
	return func(ctx context.Context, value T) (err error) {
		returned := false
		defer func() {
			if !returned {
				err = &broker.PanicError{
					Value: recover(),
					Stack: debug.Stack(),
				}
			}
			if err != nil {
				err = &attributedError{
					cause:      err,
					attributes: attributes(value),
				}
			}
		}()
		err = callback(ctx, value)
		returned = true
		return err
	}
}

// ProcessingLogConfig returns processing diagnostics with retries and failures
// at ERROR and rejections at INFO. Successful and canceled processing is silent.
func ProcessingLogConfig() broker.ProcessingLogConfig {
	return PolicyLogConfig(ErrorPolicy{})
}

// PolicyLogConfig adds WARN severity for retries accepted by policy.Retryable.
func PolicyLogConfig(policy ErrorPolicy) broker.ProcessingLogConfig {
	return broker.ProcessingLogConfig{
		Level: func(result broker.Disposition, err error) slog.Level {
			if _, ok := result.(broker.Reject); ok && err == nil {
				return slog.LevelInfo
			}
			if retry, ok := result.(broker.RetryAfter); ok && err == nil && retry.Cause != nil &&
				!unexpectedFailure(retry.Cause) && policy.Retryable != nil && policy.Retryable(retry.Cause) {
				return slog.LevelWarn
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
				if len(app.Violations) != 0 {
					violations := make([]logViolation, 0, len(app.Violations))
					for _, field := range app.Violations {
						violations = append(violations, logViolation{
							Field: field.Field,
							Rule:  field.Rule,
							Param: field.Param,
						})
					}
					attrs = append(attrs, slog.Any("violations", violations))
				}
			}
			var field *broker.FieldError
			if app == nil && errors.As(err, &field) {
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

type logViolation struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Param string `json:"param,omitempty"`
}

func (e *attributedError) Error() string { return e.cause.Error() }
func (e *attributedError) Unwrap() error { return e.cause }

func terminal(err error, retryable func(error) bool) bool {
	// Like broker's own processing check, the first decode or stage error in the
	// chain decides: a decode failure or a metadata stage is a poison message.
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if _, ok := cause.(*broker.DecodeError); ok {
			return true
		}
		if stage, ok := cause.(*broker.StageError); ok {
			if stage.Stage == broker.StageMetadata {
				return true
			}
			break
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

func wrapErrors(delay time.Duration, retryable func(error) bool, rejectTerminal bool) func(broker.HandlerFunc) broker.HandlerFunc {
	return func(next broker.HandlerFunc) broker.HandlerFunc {
		return func(ctx context.Context, delivery broker.Delivery) (broker.Disposition, error) {
			result, err := next(ctx, delivery)
			if err != nil {
				err, _ = transaction.NormalizePanic(err)
			}
			failure := unexpectedFailure(err)
			if ctxErr := ctx.Err(); ctxErr != nil {
				if err == nil || (!failure && transaction.OnlyCancellation(err, ctxErr)) {
					return nil, ctxErr
				}
				return nil, errors.Join(err, ctxErr)
			}
			if err == nil {
				return result, nil
			}
			if rejectTerminal && !failure && terminal(err, retryable) {
				return broker.Reject{
					Cause: err,
				}, nil
			}
			return broker.RetryAfter{
				Delay: delay,
				Cause: err,
			}, nil
		}
	}
}

func unexpectedFailure(err error) bool {
	var handlerPanic *broker.PanicError
	var txPanic *sqltx.PanicError
	return errors.As(err, &handlerPanic) || errors.As(err, &txPanic) || errors.Is(err, sqltx.ErrCommit)
}
