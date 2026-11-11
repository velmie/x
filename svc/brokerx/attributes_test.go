package brokerx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
)

func TestRecoverWithAttributesRetainsPanicAndDiagnosticFields(t *testing.T) {
	cause := &apperror.Error{Category: apperror.Conflict, Code: "CONFLICT", Message: "concurrent update"}
	for _, panicInstead := range []bool{false, true} {
		var logs bytes.Buffer
		callback := brokerx.RecoverWithAttributes(func(context.Context, string) error {
			if panicInstead {
				panic(cause)
			}
			return cause
		}, func(id string) []slog.Attr { return []slog.Attr{slog.String("order_id", id)} })
		h := broker.NewTypedHandler(func([]byte) (string, error) { return "order-7", nil }, callback).WithMiddleware(
			broker.LogProcessing(slog.New(slog.NewJSONHandler(&logs, nil)), brokerx.PolicyLogConfig(brokerx.ErrorPolicy{})),
			brokerx.HandleErrors(brokerx.ErrorPolicy{RetryDelay: time.Second}),
			broker.RecoverPanics(),
		)
		result, err := h.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
		if err != nil {
			t.Fatal(err)
		}
		var got error
		if panicInstead {
			v, ok := result.(broker.RetryAfter)
			if !ok {
				t.Fatalf("panic result: %T", result)
			}
			got = v.Cause
			var panicErr *broker.PanicError
			if !errors.As(got, &panicErr) || len(panicErr.Stack) == 0 {
				t.Fatalf("panic lost: %v", got)
			}
		} else {
			v, ok := result.(broker.Reject)
			if !ok {
				t.Fatalf("rejection result: %T", result)
			}
			got = v.Cause
		}
		if !errors.Is(got, cause) || !strings.Contains(got.Error(), "concurrent update") {
			t.Fatalf("cause lost: %v", got)
		}
		for _, want := range []string{"order-7", "CONFLICT", "concurrent update"} {
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("missing %q: %s", want, logs.String())
			}
		}
	}
}

func TestProcessingLogReportsTransactionPhase(t *testing.T) {
	var output bytes.Buffer
	cause := errors.Join(sqltx.ErrCommit, errors.New("database unavailable"))
	h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, cause
	}).WithMiddleware(
		broker.LogProcessing(slog.New(slog.NewJSONHandler(&output, nil)), brokerx.PolicyLogConfig(brokerx.ErrorPolicy{})),
		brokerx.HandleErrors(brokerx.ErrorPolicy{}),
	)
	_, err := h.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if err != nil || !strings.Contains(output.String(), `"transaction_phase":"commit"`) ||
		!strings.Contains(output.String(), `"transaction_outcome":"unknown"`) ||
		!strings.Contains(output.String(), "database unavailable") {
		t.Fatalf("error=%v log=%s", err, output.String())
	}
}

func TestProcessingLogKeepsCooperativeCancellationQuiet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	h := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, ctx.Err()
	}).WithMiddleware(
		broker.LogProcessing(slog.New(slog.NewJSONHandler(&output, nil)), brokerx.PolicyLogConfig(brokerx.ErrorPolicy{})),
		brokerx.HandleErrors(brokerx.ErrorPolicy{}),
	)
	_, err := h.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
	if err != ctx.Err() || output.Len() != 0 {
		t.Fatalf("error=%v log=%s", err, output.String())
	}
}
