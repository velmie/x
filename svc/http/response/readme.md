# HTTP response envelopes

Map an application failure at the HTTP boundary and log the original error once,
including any failure writing the response:

```go
status, body := response.FromApplicationError(err, response.ErrorOptions{})
writeErr := response.WriteErrors(w, r, status, body)
logger.LogAttrs(
    r.Context(),
    level,
    "request failed",
    slog.String("operation", operation),
    slog.Any("error", errors.Join(err, writeErr)),
)
```

The service owns severity, diagnostic context and redaction. The logger must
redact sensitive values from errors without discarding useful causes. Apply the
service's panic, cancellation, authentication and retry policies before mapping.

`FromApplicationError` selects the first application error in `errors.As` order.
It maps Invalid to 400, Forbidden to 403, NotFound to 404, Conflict and
Precondition to 409, and Unavailable to 503. Internal, unknown and unclassified
errors become 500 with code `INTERNAL_SERVER_ERROR`.

For a common public message, `ErrorOptions.Message`, when nonempty, overrides the
public message and must be safe for clients. Otherwise a supported non-internal
error uses its nonempty application message, or its category name with underscores
replaced by spaces. Internal, unknown and unclassified errors use
`internal server error`.

A non-internal error without violations becomes one common entry with its public
code and message. Violations instead become field entries, in order:
the uppercase rule is the code, the field is the source, and a nonempty parameter
is stored under its rule name in `meta`. Optional violation messages become
titles. The application owns these public fields. Internal and unknown errors
never expose their code, message, violations or cause.

`WriteErrors` sets `Content-Type: application/json` and sends the supplied status.
It omits the body for HEAD and returns encoding and write failures. Headers and
status may already have been sent when encoding or writing fails. It does not log
or retry. Services can construct `response.Errors` and call `WriteErrors` directly
for HTTP-specific contracts such as conditional requests.
