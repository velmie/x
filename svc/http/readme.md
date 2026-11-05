# HTTP service components

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
its shutdown context. Log a returned failure once and close dependencies only
after `Serve` returns. It drains active requests and joins their handlers even
after the drain deadline. Handlers must finish after their request contexts are
canceled and join their own work. Detached workers, hijacked connections, and
shutdown hooks stay with their owners.

`server.Recover` writes the standard internal-error envelope before commitment
or aborts committed delivery. An optional responder handles another protocol.
`server.AccessLog` records a bounded method, status, duration, and outcome. Its
optional attributes must be bounded, safe, and separate from reserved log keys.
Use `server.WithOperation` for a stable application-owned name. The logging
adapter must redact sensitive values in panic objects, errors, and attributes.
