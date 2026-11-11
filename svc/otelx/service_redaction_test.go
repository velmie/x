package otelx_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestServiceCollectorResponseDoesNotExposeCredentials(t *testing.T) {
	const credential = "Bearer example-secret-credential"
	handler, logs := installServiceErrorHandler(t)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "collector rejected "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer collector.Close()
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "http",
		Endpoint:       strings.TrimPrefix(collector.URL, "http://"),
		LocalPlaintext: true,
		Headers:        map[string]string{"authorization": credential},
		MetricsMode:    "prometheus",
		SampleRatio:    1,
		Environment:    "test",
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	rt, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	_, span := rt.TracerProvider.Tracer("test").Start(t.Context(), "test")
	span.End()
	err = rt.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal("expected diagnostic HTTP status")
	}
	if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "collector rejected") {
		t.Fatal("returned error exposes collector response text")
	}
	verifyServiceDiagnostic(t, handler, logs, err, credential, "401")
}

func TestServiceGRPCCollectorResponseDoesNotExposeCredentials(t *testing.T) {
	const credential = "Bearer grpc-example-secret"
	handler, logs := installServiceErrorHandler(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, &echoGRPCCollector{})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "grpc",
		Endpoint:       listener.Addr().String(),
		LocalPlaintext: true,
		Headers:        map[string]string{"authorization": credential},
		MetricsMode:    "prometheus",
		SampleRatio:    1,
		Environment:    "test",
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	rt, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	_, span := rt.TracerProvider.Tracer("test").Start(t.Context(), "test")
	span.End()
	err = rt.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Unauthenticated") {
		t.Fatal("gRPC authentication status was lost")
	}
	if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "collector rejected") {
		t.Fatal("returned error exposes collector status text")
	}
	verifyServiceDiagnostic(t, handler, logs, err, credential, "Unauthenticated")
}

type echoGRPCCollector struct {
	collectortrace.UnimplementedTraceServiceServer
}

func (*echoGRPCCollector) Export(ctx context.Context, _ *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	return nil, status.Error(codes.Unauthenticated, "collector rejected "+strings.Join(md.Get("authorization"), ""))
}

func TestServiceMetricCollectorResponseDoesNotExposeCredentials(t *testing.T) {
	const credential = "Bearer metric-example-secret"
	handler, logs := installServiceErrorHandler(t)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "collector rejected "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer collector.Close()
	cfg := otelx.ServiceConfig{
		Enabled:         true,
		TraceProtocol:   "stdout",
		StdoutFile:      filepath.Join(t.TempDir(), "traces"),
		MetricsMode:     "otlp",
		MetricsEndpoint: strings.TrimPrefix(collector.URL, "http://"),
		LocalPlaintext:  true,
		Headers:         map[string]string{"authorization": credential},
		Environment:     "test",
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	rt, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := rt.MeterProvider.Meter("test").Int64Counter("test.requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 1)
	err = rt.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal("metric collector status was lost")
	}
	if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "collector rejected") {
		t.Fatal("metric error exposes collector response text")
	}
	verifyServiceDiagnostic(t, handler, logs, err, credential, "401")
}

func installServiceErrorHandler(t *testing.T) (otel.ErrorHandler, *runtimeLog) {
	t.Helper()
	logs := &runtimeLog{}
	handler := otelx.NewErrorHandler(slog.New(slog.NewJSONHandler(logs, nil)))
	previous := otel.GetErrorHandler()
	otel.SetErrorHandler(handler)
	t.Cleanup(func() { otel.SetErrorHandler(previous) })
	return handler, logs
}

func verifyServiceDiagnostic(t *testing.T, handler otel.ErrorHandler, logs *runtimeLog, err error, credential, statusText string) {
	t.Helper()
	handler.Handle(err)
	output := logs.String()
	if strings.Contains(output, credential) || strings.Contains(output, "collector rejected") {
		t.Fatal("serialized diagnostic exposes collector response text")
	}
	if !strings.Contains(output, statusText) || !strings.Contains(output, `"operation":"telemetry"`) || !strings.Contains(output, `"stage":"sdk"`) {
		t.Fatal("serialized diagnostic lost operation, stage, or status")
	}
}

func TestServiceCollectorRedirectDoesNotExposeCredentials(t *testing.T) {
	const credential = "example-redirect-secret"
	for _, location := range []string{"http://example.test/?token=" + credential, "://invalid/" + credential} {
		t.Run("redirect", func(t *testing.T) {
			handler, logs := installServiceErrorHandler(t)
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", location)
				http.Error(w, "collector rejected "+r.Header.Get("Authorization"), http.StatusTemporaryRedirect)
			}))
			defer collector.Close()
			cfg := otelx.ServiceConfig{
				Enabled:        true,
				TraceProtocol:  "http",
				Endpoint:       strings.TrimPrefix(collector.URL, "http://"),
				LocalPlaintext: true,
				Headers:        map[string]string{"authorization": credential},
				MetricsMode:    "prometheus",
				SampleRatio:    1,
				Environment:    "test",
			}
			res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
			rt, err := otelx.SetupService(t.Context(), cfg, res)
			if err != nil {
				t.Fatal(err)
			}
			_, span := rt.TracerProvider.Tracer("test").Start(t.Context(), "test")
			span.End()
			err = rt.Shutdown(context.Background())
			if err == nil || !strings.Contains(err.Error(), "307") || !strings.Contains(err.Error(), "export telemetry") {
				t.Fatal("redirect rejection lost its operation or status")
			}
			if strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "collector rejected") {
				t.Fatal("redirect diagnostic exposed collector data")
			}
			verifyServiceDiagnostic(t, handler, logs, err, credential, "307")
		})
	}
}
