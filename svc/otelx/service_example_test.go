package otelx_test

import (
	"context"
	"fmt"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func ExampleSetupService() {
	// A configuration loader can apply the defaults in ServiceConfig's tags.
	cfg := otelx.ServiceConfig{}
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
