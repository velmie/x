package otelx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

func TestErrorHandlerPassesOriginalErrorObject(t *testing.T) {
	sentinel := errors.New("collector unavailable")
	cause := fmt.Errorf("trace export: %w", sentinel)
	var output bytes.Buffer
	capture := &captureErrorHandler{
		Handler: slog.NewJSONHandler(&output, nil),
	}
	NewErrorHandler(slog.New(capture)).Handle(cause)
	if capture.err != cause {
		t.Fatalf("handler received %v, want original error %v", capture.err, cause)
	}
	if !errors.Is(capture.err, sentinel) {
		t.Fatalf("handler lost error cause: %v", capture.err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["error"] != cause.Error() || record["operation"] != "telemetry" || record["stage"] != "sdk" {
		t.Fatalf("unexpected log record: %v", record)
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
				ErrorMessage:  "collector limit reached",
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
	if !strings.Contains(got, "collector limit reached") || !strings.Contains(got, "2 spans rejected") ||
		!strings.Contains(got, "stream close failed") {
		t.Fatalf("joined diagnostic lost exporter details: %s", got)
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
