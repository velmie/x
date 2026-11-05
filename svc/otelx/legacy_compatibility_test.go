package otelx

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

func TestLegacySetupWrapsExporterConstructorCause(t *testing.T) {
	cfg := DefaultConfig
	cfg.Communication.ExportMethod = ExportMethodGRPC
	cfg.Communication.Endpoint = "%"
	_, err := Setup(context.Background(), &cfg)
	var escape url.EscapeError
	if !errors.As(err, &escape) {
		t.Fatalf("constructor error lost its typed cause: %T %v", err, err)
	}
}

func TestLegacySetupCleansPartialSetupAndJoinsCauses(t *testing.T) {
	primary := errors.New("detector failed")
	secondary := errors.New("exporter cleanup failed")
	exporter := &legacyExporter{
		shutdownErr: secondary,
	}
	cfg := DefaultConfig
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Setup(
		ctx,
		&cfg,
		WithExporter(exporter),
		WithResourceDetectors(
			failingDetector{
				cause: primary,
			},
		),
	)
	if !errors.Is(err, primary) || !errors.Is(err, secondary) {
		t.Fatalf("lost setup or cleanup failure: %v", err)
	}
	if exporter.shutdowns.Load() != 1 || exporter.canceled.Load() {
		t.Fatal("exporter was not closed exactly once with detached cancellation")
	}
}

func TestLegacyDisabledSetupDoesNotConstructExporter(t *testing.T) {
	restoreLegacyGlobals(t)
	cfg := DefaultConfig
	cfg.Disabled = true
	cfg.Communication.ExportMethod = "unknown"
	result, err := Setup(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("disabled setup attempted to create exporter: %v", err)
	}
	if _, ok := result.Exporter.(NoopExporter); !ok {
		t.Fatalf("disabled exporter is not noop: %T", result.Exporter)
	}
	provider := result.TracerProvider.(interface{ Shutdown(context.Context) error })
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyGRPCAuthorizationHeader(t *testing.T) {
	restoreLegacyGlobals(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collector := &traceCollector{
		requests: make(chan *collectortrace.ExportTraceServiceRequest, 2),
		auth:     make(chan string, 2),
	}
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	cfg := DefaultConfig
	cfg.Communication = CommunicationConfig{
		Endpoint:     listener.Addr().String(),
		ExportMethod: ExportMethodGRPC,
	}
	cfg.Security = SecurityConfig{
		AuthorizationHeader: "Bearer legacy-test-token",
		Insecure:            true,
	}
	result, err := Setup(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, span := result.TracerProvider.Tracer("legacy-test").Start(context.Background(), "legacy operation")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := result.TracerProvider.(interface{ Shutdown(context.Context) error })
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case auth := <-collector.auth:
		if auth != "Bearer legacy-test-token" {
			t.Fatalf("authorization header not sent: %q", auth)
		}
	case <-ctx.Done():
		t.Fatal("collector did not receive the span")
	}
}

func restoreLegacyGlobals(t *testing.T) {
	t.Helper()
	tracer := otel.GetTracerProvider()
	propagator := otel.GetTextMapPropagator()
	t.Cleanup(
		func() {
			otel.SetTracerProvider(tracer)
			otel.SetTextMapPropagator(propagator)
		},
	)
}

type failingDetector struct {
	cause error
}

func (d failingDetector) Detect(context.Context) (*resource.Resource, error) {
	return nil, d.cause
}

type legacyExporter struct {
	shutdownErr error
	shutdowns   atomic.Int64
	canceled    atomic.Bool
}

func (*legacyExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }

func (e *legacyExporter) Shutdown(ctx context.Context) error {
	e.shutdowns.Add(1)
	e.canceled.Store(ctx.Err() != nil)
	return e.shutdownErr
}

func TestLegacyFailedSetupDoesNotLeaveBatchWorkers(t *testing.T) {
	before := batchWorkerCount(t)
	for range 16 {
		cfg := DefaultConfig
		_, err := Setup(
			context.Background(),
			&cfg,
			WithExporter(NoopExporter{}),
			WithResourceDetectors(
				failingDetector{
					cause: errors.New("detector failed"),
				},
			),
		)
		if err == nil {
			t.Fatal("expected detector failure")
		}
	}
	for range 100 {
		runtime.Gosched()
	}
	if after := batchWorkerCount(t); after > before {
		t.Fatalf("failed setup left SDK batch workers: before=%d after=%d", before, after)
	}
}

func batchWorkerCount(t *testing.T) int {
	t.Helper()
	var stacks bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
		t.Fatal(err)
	}
	return strings.Count(stacks.String(), "go.opentelemetry.io/otel/sdk/trace.(*batchSpanProcessor).processQueue(")
}
