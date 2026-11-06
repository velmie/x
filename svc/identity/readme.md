# Service identity

Use a verified principal in an application operation and compare permissions exactly:

```go
if !principal.Valid() || !principal.Has("books:write") {
    return ErrForbidden
}
```

An authentication boundary establishes the principal. `Valid` checks only the
issuer and subject; it does not prove that a principal was authenticated or
validate its permissions. Issuer and subject must be nonblank UTF-8 text within
the exported byte limits. Their original text, including surrounding whitespace,
is retained. The zero principal is invalid. `Has` uses exact comparison and
returns false for an empty permission slice; it does not expand roles or wildcards.
The verifier validates the number and contents of permissions against the
exported limits.

A custom authenticator can wrap `ErrInvalidCredentials` or `ErrUnavailable`
with `%w` to classify a rejection while preserving its cause. The service owns
logging and redaction of foreign error causes. These identities do not replace
application errors such as forbidden access.

The package uses only the standard library and requires Go 1.21. It starts no
workers and owns no resources. HTTP and Connect boundaries can share this type;
application-specific permissions and ownership rules remain in the service.
