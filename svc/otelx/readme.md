# otelx

This package simplifies the process of configuring and initializing OpenTelemetry tracing.

## Environment variables

It includes a function for reading configuration values from environment variables. Below is a table describing the
available environment variables:

| Variable                                | OTEL Standard Equivalent                | Description                                                   | Default Value |
|-----------------------------------------|-----------------------------------------|---------------------------------------------------------------|---------------|
| TRACING_DISABLED                        | OTEL_SDK_DISABLED                       | Disables tracing by setting noop exporter                     | false         |
| TRACING_PROPAGATION_BAGGAGE             | -                                       | Enables or disables baggage propagation                       | true          |
| TRACING_PROPAGATION_TRACE_CONTEXT       | -                                       | Enables or disables trace context propagation                 | true          |
| TRACING_PROPAGATION_B3_MULTIPLE_HEADER  | -                                       | Enables or disables B3 multiple header propagation            | false         |
| TRACING_PROPAGATION_B3_SINGLE_HEADER    | -                                       | Enables or disables B3 single header propagation              | true          |
| TRACING_SAMPLING_RATIO                  | OTEL_TRACES_SAMPLER_ARG                 | Sets the sampling ratio for traces                            | 1             |
| TRACING_RESOURCE_ATTRIBUTES             | OTEL_RESOURCE_ATTRIBUTES                | Sets resource attributes                                      | none          |
| TRACING_RESOURCE_DETECTORS              | -                                       | List of resource detectors to use                             | none          |
| TRACING_RESOURCE_SERVICE_NAME           | OTEL_SERVICE_NAME                       | Sets the service name for the resource                        | none          |
| TRACING_RESOURCE_DEPLOYMENT_ENVIRONMENT | -                                       | Sets the deployment environment for the resource              | none          |
| TRACING_SECURITY_AUTHORIZATION_HEADER   | -                                       | Sets the authorization header for security                    | none          |
| TRACING_SECURITY_INSECURE               | -                                       | Sets the security mode (insecure or not)                      | true          |
| TRACING_COMMUNICATION_ENDPOINT          | OTEL_EXPORTER_OTLP_TRACES_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT | Sets the endpoint for communication                           | none          |
| TRACING_COMMUNICATION_EXPORT_METHOD     | OTEL_EXPORTER_OTLP_TRACES_PROTOCOL or OTEL_EXPORTER_OTLP_PROTOCOL | Sets the export method for communication (grpc, http, stdout) | grpc          |

The package supports both legacy TRACING_* variables and standard OpenTelemetry OTEL_* variables. When both are set, OTEL_* variables take precedence.

TRACING_RESOURCE_ATTRIBUTES can be extended with other environment variables:

TRACING_RESOURCE_ATTRIBUTES=attr.key=val,from.another.var=$ANOTHER_VAR

## Usage example

```go
    cfg, err := otelx.ConfigFromEnv()

// optionally rewrite parameters
cfg.Resource.ServiceName = "My Service"
cfg.Communication.ExportMethod = otelx.ExportMethodGRPC

// this will create and set tracer provider, exporter and propagators based on the given configuration
// every component could be configured or overridden by specifying options
telemetry, err := otelx.Setup(
context.Background(),
cfg,
otelx.WithResourceAttributes(attribute.String("my.custom.param", "test")),
)

if err != nil {
fmt.Println("failed to setup OpenTelemetry: ", err)
return
}
// ...
```

You can use custom detectors. For this, they need to be registered in advance using the otelx.RegisterDetector function.

There are three ways to add detectors:

* Through the TRACING_RESOURCE_DETECTORS environment variable, where the detector names should be listed separated by
  commas.
* You can add the detector name after loading the configuration.
* You can directly set a detector instance using the WithResourceDetectors option.

```go
package example

import (
	"context"
	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

type MyCustomDetector struct{}

func (m MyCustomDetector) Detect(ctx context.Context) (*resource.Resource, error) {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		attribute.String("my.custom.attribute1", "hello"),
		attribute.Int("my.custom.attribute2", 42),
	), nil
}

// ... 

func main() {
	cfg, err := otelx.ConfigFromEnv()

	otelx.RegisterDetector("my.custom_detector", &MyCustomDetector{})

	// it's possible to specify directly which detectors to use
	cfg.Resource.Detectors = append(cfg.Resource.Detectors, "my.custom_detector")

	otelx.Setup(context.Background(), cfg) // alternatively you may specify it directly using otelx.WithResourceDetectors(&MyCustomDetector{})
}
```