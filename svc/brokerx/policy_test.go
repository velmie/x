package brokerx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
)

func TestHandleErrorsClassifiesProcessingFailures(t *testing.T) {
	terminal := &apperror.Error{Category: apperror.Conflict, Code: "CONFLICT"}
	failure := errors.New("database failed")
	for _, tt := range []struct {
		name      string
		cause     error
		cancel    bool
		retryable bool
		want      string
	}{
		{"success", nil, false, false, "handled"},
		{"decode", &broker.DecodeError{Cause: failure}, false, false, "reject"},
		{"metadata", &broker.StageError{Stage: broker.StageMetadata, Cause: failure}, false, false, "reject"},
		{"nested decode", &broker.StageError{Stage: broker.StageHandle, Cause: &broker.DecodeError{Cause: failure}}, false, false, "retry"},
		{"application", terminal, false, false, "reject"},
		{"exception", terminal, false, true, "retry"},
		{"begin", errors.Join(sqltx.ErrBegin, terminal), false, false, "retry"},
		{"uncertain commit", errors.Join(sqltx.ErrCommit, terminal), false, false, "retry"},
		{"transaction panic", &sqltx.PanicError{Value: terminal}, false, false, "retry"},
		{"handler panic", &broker.PanicError{Value: terminal}, false, false, "retry"},
		{"canceled", context.Canceled, true, false, "canceled"},
		{"canceled success", nil, true, false, "canceled"},
		{"mixed cancellation", errors.Join(context.Canceled, failure), true, false, "joined"},
		{"canceled transaction panic", &sqltx.PanicError{Value: context.Canceled}, true, false, "joined"},
		{"canceled uncertain commit", errors.Join(sqltx.ErrCommit, context.Canceled), true, false, "joined"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
				if tt.cause != nil {
					return nil, tt.cause
				}
				return broker.Handled{}, nil
			}).WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{RetryDelay: time.Second, Retryable: func(error) bool { return tt.retryable }}))
			got, err := h.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
			switch tt.want {
			case "handled":
				if _, ok := got.(broker.Handled); !ok || err != nil {
					t.Fatalf("got %T, %v", got, err)
				}
			case "retry":
				v, ok := got.(broker.RetryAfter)
				if !ok || v.Delay != time.Second || !errors.Is(v.Cause, tt.cause) || err != nil {
					t.Fatalf("got %#v, %v", got, err)
				}
			case "reject":
				v, ok := got.(broker.Reject)
				if !ok || !errors.Is(v.Cause, tt.cause) || err != nil {
					t.Fatalf("got %#v, %v", got, err)
				}
			case "canceled":
				if got != nil || err != ctx.Err() {
					t.Fatalf("got %#v, %v", got, err)
				}
			case "joined":
				if got != nil || !errors.Is(err, tt.cause) || !errors.Is(err, ctx.Err()) {
					t.Fatalf("got %#v, %v", got, err)
				}
			default:
				t.Fatalf("unexpected case %q", tt.want)
			}
			var redelivery, rejection bool
			for _, r := range h.Requirements() {
				switch r.(type) {
				case broker.RedeliveryRequirement:
					redelivery = true
				case broker.RejectRequirement:
					rejection = true
				}
			}
			if !redelivery || !rejection {
				t.Fatalf("requirements: %#v", h.Requirements())
			}
		})
	}
}
