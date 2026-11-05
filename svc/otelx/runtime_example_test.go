package otelx_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func ExampleOpen() {
	ctx := context.Background()
	r, err := otelx.Open(
		ctx,
		otelx.RuntimeConfig{
			Enabled:  true,
			Resource: resource.NewWithAttributes("", attribute.String("service.name", "checkout")),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return stdouttrace.New(stdouttrace.WithWriter(io.Discard))
			},
		},
	)
	if err != nil {
		panic(err)
	}
	_, span := r.TracerProvider.Tracer("checkout").Start(ctx, "checkout")
	span.End()
	if err := r.Shutdown(ctx); err != nil {
		panic(err)
	}
	fmt.Println("closed")
	// Output: closed
}

func ExampleRuntime_Shutdown() {
	r, err := otelx.Open(context.Background(), otelx.RuntimeConfig{})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		panic(err)
	}
	fmt.Println("closed")
	// Output: closed
}

func ExampleNewErrorHandler() {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := otelx.NewErrorHandler(logger)
	// The executable can install handler with otel.SetErrorHandler(handler).
	handler.Handle(fmt.Errorf("temporary exporter failure"))
	fmt.Println("reported")
	// Output: reported
}

func ExampleOpen_prometheusAndView() {
	registry := prometheus.NewRegistry()
	view := sdkmetric.NewView(
		sdkmetric.Instrument{
			Name: "checkout.duration",
		},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{0.1, 0.5, 1},
			},
		},
	)
	r, err := otelx.Open(
		context.Background(),
		otelx.RuntimeConfig{
			Enabled:            true,
			Resource:           resource.Empty(),
			PrometheusRegistry: registry,
			Views: []sdkmetric.View{
				view,
			},
		},
	)
	if err != nil {
		panic(err)
	}
	// Serve r.PrometheusHandler on a server owned by the application.
	if err := r.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("closed")
	// Output: closed
}

func ExampleOpen_customExporter() {
	r, err := otelx.Open(
		context.Background(),
		otelx.RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return &spanSink{}, nil
			},
		},
	)
	if err != nil {
		panic(err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("closed")
	// Output: closed
}

type spanSink struct{}

func (*spanSink) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (*spanSink) Shutdown(context.Context) error                             { return nil }
