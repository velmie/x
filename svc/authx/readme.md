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