package otelx

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Setup initializes legacy tracing, installs its provider and propagator as
// globals, and returns the initialized components. The legacy ConfigFromEnv
// defaults and exporter options remain unchanged.
//
// Deprecated: Use Open with an explicit resource and exporter factories. Open
// returns a runtime that owns shutdown and does not install global providers.
func Setup(ctx context.Context, c *Config, op ...otlpOption) (*SetupResult, error) {
	opts := &otlpOptions{}
	for _, o := range op {
		o(opts)
	}
	var err error
	exporter := opts.exporter
	if c.Disabled {
		exporter = NoopExporter{}
	} else if exporter == nil {
		exporter, err = CreateExporter(ctx, c)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("cannot create exporter: %w", err), closeSpanExporter(ctx, exporter))
		}
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
		return nil, errors.Join(fmt.Errorf("cannot create tracer provider: %w", err), closeSpanExporter(ctx, exporter))
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
	// TracerProvider is the legacy provider installed by Setup.
	TracerProvider trace.TracerProvider
	// Exporter is the exporter used by the provider.
	Exporter sdktrace.SpanExporter
	// Propagator is the legacy propagator installed by Setup.
	Propagator propagation.TextMapPropagator
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

// WithTracerProviderOptions adds native options to the legacy tracer provider.
func WithTracerProviderOptions(toOpts ...sdktrace.TracerProviderOption) otlpOption {
	return func(opts *otlpOptions) {
		opts.tpOpts = append(opts.tpOpts, toOpts...)
	}
}

// WithResourceDetectors adds native resource detectors to legacy Setup.
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
