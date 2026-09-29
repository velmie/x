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

`WarnFunc` receives bounded diagnostics identifying the operation, stage, reason,
outcome, and HTTP status when applicable. Each shared refresh failure produces
one warning, except cancellation. Response bodies, endpoint URLs, key IDs, and
underlying error text are excluded. A body-close warning is reported only when
there is no primary refresh failure. Callbacks run after the shared load completes
and may call `FetchPublicKey` or `Stop`.

Returned errors retain underlying network, context, and decoding causes for
`errors.Is` and `errors.As`. Those causes may contain sensitive data. Log the
sanitized outer error or the supplied warning, rather than an unwrapped cause.

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
