package otelx_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/validation"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestValidateServiceSetup(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	identity := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	base := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    filepath.Join(t.TempDir(), "traces"),
		MetricsMode:   "prometheus",
		Environment:   "test",
	}
	for _, tc := range []struct {
		name string
		edit func(*otelx.ServiceConfig) *resource.Resource
		env  string
		path string
	}{
		{
			name: "required field",
			edit: func(c *otelx.ServiceConfig) *resource.Resource { c.Environment = ""; return identity },
			path: "environment",
		},
		{
			name: "conditional field",
			edit: func(c *otelx.ServiceConfig) *resource.Resource {
				c.RuntimeMetrics = true
				c.MetricsMode = "otlp"
				return identity
			},
			path: "runtime_metrics",
		},
		{
			name: "propagation",
			edit: func(c *otelx.ServiceConfig) *resource.Resource { c.Propagation = "invalid"; return identity },
			path: "propagation",
		},
		{
			name: "missing identity",
			edit: func(*otelx.ServiceConfig) *resource.Resource { return nil },
		},
		{
			name: "identity type",
			edit: func(*otelx.ServiceConfig) *resource.Resource {
				return resource.NewSchemaless(attribute.String("service.name", "test"), attribute.Int("service.version", 1))
			},
		},
		{
			name: "environment",
			edit: func(*otelx.ServiceConfig) *resource.Resource { return identity },
			env:  "deployment=external",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", tc.env)
			cfg := base
			res := tc.edit(&cfg)
			got := otelx.ValidateServiceSetup(cfg, res)
			_, setupErr := otelx.SetupService(t.Context(), cfg, res)
			if got == nil || setupErr == nil || got.Error() != setupErr.Error() || !errors.Is(got, otelx.ErrInvalidConfig) || !errors.Is(setupErr, otelx.ErrInvalidConfig) {
				t.Fatalf("validation = %v, setup = %v", got, setupErr)
			}
			var failure *validation.Error
			if tc.path != "" && (!errors.As(got, &failure) || !strings.Contains(got.Error(), tc.path)) {
				t.Fatalf("expected relative field %s: %v", tc.path, got)
			}
			if tc.path == "" && errors.As(got, &failure) {
				t.Fatalf("unexpected field validation error: %v", got)
			}
		})
	}
	if err := otelx.ValidateServiceSetup(base, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base.StdoutFile); !os.IsNotExist(err) {
		t.Fatalf("validation created output: %v", err)
	}
	base.Enabled = false
	if err := otelx.ValidateServiceSetup(base, nil); err != nil {
		t.Fatalf("disabled telemetry: %v", err)
	}
}

func TestValidateServiceSetupDoesNotReadCA(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		Endpoint:       "127.0.0.1:4318",
		CAFile:         filepath.Join(t.TempDir(), "missing-ca"),
		Environment:    "test",
		LocalPlaintext: true,
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	if err := otelx.ValidateServiceSetup(cfg, res); err != nil {
		t.Fatal(err)
	}
}

func TestValidateServiceSetupDoesNotConnect(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("socket creation unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := otelx.ServiceConfig{
		Enabled:        true,
		Endpoint:       listener.Addr().String(),
		Environment:    "test",
		LocalPlaintext: true,
	}
	res := resource.NewSchemaless(attribute.String("service.name", "test"), attribute.String("service.version", "1"))
	if err := otelx.ValidateServiceSetup(cfg, res); err != nil {
		t.Fatal(err)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err == nil {
		conn.Close()
		t.Fatal("validation connected to collector")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("accept: %v", err)
	}
}
