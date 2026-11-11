package otelx_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/svc/otelx"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const collectorCanary = "collector-secret-canary"

func TestServiceHTTPCollectorStatusLogged(t *testing.T) {
	for _, code := range []int{400, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			_, logs := installServiceErrorHandler(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, collectorCanary, code)
			}))
			defer server.Close()
			cfg := otelx.ServiceConfig{
				Enabled:        true,
				TraceProtocol:  "http",
				Endpoint:       strings.TrimPrefix(server.URL, "http://"),
				LocalPlaintext: true,
				MetricsMode:    "prometheus",
				SampleRatio:    1,
				Environment:    "test",
				BatchTimeout:   10 * time.Millisecond,
			}
			rt, err := otelx.SetupService(t.Context(), cfg, exportResource())
			if err != nil {
				t.Fatal(err)
			}
			produceExport(t, rt)
			deadline := time.Now().Add(3 * time.Second)
			for logs.String() == "" && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			output := logs.String()
			if !strings.Contains(output, fmt.Sprint(code)) || !strings.Contains(output, server.URL+"/v1/traces") || strings.Contains(output, collectorCanary) {
				t.Errorf("bad logged diagnostic: %s", output)
			}
			if err := rt.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServiceHTTPPartialSuccessIsDiscarded(t *testing.T) {
	_, logs := installServiceErrorHandler(t)
	body, err := proto.Marshal(&collectortrace.ExportTraceServiceResponse{
		PartialSuccess: &collectortrace.ExportTracePartialSuccess{
			RejectedSpans: 7,
			ErrorMessage:  collectorCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "http",
		Endpoint:       strings.TrimPrefix(server.URL, "http://"),
		LocalPlaintext: true,
		MetricsMode:    "prometheus",
		SampleRatio:    1,
		Environment:    "test",
	}
	rt, err := otelx.SetupService(t.Context(), cfg, exportResource())
	if err != nil {
		t.Fatal(err)
	}
	produceExport(t, rt)
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("partial response: %v", err)
	}
	if got := logs.String(); got != "" {
		t.Fatalf("unexpected partial-success log: %s", got)
	}
}

func TestServiceHTTPMetricPartialSuccessIsDiscarded(t *testing.T) {
	_, logs := installServiceErrorHandler(t)
	body, err := proto.Marshal(&collectormetric.ExportMetricsServiceResponse{
		PartialSuccess: &collectormetric.ExportMetricsPartialSuccess{
			RejectedDataPoints: 9,
			ErrorMessage:       collectorCanary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cfg := otelx.ServiceConfig{
		Enabled:         true,
		TraceProtocol:   "stdout",
		StdoutFile:      filepath.Join(t.TempDir(), "traces"),
		MetricsEndpoint: strings.TrimPrefix(server.URL, "http://"),
		LocalPlaintext:  true,
		MetricsMode:     "otlp",
		SampleRatio:     1,
		Environment:     "test",
	}
	rt, err := otelx.SetupService(t.Context(), cfg, exportResource())
	if err != nil {
		t.Fatal(err)
	}
	produceExport(t, rt)
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("metric partial response: %v", err)
	}
	if got := logs.String(); got != "" {
		t.Fatalf("unexpected metric partial-success log: %s", got)
	}
}

func TestServiceGRPCPartialSuccessDropsMessage(t *testing.T) {
	handler, logs := installServiceErrorHandler(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, &partialCollector{})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "grpc",
		Endpoint:       listener.Addr().String(),
		LocalPlaintext: true,
		MetricsMode:    "prometheus",
		SampleRatio:    1,
		Environment:    "test",
	}
	rt, err := otelx.SetupService(t.Context(), cfg, exportResource())
	if err != nil {
		t.Fatal(err)
	}
	produceExport(t, rt)
	err = rt.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "7") || strings.Contains(err.Error(), collectorCanary) {
		t.Fatalf("partial success error: %v", err)
	}
	handler.Handle(err)
	if output := logs.String(); strings.Contains(output, collectorCanary) || !strings.Contains(output, "7 spans rejected") {
		t.Fatalf("partial-success diagnostic: %s", output)
	}
}

type partialCollector struct {
	collectortrace.UnimplementedTraceServiceServer
}

func (*partialCollector) Export(context.Context, *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	return &collectortrace.ExportTraceServiceResponse{
		PartialSuccess: &collectortrace.ExportTracePartialSuccess{
			RejectedSpans: 7,
			ErrorMessage:  collectorCanary,
		},
	}, nil
}

func TestServiceGRPCLocalFailuresKeepCause(t *testing.T) {
	collector := newExportCollector(t, "grpc-trace")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		endpoint  string
		ca        string
		plaintext bool
		want      string
	}{
		{
			name:      "refused",
			endpoint:  refused,
			plaintext: true,
			want:      "connection refused",
		},
		{
			name:     "untrusted",
			endpoint: collector.endpoint,
			ca:       unrelatedExportCA(t),
			want:     "certificate signed by unknown authority",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := collector.config("grpc-trace")
			cfg.Endpoint, cfg.CAFile, cfg.LocalPlaintext = tc.endpoint, tc.ca, tc.plaintext
			rt, err := otelx.SetupService(t.Context(), cfg, exportResource())
			if err != nil {
				t.Fatal(err)
			}
			produceExport(t, rt)
			err = rt.Shutdown(context.Background())
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("missing transport cause %q: %v", tc.want, err)
			}
		})
	}
}
