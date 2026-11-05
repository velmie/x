package otelx_test

import (
	"context"
	"fmt"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func ExampleNewPropagation() {
	policy, err := otelx.NewPropagation(
		otelx.PropagationPolicy{
			Format: otelx.FormatW3C,
			Baggage: []otelx.BaggageRule{
				{
					Key: "traffic.class",
					Values: []string{
						"interactive",
						"batch",
					},
				},
			},
		},
	)
	if err != nil {
		panic(err)
	}
	ctx := policy.Extract(
		context.Background(),
		propagation.MapCarrier{
			"traceparent": "00-12345678901234567890123456789012-1234567890123456-01",
			"baggage":     "traffic.class=interactive,private=ignored",
		},
	)
	fmt.Println(trace.SpanContextFromContext(ctx).IsRemote(), baggage.FromContext(ctx).Member("traffic.class").Value())
	// Output: true interactive
}

func ExamplePropagation_FilterContext() {
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		panic(err)
	}
	parent := trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID: trace.TraceID{1},
			SpanID:  trace.SpanID{1},
		},
	)
	ctx := trace.ContextWithSpanContext(context.Background(), parent)
	filtered := policy.FilterContext(ctx)
	fmt.Println(trace.SpanContextFromContext(filtered).IsRemote())
	// Output: false
}
