package brokerx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
)

var _ func() broker.ProcessingLogConfig = brokerx.ProcessingLogConfig

func TestRetryErrors(t *testing.T) {
	failure := errors.New("dependency failed")
	terminal := &apperror.Error{
		Category: apperror.Invalid,
		Cause:    failure,
	}
	for _, tc := range []struct {
		name   string
		cause  error
		cancel bool
		want   string
	}{
		{
			name:   "success",
			cause:  nil,
			cancel: false,
			want:   "success",
		},
		{
			name:   "application",
			cause:  terminal,
			cancel: false,
			want:   "retry",
		},
		{
			name: "decode",
			cause: &broker.DecodeError{
				Cause: failure,
			},
			cancel: false,
			want:   "retry",
		},
		{
			name: "metadata",
			cause: &broker.StageError{
				Stage: broker.StageMetadata,
				Cause: failure,
			},
			cancel: false,
			want:   "retry",
		},
		{
			name:   "active cancellation",
			cause:  context.Canceled,
			cancel: false,
			want:   "retry",
		},
		{
			name:   "active deadline",
			cause:  context.DeadlineExceeded,
			cancel: false,
			want:   "retry",
		},
		{
			name:   "begin",
			cause:  errors.Join(sqltx.ErrBegin, failure),
			cancel: false,
			want:   "retry",
		},
		{
			name:   "commit",
			cause:  errors.Join(sqltx.ErrCommit, failure),
			cancel: false,
			want:   "retry",
		},
		{
			name: "transaction panic",
			cause: &sqltx.PanicError{
				Value: failure,
			},
			cancel: false,
			want:   "retry",
		},
		{
			name: "handler panic",
			cause: &broker.PanicError{
				Value: failure,
			},
			cancel: false,
			want:   "retry",
		},
		{
			name:   "canceled success",
			cause:  nil,
			cancel: true,
			want:   "cancel",
		},
		{
			name:   "wrapped cancellation",
			cause:  fmt.Errorf("wrapped: %w", context.Canceled),
			cancel: true,
			want:   "cancel",
		},
		{
			name:   "joined cancellation and independent deadline",
			cause:  errors.Join(context.Canceled, context.DeadlineExceeded),
			cancel: true,
			want:   "joined",
		},
		{
			name:   "rollback failure",
			cause:  errors.Join(context.Canceled, failure),
			cancel: true,
			want:   "joined",
		},
		{
			name:   "uncertain commit",
			cause:  errors.Join(sqltx.ErrCommit, context.Canceled),
			cancel: true,
			want:   "joined",
		},
		{
			name: "canceled panic",
			cause: &sqltx.PanicError{
				Value: context.Canceled,
			},
			cancel: true,
			want:   "joined",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
				if tc.cause != nil {
					return nil, tc.cause
				}
				return broker.Handled{}, nil
			}).WithMiddleware(brokerx.RetryErrors(3 * time.Second))
			requirements := h.Requirements()
			if len(requirements) != 1 {
				t.Fatalf("requirements=%#v", requirements)
			}
			if _, ok := requirements[0].(broker.RedeliveryRequirement); !ok {
				t.Fatalf("requirements=%#v", requirements)
			}
			got, err := h.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
			switch tc.want {
			case "success":
				if _, ok := got.(broker.Handled); !ok || err != nil {
					t.Fatalf("got %v, %v", got, err)
				}
			case "retry":
				retry, ok := got.(broker.RetryAfter)
				if !ok || retry.Delay != 3*time.Second || !errors.Is(retry.Cause, tc.cause) || err != nil {
					t.Fatalf("got %#v, %v", got, err)
				}
				var app *apperror.Error
				if tc.cause == terminal && (!errors.As(retry.Cause, &app) || app != terminal) {
					t.Fatal("lost application cause")
				}
			case "cancel":
				if got != nil || err != ctx.Err() {
					t.Fatalf("got %v, %v", got, err)
				}
			case "joined":
				if got != nil || !errors.Is(err, tc.cause) || !errors.Is(err, ctx.Err()) {
					t.Fatalf("got %v, %v", got, err)
				}
			}
		})
	}
}

func TestRetryErrorsPassesDispositionAndRecoversPanic(t *testing.T) {
	cause := errors.New("panic cause")
	for _, panicCallback := range []bool{false, true} {
		h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
			if panicCallback {
				panic(cause)
			}
			return broker.Reject{
				Cause: cause,
			}, nil
		}).WithMiddleware(brokerx.RetryErrors(time.Second), broker.RecoverPanics())
		result, err := h.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
		if err != nil {
			t.Fatal(err)
		}
		if panicCallback {
			retry, ok := result.(broker.RetryAfter)
			var p *broker.PanicError
			if !ok || !errors.As(retry.Cause, &p) || !errors.Is(retry.Cause, cause) {
				t.Fatalf("result=%#v", result)
			}
		} else if reject, ok := result.(broker.Reject); !ok || reject.Cause != cause {
			t.Fatalf("result=%#v", result)
		}
	}
}

func TestRetryProcessingDiagnostics(t *testing.T) {
	var output bytes.Buffer
	cause := &apperror.Error{
		Category: apperror.Invalid,
		Message:  "safe detail secret-token",
		Violations: []apperror.Violation{
			{
				Field: "email",
				Rule:  "required",
			},
		},
	}
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if err, ok := attr.Value.Any().(error); ok {
				attr.Value = slog.StringValue(strings.ReplaceAll(err.Error(), "secret-token", "[redacted]"))
			}
			return attr
		},
	}))
	h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) { return nil, cause }).WithMiddleware(broker.LogProcessing(logger, brokerx.ProcessingLogConfig()), brokerx.RetryErrors(0))
	_, err := h.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"safe detail", "email", "required", "[redacted]"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "secret-token") {
		t.Fatal(output.String())
	}
	output.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = h.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// This callback returns an independent error, so cancellation must not erase it.
	if !strings.Contains(output.String(), "safe detail") {
		t.Fatal(output.String())
	}
}
