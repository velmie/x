package otelx_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel"
	metriccollector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracecollector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	resourceproto "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const exportCredential = "Bearer PRIVATE_EXPORT_CREDENTIAL"

func TestExportAuthenticatedTransports(t *testing.T) {
	for _, profile := range []string{"http-trace", "http-metric", "grpc-trace"} {
		t.Run(profile, func(t *testing.T) {
			collector := newExportCollector(t, profile)
			// Explicit settings must defeat hostile inherited endpoint, TLS and headers.
			for _, key := range []string{
				"OTEL_EXPORTER_OTLP_ENDPOINT",
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
			} {
				t.Setenv(key, "http://127.0.0.1:1/PRIVATE_AMBIENT")
			}
			for _, key := range []string{
				"OTEL_EXPORTER_OTLP_INSECURE",
				"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
				"OTEL_EXPORTER_OTLP_METRICS_INSECURE",
			} {
				t.Setenv(key, "true")
			}
			for _, key := range []string{
				"OTEL_EXPORTER_OTLP_HEADERS",
				"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
				"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
			} {
				t.Setenv(key, "authorization=PRIVATE_AMBIENT")
			}
			cfg := collector.config(profile)
			cfg.StdoutFile = filepath.Join(t.TempDir(), "traces")
			runtime, err := otelx.SetupService(t.Context(), cfg, exportResource())
			if err != nil {
				t.Fatal(err)
			}
			produceExport(t, runtime)
			if profile != "http-metric" {
				response := httptest.NewRecorder()
				runtime.PrometheusHandler.ServeHTTP(response, httptest.NewRequest("GET", "/bookstore/metrics", nil))
				if !strings.Contains(response.Body.String(), "export_probe_total") {
					t.Fatal("Prometheus reader lost independent counter")
				}
			}
			if err := runtime.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			collector.mu.Lock()
			defer collector.mu.Unlock()
			if profile == "http-metric" {
				if collector.metricAccepted == 0 || collector.traceAccepted != 0 {
					t.Fatal("stdout/HTTP metric reader selection crossed exporters")
				}
			} else if collector.traceAccepted == 0 || collector.metricAccepted != 0 {
				t.Fatal("network trace/Prometheus reader selection crossed exporters")
			}
			if collector.accepted == 0 || collector.rejected != 0 {
				t.Fatalf("accepted=%d rejected=%d", collector.accepted, collector.rejected)
			}
			if len(collector.resources) == 0 {
				t.Fatal("collector accepted no resource data")
			}
			for _, resource := range collector.resources {
				assertExportResource(t, resource)
			}
			if profile == "http-metric" {
				output, err := os.ReadFile(cfg.StdoutFile)
				if err != nil || !strings.Contains(string(output), "export.probe") {
					t.Fatal("stdout trace missing while HTTP metrics active")
				}
			}
		})
	}
}

func TestExportTransportRejections(t *testing.T) {
	var logs runtimeLog
	previous := otel.GetErrorHandler()
	otel.SetErrorHandler(otelx.NewErrorHandler(slog.New(slog.NewJSONHandler(&logs, nil))))
	defer otel.SetErrorHandler(previous)
	for _, profile := range []string{"http-trace", "http-metric", "grpc-trace"} {
		for _, failure := range []string{"ca", "hostname", "credentials"} {
			t.Run(profile+"/"+failure, func(t *testing.T) {
				collector := newExportCollector(t, profile)
				cfg := withOutput(t, collector.config(profile))
				switch failure {
				case "ca":
					cfg.CAFile = unrelatedExportCA(t)
				case "hostname":
					_, port, err := net.SplitHostPort(collector.endpoint)
					if err != nil {
						t.Fatal(err)
					}
					cfg.Endpoint = net.JoinHostPort("localhost", port)
					if profile == "http-metric" {
						cfg.MetricsEndpoint = cfg.Endpoint
					}
				case "credentials":
					cfg.Headers = map[string]string{"authorization": "Bearer PRIVATE_WRONG_CREDENTIAL"}
				}
				runtime, err := otelx.SetupService(t.Context(), cfg, exportResource())
				if err != nil {
					t.Fatalf("valid configuration should reach transport: %v", err)
				}
				produceExport(t, runtime)
				started := time.Now()
				err = runtime.Shutdown(context.Background())
				if err == nil {
					t.Fatal("untrusted or unauthorized collector accepted export")
				}
				if time.Since(started) > 9*time.Second {
					t.Fatal("cleanup exceeded four bounded phases")
				}
				collector.mu.Lock()
				accepted, rejected := collector.accepted, collector.rejected
				collector.mu.Unlock()
				if accepted != 0 {
					t.Fatalf("rejected export stored%d payloads", accepted)
				}
				if failure == "credentials" && rejected == 0 {
					t.Fatal("credential failure never reached authenticating collector")
				}
				if strings.Contains(err.Error(), "PRIVATE_WRONG_CREDENTIAL") || strings.Contains(err.Error(), exportCredential) {
					t.Fatalf("unsafe returned error: %v", err)
				}
			})
		}
	}
	if strings.Contains(logs.String(), "PRIVATE_WRONG_CREDENTIAL") || strings.Contains(logs.String(), exportCredential) {
		t.Fatal("raw transport diagnostic escaped")
	}
}

func TestExportHTTPRedirectsAndAsyncFailure(t *testing.T) {
	var logs runtimeLog
	previous := otel.GetErrorHandler()
	otel.SetErrorHandler(otelx.NewErrorHandler(slog.New(slog.NewJSONHandler(&logs, nil))))
	defer otel.SetErrorHandler(previous)
	for _, profile := range []string{"http-trace", "http-metric"} {
		for _, code := range []int{307, 308} {
			t.Run(profile+"/"+http.StatusText(code), func(t *testing.T) {
				var hits atomic.Int64
				receiver := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(200) }),
				)
				defer receiver.Close()
				collector := newExportCollector(t, profile)
				collector.redirect = receiver.URL
				collector.responseCode = code
				runtime, err := otelx.SetupService(t.Context(), withOutput(t, collector.config(profile)), exportResource())
				if err != nil {
					t.Fatal(err)
				}
				produceExport(t, runtime)
				if err := runtime.Shutdown(context.Background()); err == nil {
					t.Fatal("redirect export succeeded")
				}
				if hits.Load() != 0 {
					t.Fatal("redirect forwarded credentials or payload")
				}
			})
		}
	}
	collector := newExportCollector(t, "http-trace")
	collector.responseCode = 400
	runtime, err := otelx.SetupService(t.Context(), withOutput(t, collector.config("http-trace")), exportResource())
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Count(logs.String(), `"stage":"sdk"`)
	produceExport(t, runtime)
	deadline := time.Now().Add(3 * time.Second)
	for strings.Count(logs.String(), `"stage":"sdk"`) == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Count(logs.String(), `"stage":"sdk"`) == before {
		t.Error("actual asynchronous exporter failure was silent")
	}
	_ = runtime.Shutdown(context.Background())
	if strings.Contains(logs.String(), exportCredential) {
		t.Fatal("exporter leaked response or credentials")
	}
}

type exportCollector struct {
	tracecollector.UnimplementedTraceServiceServer
	mu             sync.Mutex
	endpoint       string
	caFile         string
	redirect       string
	responseCode   int
	accepted       int
	rejected       int
	traceAccepted  int
	metricAccepted int
	resources      []*resourceproto.Resource
}

func newExportCollector(t *testing.T, profile string) *exportCollector {
	t.Helper()
	collector := &exportCollector{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS12 {
			t.Error("collector received unverified transport")
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Error("invalid OTLP HTTP protocol")
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Authorization") != exportCredential {
			collector.mu.Lock()
			collector.rejected++
			collector.mu.Unlock()
			http.Error(w, "collector rejected credentials", http.StatusUnauthorized)
			return
		}
		if collector.responseCode != 0 {
			if collector.redirect != "" {
				w.Header().Set("Location", collector.redirect)
			}
			http.Error(w, "collector rejected request", collector.responseCode)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		collector.mu.Lock()
		defer collector.mu.Unlock()
		switch r.URL.Path {
		case "/v1/traces":
			collector.traceAccepted++
			var request tracecollector.ExportTraceServiceRequest
			if err := proto.Unmarshal(data, &request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			for _, resource := range request.ResourceSpans {
				collector.resources = append(collector.resources, resource.Resource)
			}
		case "/v1/metrics":
			collector.metricAccepted++
			var request metriccollector.ExportMetricsServiceRequest
			if err := proto.Unmarshal(data, &request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			for _, resource := range request.ResourceMetrics {
				collector.resources = append(collector.resources, resource.Resource)
			}
		default:
			t.Errorf("unexpected OTLP path %q", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		collector.accepted++
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	collector.caFile = filepath.Join(t.TempDir(), "collector-ca.pem")
	if err := os.WriteFile(collector.caFile, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	}), 0600); err != nil {
		t.Fatal(err)
	}
	collector.endpoint = strings.TrimPrefix(server.URL, "https://")
	if profile != "grpc-trace" {
		return collector
	}
	certificate := server.TLS.Certificates[0]
	server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	})))
	tracecollector.RegisterTraceServiceServer(grpcServer, collector)
	done := make(chan error, 1)
	go func() { done <- grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); <-done })
	collector.endpoint = listener.Addr().String()
	return collector
}

func (c *exportCollector) Export(
	ctx context.Context,
	request *tracecollector.ExportTraceServiceRequest,
) (*tracecollector.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	values, _ := metadata.FromIncomingContext(ctx)
	auth := values.Get("authorization")
	if len(auth) != 1 || auth[0] != exportCredential {
		c.rejected++
		return nil, status.Error(codes.Unauthenticated, "collector rejected credentials")
	}
	for _, resource := range request.ResourceSpans {
		c.resources = append(c.resources, resource.Resource)
	}
	c.accepted++
	c.traceAccepted++
	return &tracecollector.ExportTraceServiceResponse{}, nil
}
func (c *exportCollector) config(profile string) otelx.ServiceConfig {
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		Endpoint:      c.endpoint,
		CAFile:        c.caFile,
		Headers:       map[string]string{"authorization": exportCredential},
		SampleRatio:   1,
		Environment:   "test",
		MetricsMode:   "prometheus",
		TraceProtocol: "http",
	}
	if profile == "grpc-trace" {
		cfg.TraceProtocol = "grpc"
	}
	if profile == "http-metric" {
		cfg.TraceProtocol = "stdout"
		cfg.MetricsMode = "otlp"
		cfg.MetricsEndpoint = c.endpoint
	}
	return cfg
}
func exportResource() *resource.Resource {
	return resource.NewSchemaless(attribute.String("service.name", "export-test"), attribute.String("service.version", "v1.2.3"), attribute.String("service.commit", "abc123"), attribute.String("build.date", "2026-09-28T00:00:00Z"))
}
func withOutput(t *testing.T, cfg otelx.ServiceConfig) otelx.ServiceConfig {
	t.Helper()
	cfg.StdoutFile = filepath.Join(t.TempDir(), "traces")
	return cfg
}
func produceExport(t *testing.T, runtime *otelx.Runtime) {
	t.Helper()
	_, span := runtime.TracerProvider.Tracer("test").Start(context.Background(), "export.probe")
	span.End()
	counter, err := runtime.MeterProvider.Meter("test").Int64Counter("export.probe")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
}
func assertExportResource(t *testing.T, resource *resourceproto.Resource) {
	t.Helper()
	got := map[string]string{}
	for _, a := range resource.Attributes {
		got[a.Key] = a.Value.GetStringValue()
	}
	want := map[string]string{
		"service.name":                "export-test",
		"service.version":             "v1.2.3",
		"deployment.environment.name": "test",
		"service.commit":              "abc123",
		"build.date":                  "2026-09-28T00:00:00Z",
	}
	if len(got) != len(want) {
		t.Fatalf("resource fields=%v", got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("resource%s=%q want%q", key, got[key], value)
		}
	}
}
func unrelatedExportCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "unrelated",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "unrelated.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type runtimeLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *runtimeLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *runtimeLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }

func TestServiceIgnoresAmbientCertificateForPlaintext(t *testing.T) {
	collector := newExportCollector(t, "http-trace")
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", collector.caFile)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer receiver.Close()
	for _, profile := range []string{"http-trace", "http-metric"} {
		t.Run(profile, func(t *testing.T) {
			cfg := withOutput(t, collector.config(profile))
			cfg.LocalPlaintext = true
			cfg.Endpoint = strings.TrimPrefix(receiver.URL, "http://")
			if profile == "http-metric" {
				cfg.MetricsEndpoint = cfg.Endpoint
			}
			cfg.CAFile = ""
			runtime, err := otelx.SetupService(t.Context(), cfg, exportResource())
			if err != nil {
				t.Fatalf("ambient certificate affected explicit plaintext: %v", err)
			}
			if err := runtime.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
