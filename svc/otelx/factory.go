package otelx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var detectorsReg = make(map[string]resource.Detector)

func RegisterDetector(name string, detector resource.Detector) {
	detectorsReg[name] = detector
}

func CreateTracerProvider(
	ctx context.Context,
	ex sdktrace.SpanExporter,
	c *Config,
	opts ...tracerProviderOption,
) (trace.TracerProvider, error) {
	tpOpts := &tracerProviderOptions{
		tpOpts: []sdktrace.TracerProviderOption{
			sdktrace.WithBatcher(ex),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Sampling.Ratio))),
		},
	}
	for _, opt := range opts {
		opt(tpOpts)
	}

	r := c.Resource

	attrs := tpOpts.resourceAttributes
	if r.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceName(r.ServiceName))
	}
	if r.DeploymentEnvironment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(r.DeploymentEnvironment))
	}
	for k, v := range r.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	for _, name := range r.Detectors {
		if d, ok := detectorsReg[name]; ok {
			tpOpts.resourceDetectors = append(tpOpts.resourceDetectors, d)
		}
	}

	res := resource.NewWithAttributes(semconv.SchemaURL, attrs...)

	for _, d := range tpOpts.resourceDetectors {
		detected, err := d.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot detect: %w", err)
		}
		res, err = resource.Merge(res, detected)
		if err != nil {
			return nil, fmt.Errorf("cannot merge resources: %w", err)
		}
	}

	otelTpOpts := append(
		tpOpts.tpOpts,
		sdktrace.WithResource(res),
	)

	return sdktrace.NewTracerProvider(otelTpOpts...), nil
}

func CreatePropagator(c *PropagationConfig) propagation.TextMapPropagator {
	var propagators []propagation.TextMapPropagator
	if c.TraceContext {
		propagators = append(propagators, propagation.TraceContext{})
	}
	if c.Baggage {
		propagators = append(propagators, propagation.Baggage{})
	}
	if c.B3SingleHeader || c.B3MultipleHeader {
		enc := b3.B3Unspecified
		if c.B3SingleHeader {
			enc |= b3.B3SingleHeader
		}
		if c.B3MultipleHeader {
			enc |= b3.B3MultipleHeader
		}
		propagators = append(propagators, b3.New(b3.WithInjectEncoding(enc)))
	}

	return propagation.NewCompositeTextMapPropagator(propagators...)
}

func CreateExporter(ctx context.Context, c *Config) (e sdktrace.SpanExporter, err error) {
	switch c.Communication.ExportMethod {
	case ExportMethodHTTP:
		return otlptracehttp.New(ctx, otlptracehttpOptions(c)...)
	case ExportMethodGRPC:
		return otlptracegrpc.New(ctx, otlptracegrpcOptions(c)...)
	case ExportMethodStdout:
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	default:
		err = fmt.Errorf("unknown export method: %s", c.Communication.ExportMethod)
		return nil, err
	}
}

func otlptracehttpOptions(c *Config) (opts []otlptracehttp.Option) {
	if c.Communication.Endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(c.Communication.Endpoint))
	}
	if c.Security.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if c.Security.AuthorizationHeader != "" {
		h := otlptracehttp.WithHeaders(map[string]string{"Authorization": c.Security.AuthorizationHeader})
		opts = append(opts, h)
	}

	return opts
}

func otlptracegrpcOptions(c *Config) (opts []otlptracegrpc.Option) {
	if c.Communication.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(c.Communication.Endpoint))
	}
	if c.Security.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	return opts
}

// tracerProviderOptions is the structure we want to configure
type tracerProviderOptions struct {
	tpOpts             []sdktrace.TracerProviderOption
	resourceDetectors  []resource.Detector
	resourceAttributes []attribute.KeyValue
}

// tracerProviderOption is a function type for configuring tracerProviderOptions
type tracerProviderOption func(*tracerProviderOptions)

// TPWithTracerProviderOptions adds a tracerProviderOption
func TPWithTracerProviderOptions(opt ...sdktrace.TracerProviderOption) tracerProviderOption {
	return func(opts *tracerProviderOptions) {
		opts.tpOpts = append(opts.tpOpts, opt...)
	}
}

// TPWithResourceDetectors adds a resource.Detector
func TPWithResourceDetectors(detector ...resource.Detector) tracerProviderOption {
	return func(opts *tracerProviderOptions) {
		opts.resourceDetectors = append(opts.resourceDetectors, detector...)
	}
}

// TPWithResourceAttributes adds an attribute.KeyValue
func TPWithResourceAttributes(kv ...attribute.KeyValue) tracerProviderOption {
	return func(opts *tracerProviderOptions) {
		opts.resourceAttributes = append(opts.resourceAttributes, kv...)
	}
}
