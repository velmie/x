package otelx

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestOpenRejectsConflictingMetricPipelinesBeforeAcquisition(t *testing.T) {
	called := false
	_, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:            true,
			Resource:           resource.Empty(),
			PrometheusRegistry: prometheus.NewRegistry(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				called = true
				return nil, nil
			},
		},
	)
	if !errors.Is(err, ErrInvalidConfig) || called {
		t.Fatalf("conflicting metric pipelines were acquired: called=%t err=%v", called, err)
	}
}

func TestRuntimeAcceptsExplicitZeroRatioSampler(t *testing.T) {
	exporter := &countingSpanExporter{}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			Sampler:  sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0)),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "not sampled")
	span.End()
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exporter.spans.Load() != 0 {
		t.Fatalf("zero-ratio sampler exported %d spans", exporter.spans.Load())
	}
}

func TestRuntimeMergesSDKServiceNameEnvironment(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=attribute-value")
	t.Setenv("OTEL_SERVICE_NAME", "environment-value")
	exporter := &resourceSpanExporter{
		resources: make(chan *resource.Resource, 1),
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := <-exporter.resources
	value, ok := got.Set().Value("service.name")
	if !ok || value.AsString() != "environment-value" {
		t.Fatalf("SDK service-name environment was not merged: %v", got)
	}
}

type resourceSpanExporter struct {
	resources chan *resource.Resource
}

func (e *resourceSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	for _, span := range spans {
		e.resources <- span.Resource()
	}
	return nil
}

func (*resourceSpanExporter) Shutdown(context.Context) error { return nil }
