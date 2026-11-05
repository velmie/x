package otelx

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// defaultPropagation supplies the closed default until a policy is selected.
type defaultPropagation struct{}

func (defaultPropagation) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	ctx = trace.ContextWithSpanContext(baggage.ContextWithoutBaggage(ctx), trace.SpanContext{})
	var value string
	found := false
	for _, key := range carrier.Keys() {
		lower := strings.ToLower(key)
		if !isTraceHeader(lower) {
			continue
		}
		values := carrierValues(carrier, key)
		if lower != "traceparent" || found || len(values) != 1 || len(values[0]) > 512 {
			return ctx
		}
		found = true
		value = values[0]
	}
	if !validTraceparent(value) {
		return ctx
	}
	flags, _ := strconv.ParseUint(value[53:], 16, 8)
	if flags&1 == 0 {
		value = value[:53] + "00"
	} else {
		value = value[:53] + "01"
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": value})
}

func (defaultPropagation) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	clearPropagationFields(carrier)
	sc := trace.SpanContextFromContext(ctx).WithTraceState(trace.TraceState{})
	sc = sc.WithTraceFlags(sc.TraceFlags() & trace.FlagsSampled)
	propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(ctx, sc), carrier)
}

func (defaultPropagation) Fields() []string {
	return []string{"traceparent"}
}

func validTraceparent(value string) bool {
	if len(value) != 55 || value[:3] != "00-" || value[35] != '-' || value[52] != '-' {
		return false
	}
	for i, c := range value {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func carrierValues(carrier propagation.TextMapCarrier, key string) []string {
	switch c := carrier.(type) {
	case propagation.HeaderCarrier:
		return http.Header(c)[key]
	case propagation.MapCarrier:
		return []string{c[key]}
	case propagation.ValuesGetter:
		return c.Values(key)
	default:
		return []string{carrier.Get(key)}
	}
}

func isTraceHeader(key string) bool {
	switch key {
	case "traceparent", "b3", "x-b3-traceid", "x-b3-spanid", "x-b3-parentspanid", "x-b3-sampled", "x-b3-flags":
		return true
	default:
		return false
	}
}

func clearPropagationFields(carrier propagation.TextMapCarrier) {
	for _, key := range carrier.Keys() {
		lower := strings.ToLower(key)
		if !isTraceHeader(lower) && lower != "tracestate" && lower != "baggage" {
			continue
		}
		switch c := carrier.(type) {
		case propagation.HeaderCarrier:
			delete(c, key)
		case propagation.MapCarrier:
			delete(c, key)
		}
	}
}
