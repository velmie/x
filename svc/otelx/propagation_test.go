package otelx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const testTraceparent = "00-12345678901234567890123456789012-1234567890123456-01"

func TestPropagationValidationAndPolicyCopy(t *testing.T) {
	rule := func(key string, values ...string) BaggageRule {
		return BaggageRule{
			Key:    key,
			Values: values,
		}
	}
	invalid := []PropagationPolicy{
		{
			Format: "unknown",
		},
		{
			MaxTraceHeaderBytes: -1,
		},
		{
			MaxBaggageHeaderBytes: -1,
		},
		{
			MaxBaggageMembers: -1,
		},
		{
			Baggage: []BaggageRule{rule("bad key", "ok")},
		},
		{
			Baggage: []BaggageRule{rule("class", "")},
		},
		{
			Baggage: []BaggageRule{rule("class")},
		},
		{
			Baggage: []BaggageRule{rule("class", "a"), rule("class", "b")},
		},
		{
			MaxBaggageHeaderBytes: 4,
			Baggage:               []BaggageRule{rule("class", "value")},
		},
		{
			MaxBaggageMembers: 1,
			Baggage:           []BaggageRule{rule("a", "a"), rule("b", "b")},
		},
	}
	for i, cfg := range invalid {
		if _, err := NewPropagation(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: expected invalid configuration, got %v", i, err)
		}
	}
	values := []string{"interactive", "batch"}
	cfg := PropagationPolicy{
		Baggage: []BaggageRule{
			{
				Key:    "traffic.class",
				Values: values,
			},
		},
	}
	p, err := NewPropagation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = "changed"
	cfg.Baggage[0].Key = "changed"
	ctx := p.Extract(
		context.Background(),
		propagation.HeaderCarrier(
			http.Header{
				"Baggage": []string{"traffic.class=interactive"},
			},
		),
	)
	if baggage.FromContext(ctx).Member("traffic.class").Value() != "interactive" {
		t.Fatal("policy changed with caller-owned input")
	}
}

func TestPropagationW3CRejectsAmbiguityAndPreservesContext(t *testing.T) {
	p, err := NewPropagation(PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		headers http.Header
		valid   bool
	}{
		{
			name:    "valid",
			headers: http.Header{"Traceparent": []string{testTraceparent}},
			valid:   true,
		},
		{
			name:    "duplicate values",
			headers: http.Header{"Traceparent": []string{testTraceparent, testTraceparent}},
		},
		{
			name: "case aliases",
			headers: http.Header{
				"Traceparent": []string{testTraceparent},
				"traceparent": []string{testTraceparent},
			},
		},
		{
			name: "foreign",
			headers: http.Header{
				"Traceparent": []string{testTraceparent},
				"B3":          []string{"1"},
			},
		},
		{
			name:    "uppercase hex",
			headers: http.Header{"Traceparent": []string{strings.Replace(testTraceparent, "123", "ABC", 1)}},
		},
		{
			name:    "future version",
			headers: http.Header{"Traceparent": []string{"01" + testTraceparent[2:]}},
		},
		{
			name:    "oversized",
			headers: http.Header{"Traceparent": []string{strings.Repeat("x", 513)}},
		},
	}
	parent := context.WithValue(context.Background(), contextKey{}, "ordinary")
	parent, cancel := context.WithCancel(parent)
	cancel()
	member, _ := baggage.NewMember("old", "value")
	oldBag, _ := baggage.New(member)
	parent = baggage.ContextWithBaggage(parent, oldBag)
	parent = trace.ContextWithSpanContext(parent, testSpanContext(false))
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				got := p.Extract(parent, propagation.HeaderCarrier(tc.headers))
				sc := trace.SpanContextFromContext(got)
				if sc.IsValid() != tc.valid || sc.IsValid() && !sc.IsRemote() {
					t.Fatalf("unexpected extracted parent: %v", sc)
				}
				if got.Err() != context.Canceled || got.Value(contextKey{}) != "ordinary" || baggage.FromContext(got).Len() != 0 {
					t.Fatal("cancellation or values lost, or inherited baggage retained")
				}
			},
		)
	}
}

func TestPropagationBaggageIsIndependentAndBounded(t *testing.T) {
	p, err := NewPropagation(
		PropagationPolicy{
			Baggage: []BaggageRule{
				{
					Key:    "traffic.class",
					Values: []string{"interactive", "batch"},
				},
			},
			MaxBaggageHeaderBytes: 80,
			MaxBaggageMembers:     2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "allowed with unknown",
			values: []string{"traffic.class=interactive,private=secret"},
			want:   "interactive",
		},
		{
			name:   "duplicate member",
			values: []string{"traffic.class=interactive,traffic.class=batch"},
		},
		{
			name:   "properties",
			values: []string{"traffic.class=interactive;secret=1"},
		},
		{
			name:   "duplicate values",
			values: []string{"traffic.class=interactive", "traffic.class=batch"},
		},
		{
			name:   "too many",
			values: []string{"traffic.class=interactive,a=b,c=d"},
		},
		{
			name:   "too long",
			values: []string{"traffic.class=interactive,private=" + strings.Repeat("a", 81)},
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				ctx := p.Extract(
					context.Background(),
					propagation.HeaderCarrier(
						http.Header{
							"Traceparent": []string{"invalid"},
							"Baggage":     tc.values,
						},
					),
				)
				if trace.SpanContextFromContext(ctx).IsValid() {
					t.Fatal("invalid parent accepted")
				}
				bag := baggage.FromContext(ctx)
				if got := bag.Member("traffic.class").Value(); got != tc.want || bag.Member("private").Value() != "" {
					t.Fatalf("unexpected baggage: %v", bag)
				}
			},
		)
	}
}

func TestPropagationB3NormalizesDebugAndPreservesLocalParent(t *testing.T) {
	for _, format := range []PropagationFormat{FormatB3Single, FormatB3Multi} {
		t.Run(
			string(format),
			func(t *testing.T) {
				p, err := NewPropagation(
					PropagationPolicy{
						Format: format,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				input := propagation.MapCarrier{}
				if format == FormatB3Single {
					input["b3"] = "12345678901234567890123456789012-1234567890123456-d"
				} else {
					input["x-b3-traceid"] = "12345678901234567890123456789012"
					input["x-b3-spanid"] = "1234567890123456"
					input["x-b3-flags"] = "1"
				}
				remote := trace.SpanContextFromContext(p.Extract(context.Background(), input))
				if !remote.IsValid() || !remote.IsRemote() || !remote.IsSampled() {
					t.Fatalf("debug parent not normalized: %v", remote)
				}
				local := testSpanContext(false)
				filtered := p.FilterContext(trace.ContextWithSpanContext(context.Background(), local))
				if sc := trace.SpanContextFromContext(filtered); sc.IsRemote() || sc.TraceID() != local.TraceID() || sc.SpanID() != local.SpanID() {
					t.Fatalf("changed local parent identity: %v", sc)
				}
				out := propagation.MapCarrier{}
				p.Inject(trace.ContextWithSpanContext(context.Background(), remote), out)
				if out["x-b3-flags"] != "" || strings.HasSuffix(out["b3"], "-d") {
					t.Fatalf("debug flag escaped: %v", out)
				}
			},
		)
	}
}

func TestPropagationInjectClearsStaleFieldsAndEnforcesBounds(t *testing.T) {
	p, err := NewPropagation(
		PropagationPolicy{
			MaxTraceHeaderBytes: 20,
			Baggage: []BaggageRule{
				{
					Key:    "traffic.class",
					Values: []string{"interactive"},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	member, _ := baggage.NewMember("traffic.class", "interactive")
	bag, _ := baggage.New(member)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)
	ctx = trace.ContextWithSpanContext(ctx, testSpanContext(false))
	headers := propagation.HeaderCarrier(
		http.Header{
			"traceparent": []string{testTraceparent},
			"Traceparent": []string{testTraceparent},
			"B3":          []string{"old"},
			"Tracestate":  []string{"vendor=private"},
			"Baggage":     []string{"secret=value"},
		},
	)
	p.Inject(ctx, headers)
	if len(headers.Values("Traceparent")) != 0 || headers.Get("B3") != "" || headers.Get("Tracestate") != "" {
		t.Fatalf("oversized or stale trace fields retained: %v", headers)
	}
	if got := headers.Get("Baggage"); got != "traffic.class=interactive" {
		t.Fatalf("missing permitted independent baggage: %q", got)
	}
}

type contextKey struct{}

func testSpanContext(remote bool) trace.SpanContext {
	return trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID:    trace.TraceID{1, 2, 3, 4},
			SpanID:     trace.SpanID{5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
			Remote:     remote,
		},
	)
}

func TestPropagationValuesGetterAndIndependentBaggageFailure(t *testing.T) {
	p, err := NewPropagation(
		PropagationPolicy{
			Baggage: []BaggageRule{
				{
					Key: "traffic.class",
					Values: []string{
						"interactive",
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	duplicateTrace := multiValueCarrier{
		"traceparent": []string{testTraceparent, testTraceparent},
		"baggage":     []string{"traffic.class=interactive"},
	}
	ctx := p.Extract(context.Background(), duplicateTrace)
	if trace.SpanContextFromContext(ctx).IsValid() || baggage.FromContext(ctx).Member("traffic.class").Value() != "interactive" {
		t.Fatal("multi-value trace must be rejected independently of baggage")
	}
	duplicateBag := multiValueCarrier{
		"traceparent": []string{testTraceparent},
		"baggage":     []string{"traffic.class=interactive", "traffic.class=interactive"},
	}
	ctx = p.Extract(context.Background(), duplicateBag)
	if !trace.SpanContextFromContext(ctx).IsValid() || baggage.FromContext(ctx).Len() != 0 {
		t.Fatal("multi-value baggage must be rejected independently of trace")
	}
}

func TestPropagationFilterContextRemovesOnlyUnapprovedMetadata(t *testing.T) {
	p, err := NewPropagation(
		PropagationPolicy{
			Baggage: []BaggageRule{
				{
					Key: "traffic.class",
					Values: []string{
						"interactive",
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := trace.ParseTraceState("vendor=private")
	if err != nil {
		t.Fatal(err)
	}
	allowed, _ := baggage.NewMember("traffic.class", "interactive")
	private, _ := baggage.NewMember("private", "secret")
	bag, _ := baggage.New(allowed, private)
	ctx := context.WithValue(context.Background(), contextKey{}, "ordinary")
	ctx = baggage.ContextWithBaggage(ctx, bag)
	parent := testSpanContext(false).WithTraceState(state)
	ctx = trace.ContextWithSpanContext(ctx, parent)
	filtered := p.FilterContext(ctx)
	sc := trace.SpanContextFromContext(filtered)
	if sc.IsRemote() || sc.TraceID() != parent.TraceID() || sc.SpanID() != parent.SpanID() || sc.TraceState().String() != "" {
		t.Fatalf("unexpected parent: %v", sc)
	}
	result := baggage.FromContext(filtered)
	if result.Member("traffic.class").Value() != "interactive" || result.Member("private").Value() != "" ||
		filtered.Value(contextKey{}) != "ordinary" {
		t.Fatal("filtered context did not retain exactly the permitted metadata")
	}
}

func TestPropagationRemovesMapAliasesAndNormalizesW3CFlags(t *testing.T) {
	p, err := NewPropagation(PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := p.Extract(
		context.Background(),
		propagation.MapCarrier{
			"traceparent": testTraceparent[:53] + "ff",
		},
	)
	if !trace.SpanContextFromContext(ctx).IsSampled() {
		t.Fatal("W3C flags were not normalized to the sampling bit")
	}
	out := propagation.MapCarrier{
		"TRACEPARENT": "stale",
		"b3":          "stale",
		"Tracestate":  "private=value",
		"Baggage":     "private=value",
	}
	p.Inject(ctx, out)
	if len(out) != 1 || out["traceparent"] != testTraceparent {
		t.Fatalf("unexpected output: %v", out)
	}
	fields := p.Fields()
	fields[0] = "changed"
	if p.Fields()[0] != "traceparent" {
		t.Fatal("caller changed policy fields")
	}
}

type multiValueCarrier map[string][]string

func (c multiValueCarrier) Get(key string) string {
	if values := c[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func (c multiValueCarrier) Set(key, value string) {
	c[key] = []string{value}
}

func (c multiValueCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

func (c multiValueCarrier) Values(key string) []string {
	return c[key]
}
