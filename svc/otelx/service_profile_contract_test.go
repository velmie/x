package otelx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/svc/confload"
	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/validation"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestServiceConfigOnlyValidationAndIdentity(t *testing.T) {
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    "unused",
		MetricsMode:   "prometheus",
		Environment:   "test",
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment=external")
	if err := otelx.ValidateServiceConfig(cfg); err != nil {
		t.Fatalf("config-only validation: %v", err)
	}
	_, err := otelx.SetupService(t.Context(), cfg, resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1")))
	var validationErr *validation.Error
	if !errors.Is(err, otelx.ErrInvalidConfig) || errors.As(err, &validationErr) {
		t.Fatalf("environment failure is not a plain configuration error: %v", err)
	}
	if !strings.Contains(err.Error(), "OTEL_RESOURCE_ATTRIBUTES") || !strings.Contains(err.Error(), "unset") {
		t.Fatalf("environment failure does not name the variable and expectation: %v", err)
	}
	if err := os.Unsetenv("OTEL_RESOURCE_ATTRIBUTES"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		res   *resource.Resource
		field string
	}{
		{
			name:  "nil",
			field: "service.name",
		},
		{
			name:  "missing",
			res:   resource.NewSchemaless(attribute.String("service.version", "1")),
			field: "service.name",
		},
		{
			name:  "non-string",
			res:   resource.NewSchemaless(attribute.String("service.name", "test"), attribute.Int("service.version", 1)),
			field: "service.version",
		},
		{
			name:  "bad-format",
			res:   resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "bad version")),
			field: "service.version",
		},
		{
			name:  "too-long",
			res:   resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", strings.Repeat("x", 129))),
			field: "service.version",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := otelx.SetupService(t.Context(), cfg, tc.res)
			var failure *validation.Error
			if !errors.Is(err, otelx.ErrInvalidConfig) || errors.As(err, &failure) {
				t.Fatalf("identity failure is not a plain configuration error: %v", err)
			}
			if !strings.Contains(err.Error(), tc.field) || !strings.Contains(err.Error(), "128") {
				t.Fatalf("identity failure does not name the attribute and format: %v", err)
			}
		})
	}
	cfg.BatchTimeout = -time.Second
	err = otelx.ValidateServiceConfig(cfg)
	if !errors.As(err, &validationErr) || !errors.Is(err, otelx.ErrInvalidConfig) || !strings.Contains(err.Error(), "batch_timeout") {
		t.Fatalf("config failure: %v", err)
	}
}

func TestServiceTimingDefaultsAndNegativeValues(t *testing.T) {
	cfg, err := confload.LoadInto[otelx.ServiceConfig](confload.New())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BatchTimeout != 100*time.Millisecond || cfg.MetricInterval != time.Second || cfg.ExportTimeout != time.Second {
		t.Fatalf("defaults: %+v", cfg)
	}
	base := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    "unused",
		MetricsMode:   "prometheus",
		Environment:   "test",
	}
	for _, tc := range []struct {
		name string
		set  func(*otelx.ServiceConfig)
	}{
		{
			name: "batch_timeout",
			set:  func(c *otelx.ServiceConfig) { c.BatchTimeout = -1 },
		},
		{
			name: "metric_interval",
			set:  func(c *otelx.ServiceConfig) { c.MetricInterval = -1 },
		},
		{
			name: "export_timeout",
			set:  func(c *otelx.ServiceConfig) { c.ExportTimeout = -1 },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.set(&cfg)
			err := otelx.ValidateServiceConfig(cfg)
			var validationErr *validation.Error
			if !errors.As(err, &validationErr) || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("negative duration: %v", err)
			}
		})
	}
}

func TestServiceExportIntervals(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		timeout  time.Duration
	}{
		{
			name:    "defaults",
			timeout: 3 * time.Second,
		},
		{
			name:     "configured",
			interval: 25 * time.Millisecond,
			timeout:  900 * time.Millisecond,
		},
	} {
		t.Run(tc.name, func(t *testing.T) { testServiceExportIntervals(t, tc.interval, tc.timeout) })
	}
}

func testServiceExportIntervals(t *testing.T, interval, deadline time.Duration) {
	requests := make(chan string, 10)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- r.URL.Path:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "http",
		Endpoint:       strings.TrimPrefix(collector.URL, "http://"),
		LocalPlaintext: true,
		Environment:    "test",
		SampleRatio:    1,
		BatchTimeout:   interval,
		MetricInterval: interval,
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	rt, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	_, span := rt.TracerProvider.Tracer("test").Start(t.Context(), "span")
	span.End()
	counter, err := rt.MeterProvider.Meter("test").Int64Counter("counter")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 1)
	seen := map[string]bool{}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for !seen["/v1/traces"] || !seen["/v1/metrics"] {
		select {
		case path := <-requests:
			seen[path] = true
		case <-timer.C:
			t.Fatalf("periodic exports missing: %v", seen)
		}
	}
}

func TestServiceConfiguredExportTimeout(t *testing.T) {
	canceled := make(chan struct{}, 1)
	release := make(chan struct{})
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read collector request: %v", err)
			return
		}
		select {
		case <-r.Context().Done():
		case <-release:
			return
		}
		select {
		case canceled <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(func() { close(release); collector.Close() })
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "http",
		Endpoint:       strings.TrimPrefix(collector.URL, "http://"),
		LocalPlaintext: true,
		Environment:    "test",
		SampleRatio:    1,
		MetricsMode:    "prometheus",
		BatchTimeout:   20 * time.Millisecond,
		ExportTimeout:  80 * time.Millisecond,
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	rt, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Shutdown(context.Background()); err != nil && !strings.Contains(strings.ToLower(err.Error()), "timeout") {
			t.Errorf("shutdown telemetry: %v", err)
		}
	})
	_, span := rt.TracerProvider.Tracer("test").Start(t.Context(), "span")
	span.End()
	select {
	case <-canceled:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("export was not canceled within configured timeout")
	}
}

func TestServiceEmptyResourceAttributesAreUnset(t *testing.T) {
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    filepath.Join(t.TempDir(), "traces.json"),
		MetricsMode:   "prometheus",
		Environment:   "test",
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	identity := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	runtime, err := otelx.SetupService(t.Context(), cfg, identity)
	if err != nil {
		t.Fatalf("empty OTEL_RESOURCE_ATTRIBUTES must behave as unset: %v", err)
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
