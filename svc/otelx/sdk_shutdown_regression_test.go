package otelx

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	errExportFailure   = errors.New("export failed")
	errShutdownFailure = errors.New("shutdown failed")
)

func TestSDKShutdownRetainsExporterFailures(t *testing.T) {
	t.Run(
		"trace final export and shutdown",
		func(t *testing.T) {
			exporter := &failingSpanExporter{}
			runtime, err := Open(
				context.Background(),
				RuntimeConfig{
					Enabled:         true,
					Resource:        resource.Empty(),
					NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) { return exporter, nil },
					BatchOptions:    []sdktrace.BatchSpanProcessorOption{sdktrace.WithBatchTimeout(time.Hour)},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, span := runtime.TracerProvider.Tracer("test").Start(context.Background(), "operation")
			span.End()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err = runtime.Shutdown(ctx)
			if !errors.Is(err, errExportFailure) || !errors.Is(err, errShutdownFailure) {
				t.Fatalf("shutdown must retain both exporter causes, got: %v", err)
			}
		},
	)
	t.Run(
		"metric final export and shutdown",
		func(t *testing.T) {
			exporter := &failingMetricExporter{}
			runtime, err := Open(
				context.Background(),
				RuntimeConfig{
					Enabled:           true,
					Resource:          resource.Empty(),
					NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) { return exporter, nil },
					ReaderOptions:     []sdkmetric.PeriodicReaderOption{sdkmetric.WithInterval(time.Hour)},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			counter, err := runtime.MeterProvider.Meter("test").Int64Counter("test.requests")
			if err != nil {
				t.Fatal(err)
			}
			counter.Add(context.Background(), 1)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err = runtime.Shutdown(ctx)
			if !errors.Is(err, errExportFailure) || !errors.Is(err, errShutdownFailure) {
				t.Fatalf("shutdown must retain both exporter causes, got: %v", err)
			}
		},
	)
}

type failingSpanExporter struct{}

func (*failingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errExportFailure
}

func (*failingSpanExporter) Shutdown(context.Context) error {
	return errShutdownFailure
}

type failingMetricExporter struct{}

func (*failingMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (*failingMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (*failingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return errExportFailure
}

func (*failingMetricExporter) ForceFlush(context.Context) error {
	return nil
}

func (*failingMetricExporter) Shutdown(context.Context) error {
	return errShutdownFailure
}

func TestShutdownRetainsInFlightMetricExportFailureAfterCanceledFlush(t *testing.T) {
	exporter := &cancelledFlushMetricExporter{
		entered: make(chan struct{}),
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
			ReaderOptions: []sdkmetric.PeriodicReaderOption{
				sdkmetric.WithInterval(time.Hour),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- r.Shutdown(ctx) }()
	<-exporter.entered
	cancel()
	err = <-result
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errExportFailure) {
		t.Fatalf("shutdown lost canceled flush or in-flight export cause: %v", err)
	}
}

type cancelledFlushMetricExporter struct {
	failingMetricExporter
	entered chan struct{}
	calls   atomic.Int64
}

func (e *cancelledFlushMetricExporter) Export(ctx context.Context, _ *metricdata.ResourceMetrics) error {
	if e.calls.Add(1) == 1 {
		close(e.entered)
		<-ctx.Done()
		return errExportFailure
	}
	return nil
}

func (*cancelledFlushMetricExporter) Shutdown(context.Context) error { return nil }

func TestShutdownRetainsMetricExportAndCloseStagesForSameCause(t *testing.T) {
	shared := errors.New("shared exporter failure")
	exporter := &sameFailureMetricExporter{
		cause: shared,
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = r.Shutdown(ctx)
	if !errors.Is(err, shared) || !strings.Contains(err.Error(), "export metrics during shutdown") ||
		!strings.Contains(err.Error(), "shutdown metric exporter") {
		t.Fatalf("lost an exporter failure stage: %v", err)
	}
}

type sameFailureMetricExporter struct {
	failingMetricExporter
	cause error
}

func (e *sameFailureMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return e.cause
}

func (e *sameFailureMetricExporter) Shutdown(context.Context) error {
	return e.cause
}

func TestShutdownRetainsMetricFailureWhenTraceUsesSameCause(t *testing.T) {
	metricExporter := &cancelledFlushMetricExporter{
		entered: make(chan struct{}),
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return &failingSpanExporter{}, nil
			},
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return metricExporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- r.Shutdown(ctx) }()
	<-metricExporter.entered
	cancel()
	err = <-result
	if !strings.Contains(err.Error(), "flush traces: export failed") ||
		!strings.Contains(err.Error(), "export metrics during shutdown: export failed") {
		t.Fatalf("lost a signal-specific failure stage: %v", err)
	}
}

func TestShutdownRetainsUnrelatedMetricErrorsWithBroadIsMethod(t *testing.T) {
	exporter := &broadMatchMetricExporter{
		cancelledFlushMetricExporter: &cancelledFlushMetricExporter{
			entered: make(chan struct{}),
		},
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- r.Shutdown(ctx) }()
	<-exporter.entered
	cancel()
	err = <-result
	if !strings.Contains(err.Error(), "export metrics during shutdown: export failed") ||
		!strings.Contains(err.Error(), "unrelated shutdown failure") {
		t.Fatalf("lost an unrelated exporter failure: %v", err)
	}
}

type broadMatchMetricExporter struct {
	*cancelledFlushMetricExporter
}

func (*broadMatchMetricExporter) Shutdown(context.Context) error {
	return &broadMatchError{}
}

type broadMatchError struct{}

func (*broadMatchError) Error() string { return "unrelated shutdown failure" }
func (*broadMatchError) Is(error) bool { return true }

func TestShutdownRetainsFlushAndCloseFailuresWithSameCause(t *testing.T) {
	shared := errors.New("shared exporter failure")
	exporter := &flushAndCloseMetricExporter{
		cause: shared,
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	err = r.Shutdown(context.Background())
	if !errors.Is(err, shared) || !strings.Contains(err.Error(), "flush metrics") ||
		!strings.Contains(err.Error(), "shutdown metric exporter") {
		t.Fatalf("lost a flush or exporter shutdown failure: %v", err)
	}
}

type flushAndCloseMetricExporter struct {
	failingMetricExporter
	cause error
	calls atomic.Int64
}

func (e *flushAndCloseMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	if e.calls.Add(1) == 1 {
		return e.cause
	}
	return nil
}

func (e *flushAndCloseMetricExporter) Shutdown(context.Context) error {
	return e.cause
}

func TestShutdownRetainsUncomparableNestedExporterErrors(t *testing.T) {
	exporter := &sameFailureMetricExporter{
		cause: nestedUncomparableError{
			details: []string{"safe"},
		},
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = r.Shutdown(ctx)
	if err == nil || !strings.Contains(err.Error(), "nested exporter failure") ||
		!strings.Contains(err.Error(), "export metrics during shutdown") ||
		!strings.Contains(err.Error(), "shutdown metric exporter") {
		t.Fatalf("nested exporter failure stages were lost: %v", err)
	}
}

type nestedUncomparableError struct {
	details any
}

func (nestedUncomparableError) Error() string { return "nested exporter failure" }

func TestShutdownRetainsCollectionAndCloseFailuresWithSameCause(t *testing.T) {
	shared := errors.New("shared collector and exporter failure")
	exporter := &sameFailureMetricExporter{
		cause: shared,
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewMetricExporter: func(context.Context) (sdkmetric.Exporter, error) {
				return exporter, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	meter := r.MeterProvider.Meter("test")
	gauge, err := meter.Int64ObservableGauge("test.gauge")
	if err != nil {
		t.Fatal(err)
	}
	_, err = meter.RegisterCallback(
		func(context.Context, metric.Observer) error { return shared },
		gauge,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = r.Shutdown(ctx)
	if !errors.Is(err, shared) || !strings.Contains(err.Error(), "shutdown metrics") ||
		!strings.Contains(err.Error(), "shutdown metric exporter") ||
		strings.Contains(err.Error(), "export metrics during shutdown") {
		t.Fatalf("lost or misclassified a collection or exporter failure: %v", err)
	}
}
