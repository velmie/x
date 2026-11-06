# HTTP service components

## Common path

Given a service-owned database pool, logger, mux, listener, and shutdown
context, use the database's native method directly as a readiness probe:

```go
databaseProbe := health.Probe{
    Name: "database",
    Check: db.PingContext,
}
mux.Handle("/readyz", health.NewHandler(logger, health.HandlerOptions{}, databaseProbe))
srv := &http.Server{
    Handler: server.Recover(logger, mux, server.RecoveryOptions{}),
    ReadHeaderTimeout: 5 * time.Second,
    ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
}
return server.Serve(ctx, srv, listener, drainTimeout)
```

The caller logs a returned lifecycle failure once and closes dependencies after
`Serve` returns. No additional server shutdown call is needed. A protocol-specific
panic response can be supplied through `server.RecoveryOptions.Responder`.

For business routes, compose middleware in this order:

**operation context -> telemetry -> access log -> recovery -> authentication -> endpoint**

An outer recovery around the router handles operational and routing failures.
Keep operational routes outside business access logs and tracing. The service
supplies any safe access attributes, including attributes derived from its own
trusted-proxy policy.

Use [requestauth](requestauth/readme.md) to authenticate protected HTTP routes
before reading a request body and pass the established principal to an operation.
The service owns key-source lifecycle and application authorization.

## Request

Use [request](request/readme.md) to resolve an optional page and limit into a
bounded collection window. The service owns query decoding and filter mapping.

## Response

The `response` package provides organization response envelopes and an
application-error mapper. `response.NewStatusRecorder(w)` observes the final
status without buffering the response body. It forwards informational responses,
records an implicit 200 on writing or flushing, and exposes the underlying writer
to `http.ResponseController`. A recorder is request-local and must not be used
for concurrent writes. See [response](response/readme.md) for error envelopes.

## Server

`server.Serve` owns an unused `http.Server` and listener. Configure native
server settings and create the listener in the service, then call `Serve` with
its shutdown context. It drains active requests and joins their handlers even
after the drain deadline. Handlers must finish after their request contexts are
canceled and join their own work. Detached workers, hijacked connections, and
shutdown hooks stay with their owners. `Serve` does not attach its context to
`BaseContext`, since that would cancel active requests before graceful draining.
Do not place middleware that returns while its inner handler continues, including
`http.TimeoutHandler`, inside this cooperative joining guarantee.

`server.Recover` writes the standard internal-error envelope before commitment
or aborts committed delivery. An optional responder handles another protocol.
`server.AccessLog` records a bounded method, status, duration, and outcome. Its
optional attributes must be bounded, safe, and separate from reserved log keys.
Use `server.WithOperation` for a stable application-owned name. The logging
adapter must redact sensitive values in panic objects, errors, and attributes.

## Health

`health.Check` samples selected probes sequentially, retains their diagnostic
causes, and returns safe public reason codes. It continues after a probe failure
unless the parent context has ended. Zero probe and handler timeouts each select
their own 500 ms default. An executing probe must return after its context ends,
including its own cleanup. The deadline does not abandon a running probe.

`health.NewHandler` runs the sample for both GET and HEAD. It returns 200 when
all probes pass and 503 otherwise. HEAD has no body. Other methods receive 405
without running probes. Responses use the organization envelope, JSON, and
`Cache-Control: no-store`. An empty selection is ready with `components: []`.

Probe names and custom reasons must be stable, bounded, and safe for publication.
The handler logs original failure causes once per failed component. The service's
logging adapter owns redaction of sensitive values in those causes. A probe panic
propagates to surrounding recovery middleware.
