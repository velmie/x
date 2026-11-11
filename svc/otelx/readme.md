# otelx

`otelx` assembles explicitly configured OpenTelemetry tracing and metrics providers. A `Runtime` owns the exporters and
SDK workers it creates, and its `Shutdown` joins their work. The module also provides bounded propagation, a bounded
HTTP instrumentation profile, and `slog` correlation.

## Service telemetry

Load `ServiceConfig` with confload, applying its tagged defaults and
`ServiceEnvPrefix` with `confload.WithSectionEnvPrefix`. Use
`confload.JSONStringMapHook` for the headers map. The canonical environment
names start with `OTELX_`. Supply an explicit resource with `service.name` and
`service.version`, then stop producers and scraping before shutdown:

```go
type Config struct {
    Telemetry otelx.ServiceConfig `k:"telemetry"`
}

loader := confload.New(
    confload.WithSectionEnvPrefix("telemetry", otelx.ServiceEnvPrefix),
    confload.WithDecodeHooks(confload.JSONStringMapHook()),
)
cfg, err := confload.LoadInto[Config](loader)
if err != nil {
    return err
}
res := resource.NewSchemaless(
    attribute.String("service.name", "orders"),
    attribute.String("service.version", "1.0.0"),
)
runtime, err := otelx.SetupService(ctx, cfg.Telemetry, res)
if err != nil {
    return err
}
runErr := runService(ctx, runtime)
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
return errors.Join(runErr, runtime.Shutdown(shutdownCtx))
```

The `batch_timeout` (default `100ms`) controls span batch delay.
`metric_interval` (default `1s`) controls periodic OTLP metric export and is
unused in Prometheus mode, where scrapes drive collection, and
`export_timeout` (default `1s`) bounds each span and metric export, the
collector HTTP client, and the gRPC exporter. Zero uses the default and
negative values are rejected.

`ValidateServiceConfig` reports only configuration fields, using relative
`k` paths in `*validation.Error`. When handling `SetupService` errors, prefix
those paths with the configuration section. Other errors matching
`otelx.ErrInvalidConfig` reject the section because of its resource, environment,
or other configuration. All other errors represent runtime failures:

```go
runtime, err := otelx.SetupService(ctx, cfg.Telemetry, res)
var failure *validation.Error
switch {
case errors.As(err, &failure):
	return validation.At("telemetry", failure)
case errors.Is(err, otelx.ErrInvalidConfig):
	return validation.Invalid("telemetry", apperror.RuleType, "", err)
case err != nil:
	return fmt.Errorf("telemetry setup: %w", err)
}
```

Enabled setup requires string attributes `service.name` and `service.version`, a
non-empty environment, and an unset or empty `OTEL_RESOURCE_ATTRIBUTES`.

`SetupService` owns its exporters, HTTP transport, and exclusive stdout output
file. It does not install global providers. Collector HTTP error bodies and gRPC
status messages are dropped because collectors may echo request headers. HTTP
redirects are rejected without exposing their targets. Use the collector's own
logs for those details. Partial-success messages in accepted responses are
reported as the collector sends them. The zero configuration disables
telemetry. When enabled, collector endpoints use `host:port`, TLS is verified,
and plaintext is allowed only for literal loopback addresses. `stdout` export
requires a new output file. Prometheus uses a dedicated registry exposed via
`Runtime.PrometheusHandler`.

## Traffic class and durable trace context

When `AllowBaggage` is enabled, the service profile accepts and emits only
`traffic.class=interactive` or `traffic.class=batch`. Use
`TrafficClassAttributes(ctx)` to add a permitted value to spans. Members with
properties or other values are discarded.

For durable records, store `Traceparent(ctx)` and restore it with
`ParseTraceparent`, regardless of the peer propagation format. The value holds
only W3C version-00 trace and span identity and the sampled flag. It does not
contain baggage or vendor state.

## Webhook egress

A webhook client can retain the queued trace as a local parent while preventing
trace propagation to the recipient. Use `otelhttpx.NewTransport` with
`propagation.NewCompositeTextMapPropagator()` and construct a fresh request with
fresh headers. A no-op propagator does not remove propagation headers already
present on a request. Validate any user-configured headers at the application
boundary. The runnable `ExampleNewTransport_webhookEgress` demonstrates the
recipient and local span behavior.

## Native customization with Open

Build `runtimeConfig` from service configuration, then open and stop the runtime:

```go
runtime, err := otelx.Open(ctx, runtimeConfig)
if err != nil {
    return err
}
runErr := runService(ctx, runtime)
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
return errors.Join(runErr, runtime.Shutdown(shutdownCtx))
```

`Shutdown` uses the caller's context, capped by `FlushTimeout`, for each preliminary flush. It then detaches
cancellation for final provider shutdown and waits for owned exporter work. This cooperative join can outlast the
context. Exporters and borrowed writers must eventually return. Stop and join telemetry producers and the scrape server
before calling it. Close borrowed files and transports afterwards. Do not shut down the exposed providers separately.
Concurrent and repeated calls wait for the same result.

For example, a service can assemble the configuration from an explicit resource, a propagation policy, and native
exporter factories:

```go
runtimeConfig := otelx.RuntimeConfig{
    Enabled:           cfg.Enabled,
    Resource:          serviceResource,
    Propagator:        policy,
    NewSpanExporter:   spanFactory,
    NewMetricExporter: metricFactory,
}
```

The native periodic reader can apply its own configured timeout to a final metric export. A timeout or another export
failure is returned; completion does not imply that the collector accepted every measurement. If final collection and
exporter shutdown return the same cause, the SDK does not always expose the provider error's precise substage. The
runtime retains it as `shutdown metrics` and separately retains `shutdown metric exporter`; a shared cause can appear
more than once. If a later setup step fails, `Open` cleans up exporters acquired by successful factories. Cleanup uses
detached cancellation and can therefore wait for an exporter to finish.

The zero `RuntimeConfig` disables telemetry. It returns no-op providers and bounded W3C propagation without acquiring
exporters, readers, registries, or workers. Unused configuration is not validated in disabled mode. When enabled,
`Resource` and at least one trace or metric pipeline are required. An omitted signal uses a no-op provider.
`NewMetricExporter` and `PrometheusRegistry` are mutually exclusive. Each successful factory returns a non-nil exporter
and transfers its ownership to the runtime. A factory that returns an error must release anything it acquired before
returning. `Open` ignores its exporter result.

Use `runtime.TracerProvider.Tracer(scope)`, `runtime.MeterProvider.Meter(scope)`, and `runtime.Propagator` when
assembling adapters. `Open` does not install globals. Applications that use global OTel APIs can explicitly call
`otel.SetTracerProvider`, `otel.SetMeterProvider`, and `otel.SetTextMapPropagator` at the executable boundary.

`Open` does not read environment variables directly. The SDK merges `OTEL_RESOURCE_ATTRIBUTES` and `OTEL_SERVICE_NAME`
into the supplied resource, with explicit resource attributes taking precedence on duplicate keys. Native SDK components
and exporter constructors may read other `OTEL_*` settings. A service that requires strict identity should validate its environment
before calling `Open` and configure native exporter options explicitly.

## Exporters, resources, and metrics

Export destinations, TLS, headers, timeouts, retries, compression, connections, and writers use native exporter
constructors and options. For example, an HTTPS trace factory can use:

```go
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
}
spanFactory := func(ctx context.Context) (sdktrace.SpanExporter, error) {
    return otlptracehttp.New(
        ctx,
        otlptracehttp.WithEndpointURL("https://collector.example:4318/v1/traces"),
        otlptracehttp.WithTLSClientConfig(tlsConfig),
        otlptracehttp.WithHeaders(map[string]string{"Authorization": authorizationValue}),
    )
}
```

For gRPC, use the native constructor and TLS credentials:

```go
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
}
spanFactory := func(ctx context.Context) (sdktrace.SpanExporter, error) {
    return otlptracegrpc.New(
        ctx,
        otlptracegrpc.WithEndpoint("collector.example:4317"),
        otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)),
        otlptracegrpc.WithHeaders(map[string]string{"authorization": authorizationValue}),
    )
}
```

A metric factory uses the corresponding native constructor:

```go
metricFactory := func(ctx context.Context) (sdkmetric.Exporter, error) {
    return otlpmetrichttp.New(
        ctx,
        otlpmetrichttp.WithEndpointURL("https://collector.example:4318/v1/metrics"),
        otlpmetrichttp.WithTLSClientConfig(tlsConfig),
    )
}
```

The service chooses and validates the endpoint, credentials, trust roots, transport, and retry policy. A custom exporter
implements the native SDK exporter interface and is returned by the corresponding factory. An explicit stdout writer
can be supplied with `stdouttrace.WithWriter`. Close a borrowed writer only after runtime shutdown.

Construct the resource with native attributes and optional detectors before `Open`. Pass a native `sdktrace.Sampler` to
`Sampler`, and native `sdkmetric.View` values to `Views`. The sampler defaults to
`sdktrace.ParentBased(sdktrace.AlwaysSample())`. `BatchOptions` and `ReaderOptions` accept native SDK tuning options.
The runtime creates no application-specific instruments.

For Prometheus, use a dedicated registry and serve the returned handler on an application-owned server:

```go
registry := prometheus.NewRegistry()
runtime, err := otelx.Open(
    ctx,
    otelx.RuntimeConfig{
        Enabled:            true,
        Resource:           serviceResource,
        PrometheusRegistry: registry,
    },
)
if err != nil {
    return err
}
```

The application may register Go or process collectors on that registry before opening the runtime. The runtime does not
register with Prometheus globals or start a listener. The registry belongs to this runtime and must not be shared with
another runtime. `runtime.PrometheusHandler` is nil when Prometheus is not selected.

## Bounded propagation

The zero `PropagationPolicy` accepts and emits W3C trace context without baggage. `FormatW3C`, `FormatB3Single`, and
`FormatB3Multi` select one encoding. An unknown format or invalid limits and rules return an error matching
`otelx.ErrInvalidConfig`.

```go
policy, err := otelx.NewPropagation(
    otelx.PropagationPolicy{
        Format: otelx.FormatW3C,
        Baggage: []otelx.BaggageRule{
            {
                Key:    "traffic.class",
                Values: []string{"interactive", "batch"},
            },
        },
    },
)
if err != nil {
    return err
}
```

Default limits are 512 bytes per trace field, 1024 bytes for the baggage header, and 16 baggage members. The complete
set of permitted key/value combinations must fit the configured bounds and the native SDK's baggage limits. Rules are
copied at construction. Values are decoded application values; serialization uses the native baggage format.

Extraction rejects mixed recognized trace encodings, duplicate case-insensitive aliases, repeated values, and fields
exceeding the bound. W3C input must use the fixed lowercase version-00 representation. Inherited span context, vendor tracestate, and
baggage are cleared while cancellation, deadlines, and ordinary context values remain. Baggage is evaluated
independently: ambiguous or oversized baggage is discarded, unknown keys are dropped, and permitted members with
properties are rejected. B3 debug is normalized to ordinary sampling state.

`FilterContext` removes vendor state and unapproved baggage while preserving trace and span identity and whether the
parent is local or remote. Injection applies the same bounds. It removes stale recognized fields from native
`propagation.HeaderCarrier` and `propagation.MapCarrier`. Other `TextMapCarrier` implementations must supply fresh
propagation fields because the native interface has no delete operation. Carriers that can hold repeated values should
implement `propagation.ValuesGetter`, so extraction can detect ambiguity.

A custom native propagator passed to `RuntimeConfig` or the HTTP profile owns its own propagation policy. Legacy
mixed-format deployments can continue using their existing native composite propagator until they migrate deliberately.

## HTTP instrumentation

`otelhttpx.NewHandler` wraps one selected finite route. Pass fixed operation and route labels, not values taken from a
request:

```go
handler, err := otelhttpx.NewHandler(
    next,
    otelhttpx.HandlerConfig{
        Operation:  "books.get",
        Route:      "/books/{id}",
        Tracer:     runtime.TracerProvider.Tracer("books"),
        Meter:      runtime.MeterProvider.Meter("books"),
        Propagator: runtime.Propagator,
    },
)
if err != nil {
    return err
}
```

Install it outside recovery to observe aborted delivery, and omit operational routes in the application router. The
handler uses `response.StatusRecorder` from `svc/http`. Use `http.ResponseController` for supported writer operations
such as flushing.

`otelhttpx.NewTransport` measures one `RoundTrip` through response headers or an error:

```go
transport, err := otelhttpx.NewTransport(
    baseTransport,
    otelhttpx.TransportConfig{
        Operation:  "peer.fetch",
        Tracer:     runtime.TracerProvider.Tracer("client"),
        Meter:      runtime.MeterProvider.Meter("client"),
        Propagator: runtime.Propagator,
    },
)
if err != nil {
    return err
}
```

The transport clones request headers, leaves the caller's request unchanged, and returns the original response body and
error. The span ends before the body is consumed. The caller owns the body, base transport, TLS, and retries. An attempt
means one invocation of `RoundTrip`, not retries inside the base transport. With `*otelx.Propagation`, the parent is
filtered before starting the child span.

The profile creates only these instruments:

| Instrument | Boundary |
| --- | --- |
| `http.server.requests` | One selected handler invocation, including an abort |
| `http.server.request.duration` | Handler entry through return or unwind, in seconds |
| `http.client.requests` | One `RoundTrip` invocation |
| `http.client.request.duration` | `RoundTrip` entry through response headers or error, in seconds |

Attributes are the fixed operation and route, normalized method, observed status, and bounded failure type where
applicable. A transport error uses `transport_failed`; HTTP status 400 or higher sets client span status `remote_error`.
A server status 500 or higher sets `server_error`; ordinary 4xx does not. An interrupted handler uses `handler_aborted`.
Before a final response status exists, the recorded status is zero. Raw URLs, headers, request bodies, and error
messages are not recorded by this profile. Connect-specific outcome annotation remains in the service.

Required dependencies and labels are validated with errors matching `otelx.ErrInvalidConfig`; instrument-construction
errors retain their causes. Do not apply both native and bounded instrumentation to the same request. Existing native
`otelhttp` instrumentation retains its native timing and attribute semantics.

## `slog` correlation and SDK diagnostics

Wrap the base handler before deriving loggers with attributes or groups:

```go
handler := otelslog.NewHandler(slog.NewJSONHandler(os.Stderr, handlerOptions))
logger := slog.New(handler).WithGroup("request")
```

The decorator removes caller-supplied `trace_id` and `span_id` attributes case-insensitively, including nested groups
and resolved `LogValuer` values. For a complete valid span context, including an unsampled one, it adds canonical IDs in
the active group. Reserved group names are ignored. A decorator cannot remove attributes already bound inside an opaque
handler. IDs are correlation metadata, not authenticated identity. Configure secret-key redaction on the underlying
handler, for example through `slog.HandlerOptions.ReplaceAttr`.

Synchronous setup and shutdown failures are returned to the caller. To handle asynchronous SDK failures, explicitly
install a handler at the executable boundary:

```go
otel.SetErrorHandler(otelx.NewErrorHandler(logger))
```

The handler passes the original error to the logger without rewriting it. Configure the service's logging handler to
redact well-known secret keys, for example with `slogx`. A service that needs a different summary can provide its own
error handler. In this example, `summarize` is a service-owned function that produces the desired summary:

```go
// summarize is defined by the service.
handler := otel.ErrorHandlerFunc(func(err error) {
    logger.Error("telemetry failure", slog.Any("error", summarize(err)))
})
otel.SetErrorHandler(handler)
```

## Legacy setup and migration

`Config`, `DefaultConfig`, `ConfigFromEnv`, `Setup`, `SetupResult`, legacy factories/options, and existing `otelhttpx`
hooks remain available. `Setup` and named detector registration are deprecated in favor of constructing an explicit
resource and using `Open`. Legacy `Setup` still installs global tracing and propagation. Its defaults still select gRPC,
plaintext, W3C plus B3 single-header propagation, and baggage. A dependency update alone does not add metrics or change
those defaults.

The compatibility fixes retain exporter-construction causes, clean up an acquired exporter if setup later fails, avoid
constructing a real exporter when disabled, and send the configured gRPC authorization header. Returned setup errors can
include both the primary failure and a cleanup failure.

`ConfigFromEnv` continues to support the existing `TRACING_*` variables and the listed OTel overrides. For a pair below,
the OTel variable takes precedence:

- `OTEL_SDK_DISABLED` / `TRACING_DISABLED`;
- `OTEL_SERVICE_NAME` / `TRACING_RESOURCE_SERVICE_NAME`;
- `OTEL_RESOURCE_ATTRIBUTES` / `TRACING_RESOURCE_ATTRIBUTES`;
- `OTEL_TRACES_SAMPLER_ARG` / `TRACING_SAMPLING_RATIO`;
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, then `OTEL_EXPORTER_OTLP_ENDPOINT`, then `TRACING_COMMUNICATION_ENDPOINT`;
- `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`, then `OTEL_EXPORTER_OTLP_PROTOCOL`, then `TRACING_COMMUNICATION_EXPORT_METHOD`.

The legacy export-method values are `grpc`, `http`, and `stdout`. Other legacy settings are `TRACING_SECURITY_INSECURE`,
`TRACING_SECURITY_AUTHORIZATION_HEADER`, `TRACING_RESOURCE_DEPLOYMENT_ENVIRONMENT`, `TRACING_RESOURCE_DETECTORS`,
`TRACING_PROPAGATION_BAGGAGE`, `TRACING_PROPAGATION_TRACE_CONTEXT`, `TRACING_PROPAGATION_B3_SINGLE_HEADER`, and
`TRACING_PROPAGATION_B3_MULTIPLE_HEADER`.

Moving to `Open` requires an explicit resource, exporter factories, propagation choice, and a runtime shutdown after
application work has joined. Existing files, transports, exposure policy, service identity, baggage vocabulary, route
names, and business metrics stay with the service. See the executable examples for the
[runtime](runtime_example_test.go), [propagation](propagation_example_test.go),
[HTTP adapters](otelhttpx/profile_example_test.go), and [`slog` correlation](otelslog/example_test.go). The runtime
examples include a custom exporter, a native view, and a Prometheus registry.
