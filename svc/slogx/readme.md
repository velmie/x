# slogx

Package slogx provides attribute-key redaction for the standard library's `log/slog` JSON and text handlers.
The service owns logger construction, output, levels, formats, and trace correlation.

## Usage

```go
package main

import (
    "log/slog"
    "os"

    "github.com/velmie/x/svc/slogx"
)

func main() {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level:       slog.LevelInfo,
        ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{}),
    })
    logger := slog.New(handler)
    logger.Info("request", slog.String("authorization", "Bearer example"))
}
```

Zero options use the default keys returned by `DefaultSecretKeys`.

## Add service keys

Add exact keys or a predicate to the defaults:

```go
redact := slogx.NewRedactor(slogx.RedactorOptions{
    AdditionalKeys: []string{"private_id"},
    MatchKey: func(key string) bool {
        return strings.HasSuffix(key, "_token")
    },
})
```

This example uses `strings.HasSuffix`. Matches are case-insensitive. The predicate receives a lower-case key and
must be safe for concurrent calls. Options cannot disable defaults, and the key slice is copied at construction.

## What is redacted

A matching attribute key or enclosing group name replaces the attribute value with `[REDACTED]`, retaining the
key and group structure. Built-in slog attributes also pass through `ReplaceAttr`. Empty-key attributes are
preserved, so an attribute discarded by another rewriter cannot be restored.

## What is not redacted

The redactor does not inspect values, messages, error text, or struct fields inside `slog.Any`. Callers must keep
messages, attribute names, and unstructured values safe. An adapter that calls a dependency must summarize errors
that may carry secrets before logging them.

## Combine with another ReplaceAttr

Call both rewriters explicitly, preserving attributes discarded by the first:

```go
redact := slogx.NewRedactor(slogx.RedactorOptions{})
replace := func(groups []string, a slog.Attr) slog.Attr {
    a = renameSource(groups, a) // service's own rewriter
    if a.Key == "" {
        return a
    }
    return redact(groups, a)
}
```

Use `replace` as the handler's `ReplaceAttr`.

## Compatibility

The module requires Go 1.21 or newer and has no dependencies outside the standard library. It starts no goroutines,
performs no I/O, changes no globals, and requires no cleanup. Adding a default key is a documented release change.

Use `NewHandler` to select the standard JSON or text handler and secret-key
redaction from configuration. Empty settings select JSON and info. Levels are
debug, info, warn, and error. Formats are JSON and text. Both are case-insensitive.
The validation tags use `oneofci`. Services using go-playground/validator to
validate `Config` need a version that supports it. It is absent in v10.22.1 and
present in v10.27.0. With this release, services do not need to change values
before validation. The handler can be composed with other `slog.Handler` implementations by the application.
See `ExampleNewHandler` for a runnable example.
