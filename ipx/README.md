# IP address utilities

Resolve a client address from an explicit trusted-proxy policy:

```go
policy := ipx.ProxyPolicy{
    TrustedProxies: trustedPrefixes,
}
if err := policy.Validate(); err != nil {
    return err
}
client, _ := ipx.ResolveClientAddress(peer, forwardedValues, policy)
source := "peer"
if client != peer.Unmap() {
    source = "forwarded"
}
```

The HTTP boundary supplies the parsed peer and the forwarding field values in
received order. It chooses the header and handles its own address parsing and
logging. This example intentionally uses the peer when forwarding is invalid.
The returned error includes the original address parsing cause.

A zero policy trusts nobody. Zero limits mean 4096 bytes and 16 hops. Negative
limits, invalid prefixes, and IPv4-mapped prefixes shorter than /96 are
configuration errors. Validate policy at startup and do not mutate its prefix
slice during use. `ResolveClientAddress` validates it again on every call.

An untrusted peer is returned without parsing forwarding data. Otherwise, the
resolver validates the entire comma-separated chain and walks from the nearest
hop to the farthest. Private and loopback addresses receive no special trust.
Missing forwarding data and an entirely trusted chain return the peer. Invalid
or oversized forwarding returns the normalized peer and an error. A scoped or
invalid peer returns an invalid address and an error. IPv4-mapped addresses and
compatible prefixes normalize to IPv4. Surrounding spaces and tabs are allowed.
Parsing errors wrap the original `netip` cause.

`RealIPGetter` and its methods remain available with their existing behavior.
It uses a different left-to-right and private-address filtering algorithm, so
migrating to `ResolveClientAddress` requires an explicit trust-policy change.
