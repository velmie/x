package brokerx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/brokerx"
)

func TestWrapProcessingPreservesIndependentTimeoutDuringShutdown(t *testing.T) {
	for _, tc := range []struct {
		name       string
		middleware broker.HandlerMiddleware
	}{
		{name: "handle", middleware: brokerx.HandleErrors(brokerx.ErrorPolicy{})},
		{name: "retry", middleware: brokerx.RetryErrors(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
				cancel()
				return nil, context.DeadlineExceeded
			})
			var output bytes.Buffer
			wrapped := brokerx.WrapProcessing(handler, slog.New(slog.NewJSONHandler(&output, nil)), brokerx.ProcessingConfig{
				Errors: tc.middleware,
				Log:    brokerx.ProcessingLogConfig(),
			})
			result, err := wrapped.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
			if result != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("result=%v error=%v, want both canceled and independent deadline", result, err)
			}
			if output.Len() == 0 {
				t.Fatal("independent timeout was not logged")
			}
		})
	}
}

func TestWrapProcessingSerializedRetrySeverity(t *testing.T) {
	conflict := errors.New("conflict")
	policy := brokerx.ErrorPolicy{Retryable: func(err error) bool { return errors.Is(err, conflict) }}
	for _, tc := range []struct {
		name       string
		cause      error
		panicValue bool
		want       string
	}{
		{name: "retryable conflict", cause: conflict, want: "WARN"},
		{name: "handler panic", cause: conflict, panicValue: true, want: "ERROR"},
		{name: "transaction panic", cause: &sqltx.PanicError{Value: conflict}, want: "ERROR"},
		{name: "uncertain commit", cause: errors.Join(sqltx.ErrCommit, conflict), want: "ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
				if tc.panicValue {
					panic(tc.cause)
				}
				return nil, tc.cause
			})
			wrapped := brokerx.WrapProcessing(handler, slog.New(slog.NewJSONHandler(&output, nil)), brokerx.ProcessingConfig{
				Errors: brokerx.HandleErrors(policy),
				Log:    brokerx.PolicyLogConfig(policy),
			})
			result, err := wrapped.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
			if _, ok := result.(broker.RetryAfter); !ok || err != nil {
				t.Fatalf("result=%v error=%v", result, err)
			}
			var record struct {
				Level string `json:"level"`
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatalf("decode log %q: %v", output.String(), err)
			}
			if record.Level != tc.want {
				t.Fatalf("level=%s want=%s log=%s", record.Level, tc.want, output.String())
			}
		})
	}
}
