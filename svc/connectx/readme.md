# Connect application errors

Convert an application failure at the RPC boundary and log its original cause:

```go
rpcErr := connectx.FromApplicationError(err, connectx.ErrorOptions{Message: "could not complete the request"})
logger.LogAttrs(
    ctx,
    level,
    "order RPC rejected",
    slog.String("operation", operation),
    slog.Any("error", err),
)
return rpcErr
```

The service owns severity, diagnostic context and redaction. Its logger must
redact sensitive values from errors without discarding useful causes. Apply
service panic, cancellation, deadline and authentication policies before mapping.

`FromApplicationError` selects the first application error in `errors.As` order.
`Code` maps Invalid to InvalidArgument, Forbidden to PermissionDenied, NotFound
to NotFound, Conflict to Aborted, Precondition to FailedPrecondition, and
Unavailable to Unavailable. Internal, unknown and unclassified failures become
Internal. The error must be non-nil.

For a common public message, `ErrorOptions.Message`, when nonempty, overrides the
public message and must be safe for clients. Otherwise a supported non-internal
error uses its nonempty application message, or its category name with underscores
replaced by spaces. Internal, unknown and unclassified errors use
`internal server error`.

The converter does not send the application code, violations, diagnostic cause or
typed details. The returned Connect error still unwraps to the original error for
`errors.Is` and `errors.As` on the server. Services that explicitly add details
with Connect's native `AddDetail` are responsible for their public content.
