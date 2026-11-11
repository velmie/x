package otelx_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/velmie/x/svc/confload"
	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

func ExampleSetupService() {
	// A configuration loader can apply the defaults in ServiceConfig's tags.
	dir, err := os.MkdirTemp("", "otelx-example-")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			panic(err)
		}
	}()
	cfg := otelx.ServiceConfig{
		Enabled:       true,
		TraceProtocol: "stdout",
		StdoutFile:    filepath.Join(dir, "traces.json"),
		MetricsMode:   "prometheus",
		SampleRatio:   1,
		Environment:   "local",
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", "orders"),
		attribute.String("service.version", "1.0.0"),
	)
	runtime, err := otelx.SetupService(context.Background(), cfg, res)
	if err != nil {
		panic(err)
	}
	// Stop and join producers and the scrape server before shutdown.
	if err := runtime.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("telemetry stopped")
	// Output: telemetry stopped
}

func ExampleTraceparent() {
	stored := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-00"
	parent, ok := otelx.ParseTraceparent(stored)
	if !ok {
		panic("invalid stored parent")
	}
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	fmt.Println(otelx.Traceparent(ctx))
	// Output: 00-0123456789abcdef0123456789abcdef-0123456789abcdef-00
}

func ExampleTrafficClassAttributes() {
	member, err := baggage.NewMember(otelx.TrafficClassKey, otelx.TrafficClassBatch)
	if err != nil {
		panic(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		panic(err)
	}
	ctx := baggage.ContextWithBaggage(context.Background(), bag)
	attrs := otelx.TrafficClassAttributes(ctx)
	fmt.Println(attrs[0].Key, attrs[0].Value.AsString())
	// Output: traffic.class batch
}

func ExampleServiceEnvPrefix() {
	loader := confload.New(confload.WithSectionEnvPrefix("telemetry", otelx.ServiceEnvPrefix))
	fmt.Println(loader.EnvName("telemetry.headers.authorization"))
	// Output: OTELX_HEADERS_AUTHORIZATION
}

func ExampleValidateServiceConfig() {
	cfg := otelx.ServiceConfig{}
	fmt.Println(otelx.ValidateServiceConfig(cfg))
	// Output: <nil>
}
