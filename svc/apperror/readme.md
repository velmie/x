# Application errors

Use an immutable sentinel for a reusable application failure and wrap it to add
operation context:

```go
const CodeBookNotFound = "BOOK_NOT_FOUND"

var ErrBookNotFound = &apperror.Error{
    Category: apperror.NotFound,
    Code:     CodeBookNotFound,
    Message:  "book not found",
}

err := fmt.Errorf("load book: %w", ErrBookNotFound)
```

`errors.Is(err, ErrBookNotFound)` is true. A transport can expose `book not
found`, while diagnostics retain `load book`. Matching uses sentinel identity,
not the code. Do not mutate an error or its violation slice after publication.

For per-occurrence public fields or wording, construct a fresh error and retain
any sentinel or typed domain error as its cause:

```go
err := &apperror.Error{
    Category: apperror.Invalid,
    Code:     CodeInvalidRequest,
    Message:  "request fields are invalid",
    Violations: []apperror.Violation{
        {
            Field: "lines[0].quantity",
            Rule:  apperror.RuleGTE,
            Param: "1",
        },
    },
    Cause: validationErr,
}
```

A violation has a public DTO path, rule, optional parameter and optional public
message. It has no rejected-value field. Keep field names, parameters and
messages safe for clients. Feature-specific codes and rules remain with their
owner. A non-internal error requires a nonempty code. The zero error is Internal.

## Classification

Use `errors.As` to find the first `*apperror.Error`: traversal is preorder,
depth first. An outer classification wins, and `errors.Join(a, b)` visits `a`
before `b`. Unknown and Internal categories, and unclassified errors, map to a
generic internal response. Transports must not expose `Error()` or `Cause`.
They may expose explicitly public fields for supported non-internal categories.
Diagnostic loggers retain the original error and must redact sensitive values.

Services using `sqltx` must check the entire error tree for a panic before
category or cancellation inspection. For example, before `FromApplicationError`:

```go
var panicErr *sqltx.PanicError
if errors.As(err, &panicErr) {
	err = normalizePanic(err)
}
```

The service's `normalizePanic` wraps the original error in an outer Internal
`apperror.Error`. Use the same normalization for HTTP, RPC and messaging.
