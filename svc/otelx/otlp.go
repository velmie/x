package otelx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Setup initializes OpenTelemetry with specified configuration and optional overrides.
// It returns a SetupResult containing initialized components or an error if initialization fails.
// The function allows overriding default exporter and propagator settings using options.
//
// Parameters:
// - ctx: Context for initialization.
// - c: Configuration for OpenTelemetry.
// - op: Optional functions to modify the default settings of OpenTelemetry.
//
// Returns:
// - A pointer to SetupResult containing initialized components.
// - An error if the initialization fails.
func Setup(ctx context.Context, c *Config, op ...otlpOption) (*SetupResult, error) {
	opts := &otlpOptions{}
	for _, o := range op {
		o(opts)
	}
	var err error
	exporter := opts.exporter
	if exporter == nil {
		exporter, err = CreateExporter(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("cannot create exporter: %s", err)
		}
	}

	if c.Disabled {
		exporter = NoopExporter{}
	}

	propagator := opts.propagator
	if propagator == nil {
		propagator = CreatePropagator(&c.Propagation)
	}

	tp, err := CreateTracerProvider(
		ctx,
		exporter,
		c,
		TPWithResourceAttributes(opts.resourceAttributes...),
		TPWithTracerProviderOptions(opts.tpOpts...),
		TPWithResourceDetectors(opts.resourceDetectors...),
	)

	if err != nil {
		return nil, fmt.Errorf("cannot create tracer provider: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)

	result := &SetupResult{
		TracerProvider: tp,
		Exporter:       exporter,
		Propagator:     propagator,
	}
	return result, nil
}

// SetupResult contains the components resulting from setting up OpenTelemetry.
type SetupResult struct {
	TracerProvider trace.TracerProvider
	Exporter       sdktrace.SpanExporter
	Propagator     propagation.TextMapPropagator
}

// WithExporter is an option to provide a custom SpanExporter for OpenTelemetry setup.
//
// Parameters:
// - exporter: A custom implementation of SpanExporter.
//
// Returns:
// - An otlpOption to use with SetupOTLP function.
func WithExporter(exporter sdktrace.SpanExporter) otlpOption {
	return func(opts *otlpOptions) {
		opts.exporter = exporter
	}
}

// WithPropagator is an option to provide a custom TextMapPropagator for OpenTelemetry setup.
//
// Parameters:
// - propagator: A custom implementation of TextMapPropagator.
//
// Returns:
// - An otlpOption to use with SetupOTLP function.
func WithPropagator(propagator propagation.TextMapPropagator) otlpOption {
	return func(opts *otlpOptions) {
		opts.propagator = propagator
	}
}

// WithResourceAttributes is an option to provide additional resource attributes for OpenTelemetry setup.
//
// Parameters:
// - attrs: A variable number of resource attributes to be used.
//
// Returns:
// - An otlpOption to use with SetupOTLP function.
func WithResourceAttributes(attrs ...attribute.KeyValue) otlpOption {
	return func(opts *otlpOptions) {
		opts.resourceAttributes = append(opts.resourceAttributes, attrs...)
	}
}

func WithTracerProviderOptions(toOpts ...sdktrace.TracerProviderOption) otlpOption {
	return func(opts *otlpOptions) {
		opts.tpOpts = append(opts.tpOpts, toOpts...)
	}
}

func WithResourceDetectors(detector ...resource.Detector) otlpOption {
	return func(opts *otlpOptions) {
		opts.resourceDetectors = append(opts.resourceDetectors, detector...)
	}
}

// otlpOption defines a function signature for options used in configuring OpenTelemetry setup.
type otlpOption func(opts *otlpOptions)

// otlpOptions holds optional settings for OpenTelemetry configuration.
type otlpOptions struct {
	tpOpts             []sdktrace.TracerProviderOption
	resourceDetectors  []resource.Detector
	exporter           sdktrace.SpanExporter
	propagator         propagation.TextMapPropagator
	resourceAttributes []attribute.KeyValue
}
