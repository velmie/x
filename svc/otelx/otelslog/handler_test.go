package otelslog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/svc/otelx/otelslog"
	"go.opentelemetry.io/otel/trace"
)

func TestHandlerJSONRemovesSpoofsAndUsesUnsampledContext(t *testing.T) {
	var output bytes.Buffer
	handler := otelslog.NewHandler(slog.NewJSONHandler(&output, nil))
	logger := slog.New(handler).
		With(slog.String("TRACE_ID", "spoof"), slog.Any("bound", spoofGroup{})).
		WithGroup("request").
		WithGroup("SPAN_ID")
	ctx := trace.ContextWithSpanContext(context.Background(), correlationContext())
	logger.InfoContext(
		ctx,
		"event",
		slog.String("span_id", "spoof"),
		slog.Any("nested", spoofGroup{}),
		slog.String("safe", "value"),
	)
	if strings.Contains(output.String(), "spoof") || strings.Contains(output.String(), "SPAN_ID") {
		t.Fatalf("spoofed correlation survived: %s", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	request, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("active group missing: %v", record)
	}
	sc := correlationContext()
	if request["trace_id"] != sc.TraceID().String() || request["span_id"] != sc.SpanID().String() {
		t.Fatalf("incorrect correlation: %v", request)
	}
	if request["safe"] != "value" || request["nested"].(map[string]any)["keep"] != "value" ||
		record["bound"].(map[string]any)["keep"] != "value" {
		t.Fatalf("ordinary attributes lost: %v", record)
	}
}

func TestHandlerStripsUntrustedIDsWithoutValidContext(t *testing.T) {
	for _, sc := range []trace.SpanContext{
		{},
		trace.NewSpanContext(
			trace.SpanContextConfig{
				TraceID: trace.TraceID{1},
			},
		),
	} {
		var output bytes.Buffer
		logger := slog.New(otelslog.NewHandler(slog.NewJSONHandler(&output, nil))).
			With(slog.String("trace_id", "spoof"))
		ctx := trace.ContextWithSpanContext(context.Background(), sc)
		logger.InfoContext(
			ctx,
			"event",
			slog.String("SPAN_ID", "spoof"),
			slog.Any("nested", spoofGroup{}),
		)
		if strings.Contains(output.String(), "spoof") || strings.Contains(strings.ToLower(output.String()), "trace_id") ||
			strings.Contains(strings.ToLower(output.String()), "span_id") {
			t.Fatalf("invalid context leaked correlation: %s", output.String())
		}
	}
}

func TestHandlerTextGrouping(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(otelslog.NewHandler(slog.NewTextHandler(&output, nil))).WithGroup("request")
	ctx := trace.ContextWithSpanContext(context.Background(), correlationContext())
	logger.InfoContext(
		ctx,
		"event",
		slog.Group(
			"nested",
			slog.String("TrAcE_Id", "spoof"),
			slog.String("safe", "yes"),
		),
	)
	got := output.String()
	if strings.Contains(got, "spoof") || !strings.Contains(got, "request.trace_id="+correlationContext().TraceID().String()) ||
		!strings.Contains(got, "request.span_id="+correlationContext().SpanID().String()) ||
		!strings.Contains(got, "request.nested.safe=yes") {
		t.Fatalf("incorrect text output: %s", got)
	}
}

func TestHandlerForwardsRecordAndErrors(t *testing.T) {
	failure := errors.New("write failed")
	next := &captureHandler{
		enabled: true,
		failure: failure,
	}
	handler := otelslog.NewHandler(next)
	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	if !handler.Enabled(ctx, slog.LevelWarn) || next.enabledContext != ctx || next.enabledLevel != slog.LevelWarn {
		t.Fatal("Enabled was not forwarded")
	}
	now := time.Unix(123, 456)
	record := slog.NewRecord(now, slog.LevelWarn, "event", 12345)
	record.AddAttrs(slog.String("safe", "yes"))
	if err := handler.Handle(ctx, record); err != failure {
		t.Fatalf("handler error changed: %v", err)
	}
	if next.context != ctx || !next.record.Time.Equal(now) || next.record.Level != slog.LevelWarn ||
		next.record.Message != "event" || next.record.PC != 12345 {
		t.Fatalf("record metadata changed: %+v", next.record)
	}
}

type spoofGroup struct{}

func (spoofGroup) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("trace_id", "spoof"),
		slog.Group("child", slog.String("SPAN_ID", "spoof")),
		slog.String("keep", "value"),
	)
}

type contextKey struct{}

type captureHandler struct {
	enabled        bool
	enabledContext context.Context
	enabledLevel   slog.Level
	context        context.Context
	record         slog.Record
	failure        error
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.enabledContext = ctx
	h.enabledLevel = level
	return h.enabled
}

func (h *captureHandler) Handle(ctx context.Context, record slog.Record) error {
	h.context = ctx
	h.record = record
	return h.failure
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func correlationContext() trace.SpanContext {
	return trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4},
			SpanID:  trace.SpanID{5, 6, 7, 8},
		},
	)
}
