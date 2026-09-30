# Authentication Package

This package provides a solution for JWT (JSON Web Tokens) authentication and validation, supporting
multiple JWT signing methods and allowing for flexible authentication mechanisms.

## Features:

- Integration with JWKS (JSON Web Key Set) for public key fetching and caching.
- Rate limiting and retry mechanisms for JWKS server requests.
- Fallback and non-blocking mechanisms for key sources.

## Basic Usage

### With JWKS source

```go
import (
"context"
"fmt"
"github.com/velmie/x/svc/authx"
"net/url"
)

func main() {
jwksURL, _ := url.Parse("https://example.com/.well-known/jwks.json")
auth, err := authentication.NewJWTMethod(
authentication.WithJWKSSource(jwksURL),
)

entity, err := auth.Authenticate(context.Background(), "some-jwt")
if err != nil {
// ...
}

fmt.Println(entity) // map[string]any filled with JWT claims
}
```

### With a given public key

```go
    var pubKey crypto.PublicKey

// init pubKey code...

method, err := authentication.NewJWTMethod(
authentication.WithJWTPublicKey(pubKey),
)
// ...
```

### JWKS wait ready

```go
    jwksURL, _ := url.Parse("https://example.com/.well-known/jwks.json")

ready := make(chan struct{})

auth, err := authentication.NewJWTMethod(
authentication.WithJWKSSource(jwksURL),
authentication.WithJWKSSourceReadySignal(ready),
)

select {
case <-ready:
case <-time.After(30 * time.Second):
// timeout error
}

entity, err := auth.Authenticate(context.Background(), "some-jwt")
if err != nil {
// ...
}

fmt.Println(entity) // map[string]any filled with JWT claims
```

### Fallback

If 2 key sources are used at once (JWKS and the given key), then JWKS has priority, and if the key cannot be found, then
the given key is used.

See `options.go` for available options.

### Error adaptation

`ErrorAdapter` preserves underlying authentication, JWT, and key-source errors
for `errors.Is` and `errors.As` while retaining its `ErrBadToken` and
`ErrNotAuthenticated` categories. A missing key maps to `ErrNotAuthenticated`.
With the corrected authentication parser, claims validation failures such as
expiration also map to `ErrNotAuthenticated`. Malformed tokens, signature
failures, and other key-source failures map to `ErrBadToken`; the original cause
can still distinguish dependency failures and cancellation. A context canceled
before authentication begins is returned directly.

JWKS diagnostics use the configured logger with operation, stage, reason,
outcome, HTTP status, source, and affected field where available. The `error`
field contains the original error object. A JWKS diagnostic also supplies its
underlying errors in `cause`, including secondary response-close failures.
Fallback messages retain the error and identify the primary and fallback sources.
The supplied logger must support concurrent calls and redact sensitive values
in error chains before output. Preserve useful cause messages and field paths.
The adapter does not add token, key ID, URL, or response-body fields. Upstream
errors can contain credentials or URLs, so a logger that prints them verbatim is
not suitable for sensitive inputs. The HTTP retry client's own logging is disabled.

### Signing algorithm allowlist

`WithJWTSigningMethods` restricts authentication to the configured algorithms,
including when multiple algorithms can use the same public key. For example,
allowing only `RS256` rejects a `PS256` token even with a valid signature from the
same RSA key. Omitting the option preserves the default algorithm list. An empty
list remains invalid. This corrects earlier behavior that ignored the configured
list and always used the defaults.

### Managed JWKS startup

`NewManagedJWTMethod` returns a ready authentication method after the first JWKS
load succeeds, or returns the startup error. Its context owns the method's
lifetime. Cancel it during application shutdown to stop refreshes and reject
subsequent key lookups, including cached keys.

```go
lifetime, stop := context.WithCancel(context.Background())
defer stop()

sourceOptions := authentication.JWKSOptions{
    Client: &http.Client{Timeout: 5 * time.Second},
    RefreshInterval: time.Minute,
    MaxResponseBytes: 1 << 20,
    MaxCacheAge: 10 * time.Minute,
    RequestOnUnknownKID: true,
    ReserveRefreshCapacity: true,
}
sourceOptions.SetRefreshRateLimit(5, time.Minute)
method, err := authx.NewManagedJWTMethod(lifetime, authx.ManagedJWTMethodOptions{
    Endpoint: "https://issuer.example/jwks",
    ValidSigningMethods: []authx.JWTSigningMethod{authx.JWTSigningMethodRS256},
    JWKSOptions: sourceOptions,
})
if err != nil {
    return err
}
// Install method in the application's authentication boundary.
```

The example requires `context`, `net/http`, `time`, `authentication`, and `authx`
imports. Choose limits for the issuer and application. The managed path uses the
supplied HTTP client directly and does not add retries. Configure retry behavior
in that client when needed. Source callbacks take precedence over `Log`.
`JWKSOptions.DiagnosticFunc` receives a typed `*authentication.JWKSError` and takes
precedence over `WarnFunc`. When either callback is supplied, automatic JWKS
logging is disabled. With neither supplied, `Log` receives one structured warning
per event. The existing `WarnFunc` remains available for safe text diagnostics.

`JWKSError` exposes `Operation`, `Stage`, `Reason`, `Outcome`, `HTTPStatus`, and `Field`
getters. Underlying errors remain available through `errors.Is` and `errors.As`,
including response-close failures. A close failure after a valid response is a
warning and does not prevent startup. The logging adapter receives these
original error objects and owns redaction before serialization. Shutdown waits
for the selected callback to finish.
A nil signing-method list uses the existing defaults. An explicitly empty list
is rejected.

`NewJWTMethod` retains asynchronous best-effort initialization and its existing
fallback behavior. Its `SourceReady` signal means initialization has finished,
including an unsuccessful initial load. It does not report startup success.

### Waiting for managed shutdown

Use `NewManagedJWTMethodWithShutdown` when the application must wait for JWKS
work to finish. It accepts the same options and returns `(method, shutdown, err)`.
The shutdown function is available whenever a source was created, including an
initial-load failure. Configuration errors before source creation return a nil
shutdown function.

```go
method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(lifetime, opts)
if shutdown != nil {
    defer func() {
        cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := shutdown(cleanup); err != nil {
            // Report incomplete cleanup at the application boundary.
        }
    }()
}
if err != nil {
    return err
}
// Install method in the application's authentication boundary.
```

A successful shutdown waits for owned refresh work, response cleanup, and
callbacks, and prevents subsequent key lookups. A deadline limits the wait. If
it expires, the source remains stopped and shutdown can be called again to wait
for completion. Call shutdown outside diagnostic callbacks. It does not wait for
application request handlers or close the supplied HTTP client as a whole.
`NewManagedJWTMethod` retains its existing signature and cancellation behavior.

### Key freshness and fallback

`JWKSOptions.MaxCacheAge` limits the time since the last successfully validated
JWKS response. Failed refreshes do not extend that time. At the configured age,
authentication returns an error matching `authentication.ErrKeySetExpired`,
including through `ErrorAdapter`. Background refresh restores availability after
a successful response. Zero leaves the age unlimited.

The managed method's optional `FallbackPublicKey` is used only when the source
returns a confirmed absent key from a snapshot within its allowed age. A valid
empty JWKS also establishes absence. Expiration, rate limiting, dependency
failure, and cancellation reject authentication without using the fallback key.
The original cause remains available through `errors.Is` and `errors.As`.

`ReserveRefreshCapacity` reserves the final request in each configured rate
window for scheduled refresh. Requests for unknown key IDs cannot consume it.
The initial load and scheduled refreshes still count against the same total
limit. Choose the refresh interval and budget together. A limit of one leaves no
capacity for requests triggered by unknown keys. Neither a refresh interval nor
a reserved slot guarantees freshness while the endpoint is unavailable.

### Verification key policy

Managed methods can opt into `authentication.JWKSVerificationPolicy` through
`ManagedJWTMethodOptions.JWKSOptions.VerificationPolicy`:

```go
sourceOptions.VerificationPolicy = &authentication.JWKSVerificationPolicy{
    AllowedAlgorithms: []string{"RS256"},
}
```

Choose the policy alongside `ValidSigningMethods`. The parser's signing-method
list restricts token algorithms, while the JWKS policy also checks each key's
material, purpose, and algorithm binding. An explicitly declared key algorithm
must match the token, including when multiple RSA algorithms are allowed by the
parser. Managed fallback preserves this check.

A policy rejection matches `authentication.ErrJWKSKeyRejected` through
`errors.Is`, including through `ErrorAdapter`. A key excluded for encryption or
other incompatible usage cannot activate the static fallback. A truly absent
key in a fresh accepted snapshot retains the existing fallback behavior. Invalid
refreshes preserve the previous snapshot and its original age. Leaving
`VerificationPolicy` nil preserves existing key acceptance behavior.
