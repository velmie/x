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

JWKS diagnostics use the configured logger with bounded failure-stage and status
information. Request URLs, credentials, response bodies, and raw dependency errors
are excluded. Fallback messages record the reason and selected source. The HTTP
retry client's own request logging is disabled to keep these values out of logs.

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
in that client when needed. `JWKSOptions.WarnFunc` takes precedence over `Log`.
A nil signing-method list uses the existing defaults. An explicitly empty list
is rejected.

`NewJWTMethod` retains asynchronous best-effort initialization and its existing
fallback behavior. Its `SourceReady` signal means initialization has finished,
including an unsuccessful initial load. It does not report startup success.

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
