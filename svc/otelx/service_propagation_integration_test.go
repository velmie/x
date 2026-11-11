package otelx_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"go.opentelemetry.io/otel/propagation"

	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestSelectedFormatHTTPBoundary(t *testing.T) {
	for _, format := range []string{"w3c", "b3-single", "b3-multi"} {
		t.Run(format, func(t *testing.T) {
			policy := profilePropagation(t, otelx.PropagationFormat(format))
			provider := sdktrace.NewTracerProvider()
			defer func() { _ = provider.Shutdown(context.Background()) }()
			ctx, parent := provider.Tracer("test").Start(context.Background(), "publish")
			defer parent.End()
			allowed, _ := baggage.NewMember("traffic.class", "interactive")
			secret, _ := baggage.NewMember("credentials", "private-token")
			bag, _ := baggage.New(allowed, secret)
			ctx = baggage.ContextWithBaggage(ctx, bag)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				extracted := policy.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
				sc := trace.SpanContextFromContext(extracted)
				if !sc.IsRemote() || sc.TraceID() != parent.SpanContext().TraceID() || sc.SpanID() != parent.SpanContext().SpanID() {
					t.Error("selected parent was not propagated")
				}
				if baggage.FromContext(extracted).Member("credentials").Value() != "" ||
					baggage.FromContext(extracted).Member("traffic.class").Value() != "interactive" {
					t.Error("baggage filtering failed")
				}
				child, span := provider.Tracer("test").Start(extracted, "handle")
				defer span.End()
				// The same boundary is used when adapting a broker message header map.
				outgoing := make(http.Header)
				policy.Inject(child, propagation.HeaderCarrier(outgoing))
				next := policy.Extract(context.Background(), propagation.HeaderCarrier(outgoing))
				if trace.SpanContextFromContext(next).SpanID() != span.SpanContext().SpanID() {
					t.Error("outbound child correlation failed")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			request.Header.Set("Tracestate", "vendor=secret")
			policy.Inject(ctx, propagation.HeaderCarrier(request.Header))
			if strings.Contains(request.Header.Get("Baggage"), "private-token") || request.Header.Get("Tracestate") != "" {
				t.Fatal("sensitive propagation leaked")
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func(close func() error) { _ = close() }(response.Body.Close)
			_, _ = io.Copy(io.Discard, response.Body)
			if response.StatusCode != http.StatusNoContent {
				t.Fatal(response.StatusCode)
			}
		})
	}
}

func TestPropagationDiscardsAmbiguousBaggage(t *testing.T) {
	policy := profilePropagation(t, otelx.FormatW3C)
	headers := http.Header{
		"Baggage": {"traffic.class=batch,unused=one,unused=two"},
	}
	ctx := policy.Extract(t.Context(), propagation.HeaderCarrier(headers))
	if got := baggage.FromContext(ctx).String(); got != "" {
		t.Fatalf("ambiguous baggage was retained: %q", got)
	}
}

func profilePropagation(t *testing.T, format otelx.PropagationFormat) propagation.TextMapPropagator {
	t.Helper()
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    filepath.Join(t.TempDir(), "traces"),
		MetricsMode:   "prometheus",
		Environment:   "test",
		AllowBaggage:  true,
		Propagation:   format,
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	runtime, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return runtime.Propagator
}
