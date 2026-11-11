package otelx_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestServiceAcceptsUnrestrictedIdentityValues(t *testing.T) {
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    filepath.Join(t.TempDir(), "traces"),
		MetricsMode:   "prometheus",
		Environment:   "тест " + strings.Repeat("x", 129),
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", ""),
		attribute.String("service.version", "версия "+strings.Repeat("x", 129)),
	)
	runtime, err := otelx.SetupService(t.Context(), cfg, res)
	if err != nil {
		t.Fatalf("setup rejected string identity: %v", err)
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg.Environment = ""
	if err := otelx.ValidateServiceConfig(cfg); err == nil {
		t.Fatal("empty environment accepted")
	}
}
