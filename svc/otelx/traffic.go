package otelx

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

const (
	// TrafficClassKey is the permitted traffic classification baggage key.
	TrafficClassKey = "traffic.class"
	// TrafficClassInteractive identifies interactive traffic.
	TrafficClassInteractive = "interactive"
	// TrafficClassBatch identifies batch traffic.
	TrafficClassBatch = "batch"
)

// TrafficClassAttributes returns only permitted traffic.class span attributes.
func TrafficClassAttributes(ctx context.Context) []attribute.KeyValue {
	member := baggage.FromContext(ctx).Member(TrafficClassKey)
	if len(member.Properties()) != 0 {
		return nil
	}
	switch member.Value() {
	case TrafficClassInteractive, TrafficClassBatch:
		return []attribute.KeyValue{attribute.String(TrafficClassKey, member.Value())}
	default:
		return nil
	}
}
