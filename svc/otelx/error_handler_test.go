package otelx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestErrorHandlerRetainsCausesAndSanitizesExporterDiagnostics(t *testing.T) {
	body := errors.New("body: private collector response\nsecond secret line")
	collector := fmt.Errorf(
		"failed to send to https://test-user:test-password@collector.example/path?token=test-secret: 400 Bad Request (%w)",
		body,
	)
	network := &url.Error{
		Op:  "Post",
		URL: "https://test-user:test-password@collector.example/path?token=test-secret",
		Err: errors.New("connection refused"),
	}
	cause := errors.Join(
		fmt.Errorf("trace export: %w", collector),
		fmt.Errorf("metric export: %w", network),
		fmt.Errorf("rpc export: %w", status.Error(codes.Unavailable, "collector secret")),
		errors.New("writer: disk full"),
	)
	var output bytes.Buffer
	capture := &captureErrorHandler{
		Handler: slog.NewJSONHandler(&output, nil),
	}
	NewErrorHandler(slog.New(capture)).Handle(cause)
	if !errors.Is(capture.err, body) || !errors.Is(capture.err, network.Err) {
		t.Fatalf("structured diagnostic lost original causes: %v", capture.err)
	}
	var networkCause *url.Error
	if !errors.As(capture.err, &networkCause) || networkCause != network {
		t.Fatalf("structured diagnostic lost the typed cause: %v", capture.err)
	}
	got := output.String()
	for _, want := range []string{"trace export", "400 Bad Request", "metric export", "connection refused", "Unavailable", "disk full"} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic omitted %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{
		"private collector response",
		"second secret",
		"test-user",
		"test-password",
		"token=test-secret",
		"collector secret",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("diagnostic leaked %q: %s", forbidden, got)
		}
	}
}

type captureErrorHandler struct {
	slog.Handler
	err error
}

func (h *captureErrorHandler) Handle(ctx context.Context, record slog.Record) error {
	record.Attrs(
		func(attr slog.Attr) bool {
			if attr.Key == "error" {
				h.err, _ = attr.Value.Any().(error)
			}
			return true
		},
	)
	return h.Handler.Handle(ctx, record)
}

func TestErrorHandlerSanitizesRealOTLPHTTPFailure(t *testing.T) {
	collector := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, "collector-token\nother-secret")
			},
		),
	)
	defer collector.Close()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracehttp.New(
					ctx,
					otlptracehttp.WithEndpointURL(collector.URL+"/v1/traces?api_key=private"),
					otlptracehttp.WithRetry(
						otlptracehttp.RetryConfig{
							Enabled: false,
						},
					),
				)
			},
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithBatchTimeout(time.Hour),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	err = r.Shutdown(context.Background())
	if err == nil {
		t.Fatal("collector failure was lost")
	}
	var output bytes.Buffer
	NewErrorHandler(slog.New(slog.NewJSONHandler(&output, nil))).Handle(err)
	got := output.String()
	if !strings.Contains(got, "400 Bad Request") || !strings.Contains(got, "traces export") {
		t.Fatalf("diagnostic lost failure context: %s", got)
	}
	for _, forbidden := range []string{"collector-token", "other-secret", "api_key=private"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, got)
		}
	}
}

func TestErrorHandlerSanitizesRealOTLPPartialSuccess(t *testing.T) {
	response, err := proto.Marshal(
		&collectortrace.ExportTraceServiceResponse{
			PartialSuccess: &collectortrace.ExportTracePartialSuccess{
				RejectedSpans: 2,
				ErrorMessage:  "collector-token\nprivate message",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/x-protobuf")
				_, _ = w.Write(response)
			},
		),
	)
	defer collector.Close()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(collector.URL+"/v1/traces"))
			},
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithBatchTimeout(time.Hour),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	err = r.Shutdown(context.Background())
	if err == nil {
		t.Fatal("partial rejection was lost")
	}
	var output bytes.Buffer
	NewErrorHandler(slog.New(slog.NewJSONHandler(&output, nil))).Handle(err)
	got := output.String()
	if strings.Contains(got, "collector-token") || strings.Contains(got, "private message") {
		t.Fatalf("partial success diagnostic leaked collector response: %s", got)
	}
	if !strings.Contains(got, "partial success") || strings.Count(got, "2 spans rejected") != 1 {
		t.Fatalf("partial success diagnostic must report the rejection once: %s", got)
	}
}

func TestErrorHandlerKeepsRealGRPCConnectionFailure(t *testing.T) {
	conn, err := grpc.NewClient(
		"passthrough:///collector",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(
			func(context.Context, string) (net.Conn, error) {
				return nil, errors.New("connection refused")
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracegrpc.New(
					ctx,
					otlptracegrpc.WithGRPCConn(conn),
					otlptracegrpc.WithRetry(
						otlptracegrpc.RetryConfig{
							Enabled: false,
						},
					),
				)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = r.Shutdown(ctx)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected actual grpc connection failure, got %v", err)
	}
	var output bytes.Buffer
	NewErrorHandler(slog.New(slog.NewJSONHandler(&output, nil))).Handle(err)
	if got := output.String(); !strings.Contains(got, "connection refused") {
		t.Fatalf("diagnostic lost connection failure: %s", got)
	}
}

func TestErrorHandlerKeepsJoinedOTLPPartialSuccessAndCloseFailure(t *testing.T) {
	response, err := proto.Marshal(
		&collectortrace.ExportTraceServiceResponse{
			PartialSuccess: &collectortrace.ExportTracePartialSuccess{
				RejectedSpans: 2,
				ErrorMessage:  "private partial response",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/x-protobuf")
				_, _ = w.Write(response)
			},
		),
	)
	defer collector.Close()
	client := collector.Client()
	client.Transport = &closeFailingTransport{
		base: client.Transport,
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(ctx context.Context) (sdktrace.SpanExporter, error) {
				return otlptracehttp.New(
					ctx,
					otlptracehttp.WithEndpointURL(collector.URL+"/v1/traces"),
					otlptracehttp.WithHTTPClient(client),
				)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	err = r.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stream close failed") {
		t.Fatalf("native exporter did not return the close failure: %v", err)
	}
	var output bytes.Buffer
	NewErrorHandler(slog.New(slog.NewJSONHandler(&output, nil))).Handle(err)
	got := output.String()
	if strings.Contains(got, "private partial response") || !strings.Contains(got, "2 spans rejected") ||
		!strings.Contains(got, "stream close failed") {
		t.Fatalf("joined diagnostic lost a cause or leaked response: %s", got)
	}
}

type closeFailingTransport struct {
	base http.RoundTripper
}

func (t *closeFailingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil {
		response.Body = &closeFailingBody{
			ReadCloser: response.Body,
		}
	}
	return response, err
}

type closeFailingBody struct {
	io.ReadCloser
}

func (b *closeFailingBody) Close() error {
	return errors.Join(b.ReadCloser.Close(), errors.New("stream close failed"))
}
