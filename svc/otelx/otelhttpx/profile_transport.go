package otelhttpx

import (
	"fmt"
	"net/http"
	"time"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TransportConfig supplies a fixed label and explicit instrumentation dependencies.
type TransportConfig struct {
	// Operation is the required, fixed operation label and client span name.
	Operation string
	// Tracer creates client spans and is required, including in disabled mode.
	Tracer trace.Tracer
	// Meter creates the request counter and duration histogram and is required.
	Meter metric.Meter
	// Propagator injects the outgoing parent and baggage and is required.
	Propagator propagation.TextMapPropagator
}

// NewTransport records a client span and metrics for one RoundTrip through
// response headers or an error. It clones request headers and returns the
// response body unchanged. The caller owns the body, base transport, retries,
// and TLS policy. An attempt is one RoundTrip invocation, not retries hidden in
// base. Invalid required fields wrap otelx.ErrInvalidConfig. Instrument errors
// preserve their causes. A bounded otelx.Propagation also filters the parent;
// another propagator owns its own policy.
func NewTransport(base http.RoundTripper, cfg TransportConfig) (http.RoundTripper, error) {
	if base == nil {
		return nil, fmt.Errorf("base transport is required: %w", otelx.ErrInvalidConfig)
	}
	if err := validateInstrumentation(cfg.Operation, cfg.Tracer, cfg.Meter, cfg.Propagator); err != nil {
		return nil, err
	}
	instruments, err := newRequestInstruments(cfg.Meter, clientCounterName, clientDurationName)
	if err != nil {
		return nil, err
	}
	return &clientTransport{
		base:        base,
		config:      cfg,
		instruments: instruments,
	}, nil
}

type clientTransport struct {
	base        http.RoundTripper
	config      TransportConfig
	instruments requestInstruments
}

func (t *clientTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	started := time.Now()
	attrs := []attribute.KeyValue{
		attribute.String(operationAttribute, t.config.Operation),
		attribute.String(methodAttribute, normalizeMethod(r.Method)),
	}
	parent := r.Context()
	if policy, ok := t.config.Propagator.(*otelx.Propagation); ok {
		parent = policy.FilterContext(parent)
	}
	ctx, span := t.config.Tracer.Start(
		parent,
		t.config.Operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	defer span.End()
	request := r.Clone(ctx)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	t.config.Propagator.Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := t.base.RoundTrip(request)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	attrs = append(attrs, attribute.Int(statusAttribute, status))
	if err != nil {
		attrs = append(attrs, attribute.String(errorAttribute, transportFailed))
		span.SetStatus(codes.Error, transportFailed)
	} else if status >= http.StatusBadRequest {
		span.SetStatus(codes.Error, remoteError)
	}
	span.SetAttributes(attrs...)
	t.instruments.record(ctx, started, attrs)
	return response, err
}
