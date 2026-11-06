# HTTP authentication

Protect a handler with a verifier and pass its principal to the operation:

```go
protected := requestauth.Require(
    verifier,
    logger,
    requestauth.PrincipalHandler(books.CreateBook),
)
mux.Handle("/bookstore/private/v1/books", protected)
```

`verifier` implements `Authenticator`; an `*authx.Verifier` satisfies it
directly. Construct the key source and trust policy in service bootstrap. The
verifier and key source own no transport authorization rules. The operation
checks permissions and ownership using its principal. `Principal` panics when a
required gate was omitted, so use `WithPrincipal` only at a trusted boundary.

The gate extracts exactly one Authorization header with a case-insensitive
Bearer scheme and two whitespace-separated fields. Duplicate headers, combined
credentials, values above 8200 bytes and tokens above 8192 bytes are rejected.
It reads no cookies, query parameters or request body. `BearerToken` and
`StrictBearerTokenExtractor` return the existing `ErrMissingToken` or
`ErrInvalidToken`. `Authenticate` also classifies extraction failures as
`identity.ErrInvalidCredentials` and does no logging or response writing.

Missing, malformed and invalid credentials receive HTTP 401 with
`WWW-Authenticate: Bearer`. An authentication dependency failure receives 503,
a request cancellation 499, a request deadline 504, and an unclassified failure
500. A live request with a dependency timeout receives 503. HEAD responses have
the same status and headers without a body. The gate records one rejection and
preserves any response-write failure in that record. The logger receives the
original error and logs it as is.

Existing users can adopt strict header framing while retaining their pipeline and
injector:

```go
extractor := requestauth.StrictBearerTokenExtractor{}
pipeline := requestauth.NewPipeline(extractor, method, injector)
```

`NewBearerTokenExtractor` and `NewPipeline` retain their existing behavior. The
package starts no workers and owns no resources. It requires Go 1.21.
