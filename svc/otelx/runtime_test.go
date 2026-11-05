package otelx

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestRuntimeDisabledDoesNotAcquireOrMutateGlobals(t *testing.T) {
	oldTracer := otel.GetTracerProvider()
	oldMeter := otel.GetMeterProvider()
	oldPropagation := otel.GetTextMapPropagator()
	var called atomic.Bool
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				called.Store(true)
				return nil, nil
			},
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				called.Store(true)
				return nil, nil
			},
			FlushTimeout: -1,
		},
	)
	if err != nil || called.Load() {
		t.Fatalf("disabled runtime acquired resources: runtime=%v err=%v", r, err)
	}
	if r.TracerProvider == nil || r.MeterProvider == nil || r.Propagator == nil || r.PrometheusHandler != nil {
		t.Fatal("disabled runtime has missing or unexpected handles")
	}
	if otel.GetTracerProvider() != oldTracer || otel.GetMeterProvider() != oldMeter || otel.GetTextMapPropagator() != oldPropagation {
		t.Fatal("Open changed a global provider or propagator")
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOpenValidatesAndCleansPartialAcquisition(t *testing.T) {
	if _, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled: true,
		},
	); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid configuration, got %v", err)
	}
	failure := errors.New("metric constructor")
	cleanup := errors.New("trace cleanup")
	ignored := errors.New("failed metric exporter cleanup")
	traceExporter := &trackingSpanExporter{
		shutdownErr: cleanup,
	}
	partial := &trackingMetricExporter{
		shutdownErr: ignored,
	}
	_, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return traceExporter, nil
			},
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return partial, failure
			},
		},
	)
	if !errors.Is(err, failure) || !errors.Is(err, cleanup) || errors.Is(err, ignored) {
		t.Fatalf("incorrect construction or cleanup cause: %v", err)
	}
	if traceExporter.shutdowns.Load() != 1 || partial.shutdowns.Load() != 0 {
		t.Fatalf("wrong exporter ownership: trace=%d metric=%d", traceExporter.shutdowns.Load(), partial.shutdowns.Load())
	}
}

func TestOpenIgnoresSpanExporterReturnedWithError(t *testing.T) {
	failure := errors.New("trace constructor")
	exporter := &trackingSpanExporter{}
	_, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return exporter, failure
			},
		},
	)
	if !errors.Is(err, failure) || exporter.shutdowns.Load() != 0 {
		t.Fatalf("failed exporter must remain factory-owned: error=%v shutdowns=%d", err, exporter.shutdowns.Load())
	}
}

func TestShutdownWaitsForExporterAfterCanceledFlushAndIsShared(t *testing.T) {
	exporter := &trackingSpanExporter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return exporter, nil
			},
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithMaxExportBatchSize(1),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	<-exporter.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const callers = 4
	results := make(chan error, callers)
	var started sync.WaitGroup
	started.Add(callers)
	for range callers {
		go func() {
			started.Done()
			results <- r.Shutdown(ctx)
		}()
	}
	started.Wait()
	select {
	case err := <-results:
		t.Fatalf("returned before exporter work finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(exporter.release)
	for range callers {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("expected shared canceled flush cause, got %v", err)
		}
	}
	if exporter.shutdowns.Load() != 1 {
		t.Fatalf("exporter shutdowns: got %d, want 1", exporter.shutdowns.Load())
	}
}

type trackingSpanExporter struct {
	entered     chan struct{}
	release     chan struct{}
	shutdowns   atomic.Int64
	shutdownErr error
}

func (e *trackingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	if e.entered != nil {
		close(e.entered)
		<-e.release
	}
	return nil
}

func (e *trackingSpanExporter) Shutdown(context.Context) error {
	e.shutdowns.Add(1)
	return e.shutdownErr
}

type trackingMetricExporter struct {
	failingMetricExporter
	shutdowns   atomic.Int64
	shutdownErr error
}

func (e *trackingMetricExporter) Shutdown(context.Context) error {
	e.shutdowns.Add(1)
	return e.shutdownErr
}

func TestIndependentRuntimesAndGlobalProviders(t *testing.T) {
	oldTracer := otel.GetTracerProvider()
	oldMeter := otel.GetMeterProvider()
	oldPropagation := otel.GetTextMapPropagator()
	firstExporter := &countingSpanExporter{}
	secondExporter := &countingSpanExporter{}
	open := func(exporter *countingSpanExporter) *Runtime {
		t.Helper()
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
		return r
	}
	first := open(firstExporter)
	second := open(secondExporter)
	_, span := first.TracerProvider.Tracer("first").Start(context.Background(), "one")
	span.End()
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, span = second.TracerProvider.Tracer("second").Start(context.Background(), "two")
	span.End()
	if err := second.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if firstExporter.spans.Load() != 1 || secondExporter.spans.Load() != 1 {
		t.Fatalf("runtimes interfered: first=%d second=%d", firstExporter.spans.Load(), secondExporter.spans.Load())
	}
	if otel.GetTracerProvider() != oldTracer || otel.GetMeterProvider() != oldMeter || otel.GetTextMapPropagator() != oldPropagation {
		t.Fatal("enabled runtimes changed globals")
	}
}

type countingSpanExporter struct {
	spans atomic.Int64
}

func (e *countingSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans.Add(int64(len(spans)))
	return nil
}

func (*countingSpanExporter) Shutdown(context.Context) error { return nil }

func TestOpenHandlesTypedNilFromNativeExporterFailure(t *testing.T) {
	_, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracegrpc.New(
					ctx,
					otlptracegrpc.WithEndpoint("%"),
					otlptracegrpc.WithInsecure(),
				)
			},
		},
	)
	var escape url.EscapeError
	if !errors.As(err, &escape) {
		t.Fatalf("native constructor cause lost: %v", err)
	}
}
