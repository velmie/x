package otelx

import (
	"context"
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The batch processor reports a final export failure asynchronously and omits it
// from Shutdown. Retain failures occurring while this runtime is shutting down.
type shutdownSpanExporter struct {
	exporter sdktrace.SpanExporter
	mu       sync.Mutex
	stopping bool
	failures []error
}

func (e *shutdownSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.exporter.ExportSpans(ctx, spans)
	if err != nil {
		e.mu.Lock()
		if e.stopping {
			e.failures = append(e.failures, err)
		}
		e.mu.Unlock()
	}
	return err
}

func (e *shutdownSpanExporter) Shutdown(ctx context.Context) error {
	return e.exporter.Shutdown(ctx)
}

func (e *shutdownSpanExporter) startShutdown() {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()
}

func (e *shutdownSpanExporter) shutdownFailures() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.failures...)
}

// The periodic reader can discard an in-flight export result after a canceled
// flush and a shutdown error when its final export also fails.
type shutdownMetricExporter struct {
	sdkmetric.Exporter
	mu            sync.Mutex
	stopping      bool
	failures      []error
	shutdownError error
}

func (e *shutdownMetricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	err := e.Exporter.Export(ctx, data)
	if err != nil {
		e.mu.Lock()
		if e.stopping {
			e.failures = append(e.failures, err)
		}
		e.mu.Unlock()
	}
	return err
}

func (e *shutdownMetricExporter) startShutdown() {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()
}

func (e *shutdownMetricExporter) Shutdown(ctx context.Context) error {
	err := e.Exporter.Shutdown(context.WithoutCancel(ctx))
	e.mu.Lock()
	e.shutdownError = err
	e.mu.Unlock()
	return err
}

func (e *shutdownMetricExporter) shutdownFailures() ([]error, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.failures...), e.shutdownError
}
