package otelx

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeExportsOTLPHTTPWithTLSHeadersAndResource(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "environment.source=environment,service.name=ambient")
	type request struct {
		path string
		auth string
		body []byte
	}
	requests := make(chan request, 8)
	collector := httptest.NewTLSServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
				}
				requests <- request{
					path: r.URL.Path,
					auth: r.Header.Get("Authorization"),
					body: body,
				}
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			},
		),
	)
	defer collector.Close()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.NewWithAttributes("", attribute.String("service.name", "explicit")),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracehttp.New(
					ctx,
					otlptracehttp.WithEndpointURL(collector.URL+"/v1/traces"),
					otlptracehttp.WithHTTPClient(collector.Client()),
					otlptracehttp.WithHeaders(map[string]string{"Authorization": "Bearer test-token"}),
				)
			},
			NewMetricExporter: func(ctx context.Context) (sdkmetric.Exporter, error) {
				return otlpmetrichttp.New(
					ctx,
					otlpmetrichttp.WithEndpointURL(collector.URL+"/v1/metrics"),
					otlpmetrichttp.WithHTTPClient(collector.Client()),
					otlpmetrichttp.WithHeaders(map[string]string{"Authorization": "Bearer test-token"}),
				)
			},
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithBatchTimeout(time.Hour),
			},
			ReaderOptions: []sdkmetric.PeriodicReaderOption{
				sdkmetric.WithInterval(time.Hour),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("runtime-test").Start(context.Background(), "operation")
	span.End()
	counter, err := r.MeterProvider.Meter("runtime-test").Int64Counter("runtime.test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(requests)
	var sawTrace, sawMetric bool
	for req := range requests {
		if req.auth != "Bearer test-token" {
			t.Errorf("missing authorization for %s", req.path)
		}
		switch req.path {
		case "/v1/traces":
			var message collectortrace.ExportTraceServiceRequest
			if err := proto.Unmarshal(req.body, &message); err != nil {
				t.Fatal(err)
			}
			for _, resourceSpans := range message.ResourceSpans {
				attributes := make(map[string]string)
				for _, pair := range resourceSpans.Resource.Attributes {
					attributes[pair.Key] = pair.Value.GetStringValue()
				}
				if attributes["service.name"] != "explicit" || attributes["environment.source"] != "environment" {
					t.Errorf("resource merge: %v", attributes)
				}
				for _, scope := range resourceSpans.ScopeSpans {
					for _, span := range scope.Spans {
						sawTrace = sawTrace || span.Name == "operation"
					}
				}
			}
		case "/v1/metrics":
			var message collectormetric.ExportMetricsServiceRequest
			if err := proto.Unmarshal(req.body, &message); err != nil {
				t.Fatal(err)
			}
			for _, resourceMetrics := range message.ResourceMetrics {
				attributes := make(map[string]string)
				for _, pair := range resourceMetrics.Resource.Attributes {
					attributes[pair.Key] = pair.Value.GetStringValue()
				}
				if attributes["service.name"] != "explicit" || attributes["environment.source"] != "environment" {
					t.Errorf("metric resource merge: %v", attributes)
				}
				for _, scope := range resourceMetrics.ScopeMetrics {
					for _, metric := range scope.Metrics {
						sawMetric = sawMetric || metric.Name == "runtime.test.requests"
					}
				}
			}
		}
	}
	if !sawTrace || !sawMetric {
		t.Fatalf("missing exported signals: trace=%t metric=%t", sawTrace, sawMetric)
	}
}

func TestRuntimeExportsOTLPGRPC(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	collector := &traceCollector{
		requests: make(chan *collectortrace.ExportTraceServiceRequest, 2),
		auth:     make(chan string, 2),
	}
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := grpc.NewClient(
		"passthrough:///in-process-collector",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracegrpc.New(
					ctx,
					otlptracegrpc.WithGRPCConn(conn),
					otlptracegrpc.WithHeaders(map[string]string{"authorization": "Bearer grpc-token"}),
				)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("runtime-test").Start(context.Background(), "grpc-operation")
	span.End()
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-collector.requests:
		if len(got.ResourceSpans) == 0 || len(got.ResourceSpans[0].ScopeSpans) == 0 ||
			got.ResourceSpans[0].ScopeSpans[0].Spans[0].Name != "grpc-operation" {
			t.Fatal("missing gRPC span")
		}
		if auth := <-collector.auth; auth != "Bearer grpc-token" {
			t.Fatalf("missing gRPC authorization: %q", auth)
		}
	default:
		t.Fatal("collector received no gRPC request")
	}
}

func TestRuntimePrometheusAndExplicitStdout(t *testing.T) {
	var output bytes.Buffer
	registry := prometheus.NewRegistry()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:            true,
			Resource:           resource.Empty(),
			PrometheusRegistry: registry,
			NewSpanExporter:    stdoutFactory(&output),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := r.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 2)
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "stdout-operation")
	span.End()
	response := httptest.NewRecorder()
	r.PrometheusHandler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "test_requests_total{") ||
		!strings.Contains(response.Body.String(), "} 2") {
		t.Fatalf("unexpected scrape: %d %s", response.Code, response.Body.String())
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "stdout-operation") {
		t.Fatal("stdout exporter did not use supplied writer")
	}
}

type traceCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	requests chan *collectortrace.ExportTraceServiceRequest
	auth     chan string
}

func (c *traceCollector) Export(
	ctx context.Context,
	request *collectortrace.ExportTraceServiceRequest,
) (*collectortrace.ExportTraceServiceResponse, error) {
	c.requests <- request
	md, _ := metadata.FromIncomingContext(ctx)
	auth := md.Get("authorization")
	if len(auth) > 0 {
		c.auth <- auth[0]
	} else {
		c.auth <- ""
	}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func stdoutFactory(writer io.Writer) func(context.Context) (sdktrace.SpanExporter, error) {
	return func(context.Context) (sdktrace.SpanExporter, error) {
		return stdouttrace.New(stdouttrace.WithWriter(writer))
	}
}
