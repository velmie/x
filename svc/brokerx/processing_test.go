package brokerx_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
)

type traceKey struct{}
type processingLog struct {
	t      *testing.T
	events *[]string
}

func (h processingLog) Enabled(context.Context, slog.Level) bool { return true }
func (h processingLog) Handle(ctx context.Context, _ slog.Record) error {
	if h.t != nil && ctx.Value(traceKey{}) != "trace" {
		h.t.Error("log lost trace context")
	}
	*h.events = append(*h.events, "log")
	return nil
}
func (h processingLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h processingLog) WithGroup(string) slog.Handler      { return h }

func TestWrapProcessingOrdersAndPreservesRequirements(t *testing.T) {
	var events []string
	trace := broker.HandlerMiddleware{
		Requirements: []broker.Requirement{broker.AttemptsRequirement{}},
		Wrap: func(next broker.HandlerFunc) broker.HandlerFunc {
			return func(ctx context.Context, d broker.Delivery) (broker.Disposition, error) {
				events = append(events, "trace enter")
				result, err := next(context.WithValue(ctx, traceKey{}, "trace"), d)
				events = append(events, "trace exit")
				if _, ok := result.(broker.RetryAfter); !ok || err != nil {
					t.Errorf("trace result=%v error=%v", result, err)
				}
				return result, err
			}
		},
	}
	h := broker.NewHandler(func(ctx context.Context, _ broker.Delivery) (broker.Disposition, error) {
		if ctx.Value(traceKey{}) != "trace" {
			t.Error("callback lost context")
		}
		events = append(events, "handler")
		panic("failure")
	}, broker.WithRequirements(broker.KeepAliveRequirement{
		Interval: time.Second,
	}))
	wrapped := brokerx.WrapProcessing(
		h,
		slog.New(processingLog{
			t:      t,
			events: &events,
		}),
		brokerx.ProcessingConfig{
			Trace:  trace,
			Errors: brokerx.RetryErrors(time.Second),
			Log:    brokerx.ProcessingLogConfig(),
		},
	)
	result, err := wrapped.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if _, ok := result.(broker.RetryAfter); !ok || err != nil {
		t.Fatalf("result=%v error=%v", result, err)
	}
	if len(events) != 4 || events[0] != "trace enter" || events[1] != "handler" || events[2] != "log" || events[3] != "trace exit" {
		t.Fatalf("events=%v", events)
	}
	if len(wrapped.Requirements()) != 3 {
		t.Fatalf("requirements=%#v", wrapped.Requirements())
	}
}

func TestWrapProcessingWithoutTraceUsesPolicy(t *testing.T) {
	cause := &apperror.Error{
		Category: apperror.Invalid,
		Cause:    errors.New("invalid"),
	}
	h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) { return nil, cause })
	var events []string
	wrapped := brokerx.WrapProcessing(
		h,
		slog.New(processingLog{
			events: &events,
		}),
		brokerx.ProcessingConfig{
			Errors: brokerx.HandleErrors(brokerx.ErrorPolicy{}),
			Log:    brokerx.PolicyLogConfig(brokerx.ErrorPolicy{}),
		},
	)
	result, err := wrapped.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if reject, ok := result.(broker.Reject); !ok || !errors.Is(reject.Cause, cause) || err != nil {
		t.Fatalf("result=%v error=%v", result, err)
	}
}
