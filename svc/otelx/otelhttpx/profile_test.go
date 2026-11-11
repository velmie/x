package otelhttpx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/otelx/otelhttpx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestBoundedTransportEndsAtHeadersAndPreservesCaller(t *testing.T) {
	spans, tracer, reader, meter := profileSignals(t)
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	parent, parentSpan := tracer.Start(context.Background(), "parent")
	defer parentSpan.End()
	state, err := trace.ParseTraceState("vendor=private")
	if err != nil {
		t.Fatal(err)
	}
	parent = trace.ContextWithSpanContext(parent, trace.SpanContextFromContext(parent).WithTraceState(state))
	member, _ := baggage.NewMember("private", "secret")
	bag, _ := baggage.New(member)
	parent = baggage.ContextWithBaggage(parent, bag)
	request, err := http.NewRequestWithContext(parent, http.MethodGet, "https://example.test/private?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = http.Header{
		"traceparent":   []string{"stale"},
		"Tracestate":    []string{"vendor=private"},
		"Authorization": []string{"Bearer secret"},
	}
	body := io.NopCloser(strings.NewReader("body"))
	transport, err := otelhttpx.NewTransport(
		roundTripFunc(
			func(out *http.Request) (*http.Response, error) {
				if out == request || out.Header.Get("Authorization") != "Bearer secret" || out.Header.Get("Tracestate") != "" ||
					out.Header.Get("Traceparent") == "" || baggage.FromContext(out.Context()).Len() != 0 {
					t.Error("outgoing request was not cloned and filtered")
				}
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       body,
				}, nil
			},
		),
		otelhttpx.TransportConfig{
			Operation:  "peer.fetch",
			Tracer:     tracer,
			Meter:      meter,
			Propagator: policy,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil || response.Body != body {
		t.Fatalf("body or error changed: %v", err)
	}
	if request.Header[strings.ToLower("Traceparent")][0] != "stale" || request.Header.Get("Tracestate") != "vendor=private" {
		t.Fatal("caller headers were mutated")
	}
	ended := spans.Ended()
	if len(ended) != 1 || ended[0].Name() != "peer.fetch" || ended[0].Parent().SpanID() != parentSpan.SpanContext().SpanID() ||
		ended[0].Parent().IsRemote() || ended[0].Parent().TraceState().Len() != 0 {
		t.Fatalf("client span did not end at headers with sanitized local parent: %v", ended)
	}
	attrs := attribute.NewSet(ended[0].Attributes()...)
	if status, _ := attrs.Value("http.response.status_code"); status.AsInt64() != 201 {
		t.Fatalf("wrong status: %v", attrs)
	}
	for _, attr := range ended[0].Attributes() {
		if strings.Contains(attr.Value.String(), "secret") || strings.Contains(attr.Value.String(), "private") {
			t.Fatalf("sensitive span attribute: %v", attr)
		}
	}
	assertProfileMetrics(t, reader, "http.client", 201, "")
}

func TestBoundedServerRecordsAbortAndPreservesPanic(t *testing.T) {
	spans, tracer, reader, meter := profileSignals(t)
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := otelhttpx.NewHandler(
		http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) {
				panic(http.ErrAbortHandler)
			},
		),
		otelhttpx.HandlerConfig{
			Operation:  "books.get",
			Route:      "/books/{id}",
			Tracer:     tracer,
			Meter:      meter,
			Propagator: policy,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("CUSTOM", "/secret?token=secret", nil))
	}()
	if recovered != http.ErrAbortHandler {
		t.Fatalf("changed abort: %v", recovered)
	}
	ended := spans.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error || ended[0].Status().Description != "handler_aborted" {
		t.Fatalf("wrong server span: %v", ended)
	}
	attrs := attribute.NewSet(ended[0].Attributes()...)
	if method, _ := attrs.Value("http.request.method"); method.AsString() != "OTHER" {
		t.Fatalf("unbounded method: %v", attrs)
	}
	assertProfileMetrics(t, reader, "http.server", 0, "handler_aborted")
}

func TestBoundedInstrumentationValidatesAndPreservesMeterCauses(t *testing.T) {
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	_, tracer, _, meter := profileSignals(t)
	hcfg := otelhttpx.HandlerConfig{
		Operation:  "test",
		Route:      "/test",
		Tracer:     tracer,
		Meter:      meter,
		Propagator: policy,
	}
	tcfg := otelhttpx.TransportConfig{
		Operation:  "test",
		Tracer:     tracer,
		Meter:      meter,
		Propagator: policy,
	}
	if _, err := otelhttpx.NewHandler(nil, hcfg); !errors.Is(err, otelx.ErrInvalidConfig) {
		t.Fatalf("missing handler: %v", err)
	}
	if _, err := otelhttpx.NewTransport(nil, tcfg); !errors.Is(err, otelx.ErrInvalidConfig) {
		t.Fatalf("missing transport: %v", err)
	}
	cause := errors.New("instrument failure")
	for _, position := range []int{1, 2} {
		failing := &failingMeter{
			Meter: metricnoop.NewMeterProvider().Meter("test"),
			fail:  position,
			cause: cause,
		}
		hcfg.Meter = failing
		if _, err := otelhttpx.NewHandler(http.NotFoundHandler(), hcfg); !errors.Is(err, cause) {
			t.Fatalf("server instrument lost cause: %v", err)
		}
		failing.calls = 0
		tcfg.Meter = failing
		if _, err := otelhttpx.NewTransport(http.DefaultTransport, tcfg); !errors.Is(err, cause) {
			t.Fatalf("client instrument lost cause: %v", err)
		}
	}
}

func profileSignals(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer, *sdkmetric.ManualReader, metric.Meter) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(
		func() {
			if err := errors.Join(traces.Shutdown(context.Background()), meters.Shutdown(context.Background())); err != nil {
				t.Error(err)
			}
		},
	)
	return recorder, traces.Tracer("test"), reader, meters.Meter("test")
}

func assertProfileMetrics(t *testing.T, reader *sdkmetric.ManualReader, prefix string, status int64, failure string) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			var attrs attribute.Set
			switch points := measurement.Data.(type) {
			case metricdata.Sum[int64]:
				if measurement.Name != prefix+".requests" || measurement.Unit != "{request}" ||
					len(points.DataPoints) != 1 || points.DataPoints[0].Value != 1 {
					t.Fatalf("unexpected counter: %v", measurement)
				}
				attrs = points.DataPoints[0].Attributes
			case metricdata.Histogram[float64]:
				if measurement.Name != prefix+".request.duration" || measurement.Unit != "s" ||
					len(points.DataPoints) != 1 || points.DataPoints[0].Count != 1 {
					t.Fatalf("unexpected histogram: %v", measurement)
				}
				attrs = points.DataPoints[0].Attributes
			default:
				continue
			}
			if actual, _ := attrs.Value("http.response.status_code"); actual.AsInt64() != status {
				t.Fatalf("unexpected status: %v", attrs)
			}
			if actual, _ := attrs.Value("error.type"); actual.AsString() != failure {
				t.Fatalf("unexpected failure: %v", attrs)
			}
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("expected counter and histogram, got %d", seen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingMeter struct {
	metric.Meter
	calls int
	fail  int
	cause error
}

func (m *failingMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	m.calls++
	if m.calls == m.fail {
		return nil, m.cause
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m *failingMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	m.calls++
	if m.calls == m.fail {
		return nil, m.cause
	}
	return m.Meter.Float64Histogram(name, options...)
}

func TestBoundedTransportPreservesFailureIdentity(t *testing.T) {
	spans, tracer, reader, meter := profileSignals(t)
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("sensitive transport detail")
	transport, err := otelhttpx.NewTransport(
		roundTripFunc(
			func(r *http.Request) (*http.Response, error) {
				if r.Header == nil {
					t.Fatal("nil outgoing headers")
				}
				return nil, cause
			},
		),
		otelhttpx.TransportConfig{
			Operation:  "peer.fetch",
			Tracer:     tracer,
			Meter:      meter,
			Propagator: policy,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	request.Header = nil
	_, got := transport.RoundTrip(request)
	if got != cause || request.Header != nil {
		t.Fatalf("error identity or caller headers changed: %v", got)
	}
	ended := spans.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error || ended[0].Status().Description != "transport_failed" {
		t.Fatalf("incorrect transport failure: %v", ended)
	}
	assertProfileMetrics(t, reader, "http.client", 0, "transport_failed")
}

func TestBoundedServerOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   string
		action func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "implicit success",
			status: 200,
			action: func(http.ResponseWriter, *http.Request) {},
		},
		{
			name:   "client error",
			status: 400,
			action: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) },
		},
		{
			name:   "server error",
			status: 503,
			want:   "server_error",
			action: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		},
		{
			name:   "informational then final",
			status: 204,
			action: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:   "implicit flush",
			status: 200,
			action: func(w http.ResponseWriter, _ *http.Request) {
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error(err)
				}
			},
		},
		{
			name:   "recovered panic",
			status: 500,
			want:   "server_error",
			action: func(w http.ResponseWriter, _ *http.Request) {
				defer func() {
					if recover() != nil {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				panic("private failure")
			},
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				spans, tracer, reader, meter := profileSignals(t)
				policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
				if err != nil {
					t.Fatal(err)
				}
				handler, err := otelhttpx.NewHandler(
					http.HandlerFunc(tc.action),
					otelhttpx.HandlerConfig{
						Operation:  "books.get",
						Route:      "/books/{id}",
						Tracer:     tracer,
						Meter:      meter,
						Propagator: policy,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/books/secret", nil))
				ended := spans.Ended()
				if len(ended) != 1 || ended[0].Status().Description != tc.want ||
					(tc.want == "") != (ended[0].Status().Code != codes.Error) {
					t.Fatalf("wrong server span: %v", ended)
				}
				assertProfileMetrics(t, reader, "http.server", int64(tc.status), "")
			},
		)
	}
}

func TestBoundedServerRealHTTPAbort(t *testing.T) {
	spans, tracer, reader, meter := profileSignals(t)
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := otelhttpx.NewHandler(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }),
		otelhttpx.HandlerConfig{
			Operation:  "books.get",
			Route:      "/books/{id}",
			Tracer:     tracer,
			Meter:      meter,
			Propagator: policy,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				handler.ServeHTTP(w, r)
			},
		),
	)
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err == nil {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("client did not observe aborted response")
	}
	<-done
	if ended := spans.Ended(); len(ended) != 1 || ended[0].Status().Description != "handler_aborted" {
		t.Fatalf("abort span missing: %v", ended)
	}
	assertProfileMetrics(t, reader, "http.server", 0, "handler_aborted")
}

func TestBoundedInstrumentationRejectsEachMissingField(t *testing.T) {
	_, tracer, _, meter := profileSignals(t)
	policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		field  string
		mutate func(*otelhttpx.HandlerConfig, *otelhttpx.TransportConfig)
	}{
		{
			field: "Operation",
			mutate: func(h *otelhttpx.HandlerConfig, c *otelhttpx.TransportConfig) {
				h.Operation = " "
				c.Operation = " "
			},
		},
		{
			field: "Tracer",
			mutate: func(h *otelhttpx.HandlerConfig, c *otelhttpx.TransportConfig) {
				h.Tracer = nil
				c.Tracer = nil
			},
		},
		{
			field: "Meter",
			mutate: func(h *otelhttpx.HandlerConfig, c *otelhttpx.TransportConfig) {
				h.Meter = nil
				c.Meter = nil
			},
		},
		{
			field: "Propagator",
			mutate: func(h *otelhttpx.HandlerConfig, c *otelhttpx.TransportConfig) {
				h.Propagator = nil
				c.Propagator = nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(
			tc.field,
			func(t *testing.T) {
				hcfg := otelhttpx.HandlerConfig{
					Operation:  "test",
					Route:      "/test",
					Tracer:     tracer,
					Meter:      meter,
					Propagator: policy,
				}
				tcfg := otelhttpx.TransportConfig{
					Operation:  "test",
					Tracer:     tracer,
					Meter:      meter,
					Propagator: policy,
				}
				tc.mutate(&hcfg, &tcfg)
				_, hErr := otelhttpx.NewHandler(http.NotFoundHandler(), hcfg)
				_, tErr := otelhttpx.NewTransport(http.DefaultTransport, tcfg)
				if !errors.Is(hErr, otelx.ErrInvalidConfig) || !strings.Contains(hErr.Error(), tc.field) ||
					!errors.Is(tErr, otelx.ErrInvalidConfig) || !strings.Contains(tErr.Error(), tc.field) {
					t.Fatalf("missing field %s: handler=%v transport=%v", tc.field, hErr, tErr)
				}
			},
		)
	}
	_, err = otelhttpx.NewHandler(
		http.NotFoundHandler(),
		otelhttpx.HandlerConfig{
			Operation:  "test",
			Tracer:     tracer,
			Meter:      meter,
			Propagator: policy,
		},
	)
	if !errors.Is(err, otelx.ErrInvalidConfig) || !strings.Contains(err.Error(), "Route") {
		t.Fatalf("missing route: %v", err)
	}
}

func TestBoundedTransportRemoteErrorStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusServiceUnavailable} {
		t.Run(
			http.StatusText(status),
			func(t *testing.T) {
				spans, tracer, reader, meter := profileSignals(t)
				policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
				if err != nil {
					t.Fatal(err)
				}
				transport, err := otelhttpx.NewTransport(
					roundTripFunc(
						func(*http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: status,
							}, nil
						},
					),
					otelhttpx.TransportConfig{
						Operation:  "peer.fetch",
						Tracer:     tracer,
						Meter:      meter,
						Propagator: policy,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				_, err = transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.test/", nil))
				if err != nil {
					t.Fatal(err)
				}
				if ended := spans.Ended(); len(ended) != 1 || ended[0].Status().Code != codes.Error ||
					ended[0].Status().Description != "remote_error" {
					t.Fatalf("wrong remote error span: %v", ended)
				}
				assertProfileMetrics(t, reader, "http.client", int64(status), "")
			},
		)
	}
}
