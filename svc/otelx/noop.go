package otelx

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type NoopExporter struct{}

func (n NoopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (n NoopExporter) Shutdown(ctx context.Context) error {
	return nil
}
