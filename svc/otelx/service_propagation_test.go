package otelx_test

import (
	"context"
	"testing"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestCanonicalTraceparentRoundTrip(t *testing.T) {
	const input = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-03"
	sc, ok := otelx.ParseTraceparent(input)
	if !ok || !sc.IsRemote() || !sc.IsSampled() {
		t.Fatal("valid parent rejected")
	}
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	const want = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	if got := otelx.Traceparent(ctx); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, invalid := range []string{"", "00-00000000000000000000000000000000-0123456789abcdef-01", "00-0123456789ABCDEF0123456789abcdef-0123456789abcdef-01", "01-0123456789abcdef0123456789abcdef-0123456789abcdef-01"} {
		if _, ok := otelx.ParseTraceparent(invalid); ok {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestTrafficClassAttributesRejectsPropertiesAndUnknownValues(t *testing.T) {
	for _, test := range []struct {
		member string
		want   bool
	}{
		{
			member: "traffic.class=interactive",
			want:   true,
		},
		{
			member: "traffic.class=batch",
			want:   true,
		},
		{
			member: "traffic.class=private",
			want:   false,
		},
		{
			member: "traffic.class=batch;source=private",
			want:   false,
		},
	} {
		t.Run(test.member, func(t *testing.T) {
			bag, err := baggage.Parse(test.member)
			if err != nil {
				t.Fatal(err)
			}
			attrs := otelx.TrafficClassAttributes(baggage.ContextWithBaggage(context.Background(), bag))
			if (len(attrs) == 1) != test.want {
				t.Fatalf("attributes: %v", attrs)
			}
		})
	}
}

func TestCanonicalParentSurvivesPeerFormatChange(t *testing.T) {
	const stored = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-00"
	parent, ok := otelx.ParseTraceparent(stored)
	if !ok {
		t.Fatal("unsampled parent rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
	for _, format := range []otelx.PropagationFormat{otelx.FormatW3C, otelx.FormatB3Single, otelx.FormatB3Multi} {
		policy, err := otelx.NewPropagation(otelx.PropagationPolicy{
			Format: format,
		})
		if err != nil {
			t.Fatal(err)
		}
		carrier := propagation.MapCarrier{}
		policy.Inject(ctx, carrier)
		restored := policy.Extract(ctx, carrier)
		if restored.Err() != context.Canceled {
			t.Fatal("cancellation lost")
		}
		if got := otelx.Traceparent(restored); got != stored {
			t.Fatalf("format %s: got %q", format, got)
		}
	}
}
