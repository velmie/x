# Bootstrap

`bootstrap` starts registered services and coordinates their graceful shutdown.
Services own their resources and the work needed to stop them.

## Usage

Create an orchestrator and register services before calling `Serve`:

```go
orc := bootstrap.NewOrchestrator(
    bootstrap.WithStopSignals(syscall.SIGINT, syscall.SIGTERM),
    bootstrap.WithShutdownTimeout(5*time.Second),
)

srv := &http.Server{Addr: ":8080", Handler: handler}
orc.Register(bootstrap.NewServerWrapper(srv))

if err := orc.Serve(); err != nil {
    // Inspect the startup and shutdown causes with errors.Is or errors.As.
}
```

The options are:

- `WithStopSignals`: signals that request shutdown. The defaults are `SIGINT`
  and `SIGTERM`. An empty argument list preserves those defaults.
- `WithShutdownTimeout`: timeout for each service's `Stop` call. There is no
  default timeout. Nonpositive values leave the timeout unchanged.
- `WithLogger`: a logger implementing `bootstrap.Logger`. The default is
  `NewNoopLogger()`, which produces no output. A supplied logger must support
  concurrent calls. Failure logs contain stage, reason, outcome, the zero-based
  registration index in `service_index`, and the original error object in `error`.
  The supplied logger owns redaction of sensitive application data before output.
  Preserve useful cause text and the service index when redacting values.

## Service ownership

A service implements:

```go
type Service interface {
    Start() error
    Stop(ctx context.Context) error
}
```

`Start` runs in its own goroutine. `Stop` must request termination and wait for
that service's work to finish. The methods can overlap, including when shutdown
is requested before startup completes. Services must handle that overlap.

Existing functions can be registered with `ServiceFunc`:

```go
orc.Register(bootstrap.ServiceFunc(start, stop))
```

The supplied `stop` function has the same ownership contract. It must stop and
wait for the work started by `start`, honoring its shutdown context. There is no
dependency ordering between services. Stops run concurrently, each with its own
timeout context when configured. A timeout is cooperative: the orchestrator
cannot force a `Stop` implementation that ignores its context to return.

## Requesting and observing shutdown

`Serve` blocks until a configured signal, a call to `Orchestrator.Stop`, or the
first observed startup error requests shutdown. It then waits for every
registered service's `Stop` call to return. Only one `Serve` call may be active
on an orchestrator. Register services before starting it.

`Orchestrator.Stop()` is an idempotent, nonblocking request. Repeated and
concurrent calls are safe before, during, and after `Serve`. A request made
before `Serve` is retained. Wait for `Serve` to return to observe the result of
shutdown. The orchestrator does not independently wait for arbitrary `Start`
goroutines after their service's `Stop` has returned.

`Serve` returns the first observed startup error together with all shutdown
errors. Use `errors.Is` or `errors.As` to inspect their causes. A sole startup
error is returned unchanged. Shutdown errors identify the service by its
zero-based registration index. If startup and shutdown succeed, `Serve` returns
`nil`. With no registered services, it returns `ErrNoRegisteredServices`.

If another `Start` call fails after shutdown has begun, its error and service
index are still logged. Such failures do not replace the first startup error or
extend the wait for shutdown. A cancellation or `http.ErrServerClosed` returned
by `Start` during shutdown is logged at info level with `reason=service_stopped`.

Returning shutdown errors is a change in public behavior: `Serve` can now return
an error after a successful startup and an explicit stop request or signal.
Previously these shutdown errors were only logged. Public method signatures and
interfaces remain unchanged.
