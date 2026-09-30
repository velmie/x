# Authentication

This package provides functionality for authentication tasks.

## Short introduction

The working horse of this package is the `Authenticate(ctx context.Context, token string) (Entity, error)` method which accepts arbitrary string token and returns a
set of attributes represented by the `type Entity map[string]any` type.

## JWT authentication

The package provides JSON Web Token (JWT) authentication capabilities through the ViaJWT struct which depends upon
a JWT parser and key source for parsing and validating the token respectively.

Here is an example of how to use the ViaJWT:

```go
package main

import (
	"context"
	"crypto"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
)

func main() {

	parser := authentication.NewJWTv5Parser(jwt.NewParser())
	keySource := authentication.KeySourceFunc(myPublicKey)

	jwtAuth := authentication.NewViaJWT(parser, keySource)

	const token = "eyJhbGciOiJFUzI1NiIsImtpZCI6IjB5eHE4Z0ViOThTUkxTWGlrNDFZY3p4UGhPcVZ5Ulc0ZG16VjBydVBtbnciLCJ0eXAiOiJ" +
		"KV1QifQ.eyJpYXQiOjE2ODMxMTU2MTQsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.i7vDxB_hUE-08n3vUCngyyiG6" +
		"qvvwR5rl1-vDsyqs5MwuXM8wIuAmPITJ3-JY7wCOxy-oSdZ-_joutqdy80mLg"

	entity, err := jwtAuth.Authenticate(context.Background(), token)
	if err != nil {
		// handle error
		// ...
	}
	fmt.Println(entity["name"]) // John Doe
	//...

}

func myPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	var key crypto.PublicKey
	// do something to get the key using the kid (key id)
	// ...
	return key, nil
}
```

### Key Sources

The package provides different key source implementations to fetch public keys for JWT token validation.

#### KeySourceFunc

`KeySourceFunc` is a function type that implements the `KeySource` interface. This allows you to use a simple function
as a key source without having to create a separate struct implementing the `KeySource` interface.

#### KeySourceMap

`KeySourceMap` is a map of key IDs (kids) to public keys, implementing the `KeySource` interface.

#### KeySourceSingle

`KeySourceSingle` is a struct that implements the `KeySource` interface and returns a single public key, regardless of the input key ID (kid). 
This implementation is useful when you have a single public key for token validation.

##### Usage

```go
// Create a KeySourceMap with key IDs and their corresponding public keys
keySource := authentication.KeySourceMap{
	"keyID1": publicKey1,
	"keyID2": publicKey2,
}

// Create a KeySourceSingle with a single public key
keySource := authentication.KeySourceSingle{
	PublicKey: publicKey,
}

// Use a custom KeySourceFunc
keySource := authentication.KeySourceFunc(func(ctx context.Context, kid string) (crypto.PublicKey, error) {
	// Fetch the public key based on the key ID (kid)
})
```

### JWKS (JSON Web Key Set) Key Source

`KeySourceJWKS` is an implementation of the `KeySource` interface that fetches public keys from a remote JSON Web Key Set (JWKS) endpoint. 
JWKS is a JSON object that represents a set of keys containing the public keys used to verify any JSON Web Token (JWT) issued by the authorization server.


#### Features

- Fetches public keys from a remote JWKS endpoint using HTTP requests.
- Supports request rate limiting and warning functions.
- Allows fetching on unknown key IDs (kids) or not requesting on unknown kids, depending on configuration.
- Caches fetched keys to avoid unnecessary requests.
- Refreshes keys periodically without blocking reads of cached keys. The refresh interval does not expire cached keys; `MaxCacheAge` enables a separate age limit.
- Shares concurrent refresh requests. Each caller can cancel its own wait. The HTTP request is canceled when all waiters leave or `Stop` is called.
- For sources created by `NewKeySourceJWKS`, `Stop` stops background refresh while cached reads and later refreshes for unknown keys remain available. Managed sources use the terminal lifecycle described below.

##### Usage

```go
package main

import (
	"context"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"net/http"
	"time"
)

func main() {

	parser := authentication.NewJWTv5Parser(jwt.NewParser())

	jwksOptions := authentication.JWKSOptions{
		Client:              http.DefaultClient, // customize http client (default: http.DefaultClient)
		RefreshInterval:     3 * time.Minute,    // customize keys refresh interval (default: 1 * time.Minute)
		RequestOnUnknownKID: true,               // whether to request unknown kid from JWKS endpoint (default: false)
		WarnFunc: func(msg string) {
			fmt.Printf("WARN: %s\n", msg) // optional warning function (default: nil)
		},
	}

	// enable rate limiting for requests to JWKS endpoint (default: no limit)
	jwksOptions.SetRefreshRateLimit(5, time.Minute) // 5 requests per minute

	keySource := authentication.NewKeySourceJWKS("https://example.com/.well-known/jwks.json", &jwksOptions)
	defer keySource.Stop()

	jwtAuth := authentication.NewViaJWT(parser, keySource)

	const token = "eyJhbGciOiJFUzI1NiIsImtpZCI6IjB5eHE4Z0ViOThTUkxTWGlrNDFZY3p4UGhPcVZ5Ulc0ZG16VjBydVBtbnciLCJ0eXAiOiJ" +
		"KV1QifQ.eyJpYXQiOjE2ODMxMTU2MTQsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.i7vDxB_hUE-08n3vUCngyyiG6" +
		"qvvwR5rl1-vDsyqs5MwuXM8wIuAmPITJ3-JY7wCOxy-oSdZ-_joutqdy80mLg"

	entity, err := jwtAuth.Authenticate(context.Background(), token)
	if err != nil {
		// handle error
		// ...
	}
	fmt.Println(entity["name"]) // John Doe
	//...

}
```

### Managed JWKS lifecycle

Use `NewManagedKeySourceJWKS` when startup must report whether the first key load
succeeded. Construction validates configuration and performs no I/O or goroutine
startup. Call `Start(ctx)` before using the source:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

source, err := authentication.NewManagedKeySourceJWKS(
    "https://example.com/.well-known/jwks.json",
    &authentication.JWKSOptions{
        Client: &http.Client{Timeout: 5 * time.Second},
        RefreshInterval: time.Minute,
    },
)
if err != nil {
    return err
}
defer source.Stop()
if err := source.Start(ctx); err != nil {
    return err
}
// Pass source to authentication.NewViaJWT with the configured JWT parser.
```

`Start` loads keys synchronously and starts periodic refresh only after success.
A failed initial load can be retried. Concurrent startup returns
`ErrKeySourceStarting`. After successful startup, repeated calls return success
without another load or changing the original lifetime context.

Before successful startup, reads return `ErrKeySourceNotStarted`. `Stop` is
idempotent and terminal for a managed source, including when called before
`Start`. Reads and startup after stopping match `ErrKeySourceStopped`.
Cancellation of the lifetime context prevents further cached reads and cancels
active requests. Its cause remains available through `errors.Is`, including
`context.Canceled` or `context.DeadlineExceeded`. Prompt HTTP termination requires
a client that honors request cancellation. Configure the client's timeout for
network bounds independent of caller waits.

Managed construction rejects invalid HTTP endpoints, negative refresh intervals
or response limits, and nonpositive configured rate limits. A zero refresh
interval selects the default one minute. The legacy constructor remains eager
and best effort. A negative legacy refresh interval selects the same default and
emits a bounded warning. Calling `Start` on a legacy source is a no-op, including
after `Stop`; it neither replaces the lifetime nor restarts background refresh.

Use `Shutdown(ctx)` when the owner must wait for completion. It permanently
closes either kind of source, cancels active work, and waits for startup,
refresh requests, response-body cleanup, and diagnostic callbacks. After a
successful return, the source cannot start more work. Reads and `Start` return
`ErrKeySourceStopped`, including for a source created by the legacy constructor.

```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
defer shutdownCancel()
if err := source.Shutdown(shutdownCtx); err != nil {
    // The source is closed, but its work may still be finishing.
    return err
}
```

Concurrent and repeated calls are supported. A context error limits waiting;
it cannot force a custom HTTP client or callback to return. A later call may
continue waiting. Call `Shutdown` outside the source's callbacks. Callbacks can
use the nonblocking `Stop`. The application still owns its authentication
handlers and shared HTTP client; the source does not close that client.

### JWT verification key policy

Set `JWKSOptions.VerificationPolicy` to opt into public verification keys:

```go
options := authentication.JWKSOptions{
    VerificationPolicy: &authentication.JWKSVerificationPolicy{
        AllowedAlgorithms: []string{"RS256", "ES256"},
    },
}
```

The policy accepts public RSA keys of at least 2048 bits, EC keys on the matching
NIST curve, and Ed25519 keys that encode canonical curve points of non-small order. Supported algorithms
are RS256/384/512, PS256/384/512, ES256/384/512, and EdDSA (Ed25519). Private and
symmetric key material is rejected for verification candidates. A nil policy
preserves legacy key acceptance, including symmetric keys.

When present, `use` must allow signatures and `key_ops` must include `verify`.
Duplicate operations, conflicting usage metadata, and malformed metadata reject
the response. Keys of another purpose or a disallowed algorithm are excluded.
Duplicate IDs among usable keys reject the whole response regardless of order.
A single usable key may omit `kid`; multiple usable keys without IDs are ambiguous.

Each admitted key is bound to one algorithm. Its `alg`, when present, must match
its key material and the policy. An omitted `alg` is accepted only when the policy
identifies exactly one compatible algorithm. `JWTv5Parser` automatically passes
the token's algorithm to `FetchPublicKeyForAlgorithm` and retains its own parser
allowlist. Custom parsers and key-source wrappers must forward this optional
method to enforce the binding. `KeySource` itself still requires only
`FetchPublicKey`, which selects a key without checking a token's algorithm.

Failed validation retains the preceding snapshot and its original age. An empty
`keys: []` clears the snapshot. A nonempty response with no usable keys is a
validation failure. Lookups of excluded keys and algorithm mismatches match
`ErrJWKSKeyRejected`, not `ErrKeyNotFound`; they must not activate an absence-only
fallback. Rejected IDs are retained only for the current snapshot.

The policy and its algorithm list are copied during construction. Managed
construction reports invalid policy configuration immediately. The eager
constructor reports it through diagnostics and subsequent lookups reject it.

### JWT error categories and causes

`JWTv5Parser` and `ViaJWT` retain the original JWT or key-source error for
`errors.Is` and `errors.As`. A missing key matches both `ErrTokenUnverifiable`
and `ErrKeyNotFound`. Other key-source failures retain the `ErrBadToken`
category and expose their underlying cause, including context cancellation.
`ViaJWT` returns the context error directly when its context is already canceled.

Claims validation failures, including expiration, not-before, issuer, and audience,
match `ErrNotAuthenticated`. This corrects earlier classification as `ErrBadToken`.
Malformed tokens and signature or verification failures match `ErrBadToken` and
have priority over nested claims failures. The original causes remain available
regardless of the selected category. The parser's configured claim requirements
are unchanged. Use `errors.Is` and `errors.As`, rather than error text, to inspect
failures.

### JWKS responses and diagnostics

A successful JWKS response must be a JSON object containing a `keys` array.
Malformed documents, a missing or null array, invalid keys, and HTTP failures
leave the last valid key set intact. A valid empty array removes all cached keys.
Each successful replacement also removes keys omitted from the new set.

Set `JWKSOptions.MaxResponseBytes` to a positive byte limit to bound successful
response bodies. The source reads at most one extra byte to detect an oversized
response and rejects it without replacing cached keys. Zero preserves unlimited
response sizes. Non-success response bodies are closed without being read.

`DiagnosticFunc` receives a `*JWKSError`. Its `Operation`, `Stage`, `Reason`,
`Outcome`, `HTTPStatus`, and `Field` getters expose bounded fields suitable for structured
logging. `Error()` includes those fields and excludes response bodies, endpoint
URLs, key IDs, and underlying error text. When `DiagnosticFunc` is unset,
`WarnFunc` receives this safe text. Supplying both selects `DiagnosticFunc` only.
`Field` identifies a configuration field or a JSON path such as `keys[1].alg`,
without including the rejected value. Secondary body-close failures also appear
in the text as `close_reason=body_close_failed`.

| Failure | Stage | Reason |
| --- | --- | --- |
| Invalid endpoint | `config` | `invalid_endpoint` |
| HTTP client failure | `request` | `request_failed` |
| Non-200 response | `response` | `http_status` |
| Response read failure | `read` | `body_read_failed` |
| Malformed JSON | `decode` | `invalid_json` |
| Exhausted refresh budget | `limit` | `rate_limit` |
| Response close failure | `close` | `body_close_failed` |

Returned configuration errors retain URL parsing causes. Refresh errors preserve
network, context, decoding, and body-close causes for `errors.Is` and `errors.As`.
A primary failure takes precedence over a secondary close failure, with both
causes available through the same diagnostic. A close failure after valid keys
were loaded remains a warning and does not prevent publication. Rate-limit
lookup failures match both `ErrJWKSRateLimited` and the historical `ErrKeyNotFound`,
and `errors.Unwrap` still returns `ErrKeyNotFound`.

Each shared refresh failure produces one event. Cancellation alone is silent,
but an accompanying close failure is reported. No callback is emitted for an
ordinary lookup rejection; the caller receives that error directly. Managed
configuration errors are returned to the constructor's caller.

Callbacks run outside source locks, may run concurrently, and may call
`FetchPublicKey` or `Stop`. `Shutdown` waits for them and must be called outside
the callback. Use `DiagnosticFunc` when the logging adapter needs the original
error objects. Pass the diagnostic and its `Unwrap()` result as structured
fields, and redact sensitive values before output. Preserve the useful cause
message and `Field` path. The legacy `WarnFunc` only receives the safe summary
string, so it cannot inspect original error objects.

### JWKS snapshot age and refresh capacity

Set `JWKSOptions.MaxCacheAge` to a positive duration to limit trust in cached
keys. Age begins when a validated JWKS snapshot is successfully published. At or
beyond the configured age, every lookup fails with `ErrKeySetExpired`, including
lookups for unknown key IDs. The same error applies if no valid snapshot has
been loaded. It remains available through `errors.Is` when using `ViaJWT`.
Expired reads do not trigger foreground refresh. Periodic refresh can restore
availability; failed requests never extend the snapshot's age. A valid empty
snapshot is fresh and represents absence of keys. Zero keeps unlimited age.
Managed construction rejects a negative age, while the legacy constructor
ignores it.

When `RequestOnUnknownKID` is enabled, unknown-key traffic shares the refresh
budget with periodic refresh. Set `ReserveRefreshCapacity` together with
`SetRefreshRateLimit` to reserve the final slot of each window for periodic or
initial loading. Demand requests may use the preceding slots and join an actual
in-flight load without consuming additional slots. The total limit remains
unchanged. With a limit of one, demand refreshes cannot start a request.

This policy preserves capacity for the next periodic refresh, but does not
promise that every scheduled refresh succeeds: initial and periodic loads can
also exhaust the total budget. Choose the rate window, refresh interval, cache
age, and HTTP timeout together for the application's rotation requirements.
Managed construction requires a positive configured rate limit when reservation
is enabled. Legacy sources without a rate limit remain unlimited.

A rate-limited miss matches `ErrKeyNotFound` through `errors.Is`, but is a wrapped
failure rather than confirmed absence. A fallback policy that requires confirmed
absence should accept only the direct `ErrKeyNotFound` result and must propagate
expiration, cancellation, lifecycle, and dependency failures.
