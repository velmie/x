package otelx_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/velmie/x/svc/confload"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestSetupServiceOwnsOutputAndExportsSelectedSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.json")
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    path,
		MetricsMode:   "prometheus",
		SampleRatio:   1,
		Environment:   "test",
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	runtime, err := otelx.SetupService(context.Background(), cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	_, span := runtime.TracerProvider.Tracer("test").Start(context.Background(), "test-span")
	span.End()
	counter, err := runtime.MeterProvider.Meter("test").Int64Counter("test_counter")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 1)
	if runtime.PrometheusHandler == nil {
		t.Fatal("missing metrics handler")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-span") {
		t.Fatal("span missing from owned output")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if _, err := otelx.SetupService(context.Background(), cfg, res); err == nil {
		t.Fatal("existing output accepted")
	}
}

func TestServiceConfigValidationAndDisabledSetup(t *testing.T) {
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		TraceProtocol:  "grpc",
		Endpoint:       "127.0.0.1:4317",
		LocalPlaintext: true,
		Environment:    "test",
	}
	err := otelx.ValidateServiceConfig(cfg, res)
	if err == nil || !errors.Is(err, otelx.ErrInvalidConfig) || !strings.Contains(err.Error(), "metrics_endpoint") {
		t.Fatalf("unexpected validation: %v", err)
	}
	cfg.Enabled = false
	cfg.StdoutFile = filepath.Join(t.TempDir(), "absent")
	cfg.TraceProtocol = "invalid"
	runtime, err := otelx.SetupService(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.StdoutFile); !os.IsNotExist(err) {
		t.Fatalf("disabled setup acquired output: %v", err)
	}
}

func TestServiceSamplingAndIndependentRegistries(t *testing.T) {
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	makeRuntime := func() (*otelx.Runtime, string) {
		path := filepath.Join(t.TempDir(), "out")
		rt, err := otelx.SetupService(t.Context(), otelx.ServiceConfig{
			Enabled:       true,
			TraceProtocol: "stdout",
			StdoutFile:    path,
			MetricsMode:   "prometheus",
			SampleRatio:   0,
			Environment:   "test",
		}, res)
		if err != nil {
			t.Fatal(err)
		}
		return rt, path
	}
	first, path := makeRuntime()
	second, _ := makeRuntime()
	counter, err := first.MeterProvider.Meter("test").Int64Counter("unique_counter")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 1)
	a, b := httptest.NewRecorder(), httptest.NewRecorder()
	first.PrometheusHandler.ServeHTTP(a, httptest.NewRequest("GET", "/", nil))
	second.PrometheusHandler.ServeHTTP(b, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(a.Body.String(), "unique_counter") || strings.Contains(b.Body.String(), "unique_counter") {
		t.Fatal("registries are not independent")
	}
	_, span := first.TracerProvider.Tracer("test").Start(t.Context(), "not-sampled")
	span.End()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := first.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := second.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-sampled") {
		t.Fatal("zero sampling exported a root span")
	}
}

func TestServiceEnvAliasesAndDescription(t *testing.T) {
	aliases := otelx.ServiceEnvAliases("telemetry")
	if aliases["telemetry.trace_protocol"] != "OTELX_TRACE_PROTOCOL" || aliases["telemetry.headers"] != "OTELX_HEADERS" {
		t.Fatalf("aliases: %v", aliases)
	}
	if otelx.ServiceEnvAliases("")["enabled"] != "OTELX_ENABLED" {
		t.Fatal("root aliases invalid")
	}
	fields, err := confload.DescribeStruct[otelx.ServiceConfig]()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"endpoint", "metrics_endpoint", "ca_file", "stdout_file"} {
		field, ok := fields.Lookup(name)
		if !ok || field.Attributes["sensitive"] == "true" {
			t.Fatalf("%s must be visible", name)
		}
	}
	field, ok := fields.Lookup("headers")
	if !ok || field.Attributes["sensitive"] != "true" {
		t.Fatal("headers must be sensitive")
	}
}

func TestServiceConfigConfloadInspectionAndValues(t *testing.T) {
	type config struct {
		Telemetry otelx.ServiceConfig `k:"telemetry"`
	}
	t.Setenv("OTELX_ENABLED", "true")
	t.Setenv("OTELX_SAMPLE_RATIO", "0")
	t.Setenv("OTELX_HEADERS", `{"authorization":"private-value"}`)
	loader := confload.New(
		confload.WithEnvAliases(otelx.ServiceEnvAliases("telemetry")),
		confload.WithDecodeHooks(confload.JSONStringMapHook()),
	)
	var cfg config
	if err := loader.Unmarshal(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Telemetry.Enabled || cfg.Telemetry.SampleRatio != 0 || cfg.Telemetry.Headers["authorization"] != "private-value" {
		t.Fatal("environment values not loaded")
	}
	fields, err := confload.DescribeStruct[config]()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := loader.Inspect(fields, cfg, loader.Snapshot(), confload.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var headersSeen, endpointSeen bool
	for _, row := range rows {
		switch row.Field.Path {
		case "telemetry.headers":
			headersSeen = row.Value == "[REDACTED]" && row.Env == "OTELX_HEADERS"
		case "telemetry.endpoint":
			endpointSeen = row.Value == "127.0.0.1:4318" && row.Env == "OTELX_ENDPOINT"
		}
	}
	if !headersSeen || !endpointSeen {
		t.Fatalf("inspection did not mask headers or expose endpoint: %v", rows)
	}
	t.Setenv("OTELX_ENABLED", "not-a-bool")
	sourceFree := confload.New(
		confload.WithFiles(filepath.Join(t.TempDir(), "missing.yaml")),
		confload.WithEnvAliases(otelx.ServiceEnvAliases("telemetry")),
	)
	if _, err := sourceFree.Inspect(fields, nil, confload.Snapshot{}, confload.InspectOptions{}); err != nil {
		t.Fatalf("schema inspection read configuration sources: %v", err)
	}
}
