package otelx

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Traceparent returns canonical W3C version-00 context, or "" when invalid.
// It includes neither baggage nor vendor state.
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	newDefaultPropagation().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// ParseTraceparent validates lowercase version-00 context.
// Invalid input returns false. Only the sampled trace flag is retained.
func ParseTraceparent(value string) (trace.SpanContext, bool) {
	normalized, ok := normalizedTraceparent(value)
	if !ok {
		return trace.SpanContext{}, false
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": normalized})
	sc := trace.SpanContextFromContext(ctx)
	return sc, sc.IsValid()
}
