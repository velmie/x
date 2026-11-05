package otelslog_test

import (
	"context"
	"log/slog"
	"os"

	"github.com/velmie/x/svc/otelx/otelslog"
	"go.opentelemetry.io/otel/trace"
)

func ExampleNewHandler() {
	base := slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
				if attr.Key == slog.TimeKey {
					return slog.Attr{}
				}
				return attr
			},
		},
	)
	logger := slog.New(otelslog.NewHandler(base))
	parent := trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID: trace.TraceID{1},
			SpanID:  trace.SpanID{2},
		},
	)
	ctx := trace.ContextWithSpanContext(context.Background(), parent)
	logger.InfoContext(ctx, "request complete")
	// Output: {"level":"INFO","msg":"request complete","trace_id":"01000000000000000000000000000000","span_id":"0200000000000000"}
}
