// Package ipx provides IP address utilities.
// ResolveClientAddress evaluates forwarding chains against explicit trusted
// proxy prefixes. It performs no network lookups and does not log addresses.
package ipx

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	defaultMaxForwardedBytes = 4096
	defaultMaxForwardedHops  = 16
	mappedIPv4PrefixBits     = 96
)

// ProxyPolicy defines trusted peers and bounds forwarded-address input.
// A zero policy trusts nobody. Zero byte and hop limits select 4096 and 16.
// Treat TrustedProxies as immutable while the policy is in use.
type ProxyPolicy struct {
	TrustedProxies    []netip.Prefix
	MaxForwardedBytes int
	MaxForwardedHops  int
}

// Validate checks prefix validity and configured forwarding limits.
// Negative limits and IPv4-mapped prefixes shorter than /96 are invalid.
// Valid prefixes may include host bits. Mapped prefixes with at least 96 bits
// are interpreted as their equivalent IPv4 prefixes.
func (p ProxyPolicy) Validate() error {
	if p.MaxForwardedBytes < 0 {
		return errors.New("proxy policy MaxForwardedBytes must be nonnegative")
	}
	if p.MaxForwardedHops < 0 {
		return errors.New("proxy policy MaxForwardedHops must be nonnegative")
	}
	for i, prefix := range p.TrustedProxies {
		if !prefix.IsValid() {
			return fmt.Errorf("trusted proxy prefix %d is invalid", i+1)
		}
		if prefix.Addr().Is4In6() && prefix.Bits() < mappedIPv4PrefixBits {
			return fmt.Errorf("trusted proxy prefix %d has an incompatible mapped IPv4 prefix length", i+1)
		}
	}
	return nil
}

// ResolveClientAddress resolves an ordered forwarding chain or returns the peer.
// It validates policy and the peer on each call. IPv4-mapped addresses are
// normalized and scoped addresses are rejected. An untrusted peer is returned
// immediately without parsing forwarded values. Each forwarded string is one
// field value; commas between values count toward the byte limit. Tokens may
// have surrounding spaces or tabs. The full chain is validated before walking
// from right to left to find the first untrusted hop. Missing data or an entirely
// trusted chain returns the peer. A malformed or oversized chain returns the
// normalized peer and an error; an invalid peer returns an invalid address and
// an error. Parsing errors wrap the original netip cause.
func ResolveClientAddress(
	peer netip.Addr,
	forwarded []string,
	policy ProxyPolicy,
) (netip.Addr, error) {
	if !peer.IsValid() || peer.Zone() != "" {
		return netip.Addr{}, errors.New("client peer must be a valid unscoped IP address")
	}
	peer = peer.Unmap()
	if err := policy.Validate(); err != nil {
		return peer, fmt.Errorf("invalid proxy policy: %w", err)
	}
	if !trustedProxy(peer, policy.TrustedProxies) || len(forwarded) == 0 {
		return peer, nil
	}
	maxBytes := policy.MaxForwardedBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxForwardedBytes
	}
	maxHops := policy.MaxForwardedHops
	if maxHops == 0 {
		maxHops = defaultMaxForwardedHops
	}
	hopCount := len(forwarded)
	if hopCount > maxHops {
		return peer, errors.New("forwarded chain exceeds hop limit")
	}
	byteCount := len(forwarded) - 1
	if byteCount > maxBytes {
		return peer, errors.New("forwarded chain exceeds byte limit")
	}
	for _, value := range forwarded {
		if len(value) > maxBytes-byteCount {
			return peer, errors.New("forwarded chain exceeds byte limit")
		}
		byteCount += len(value)
		commas := strings.Count(value, ",")
		if commas > maxHops-hopCount {
			return peer, errors.New("forwarded chain exceeds hop limit")
		}
		hopCount += commas
	}
	hops := make([]netip.Addr, 0, hopCount)
	for _, value := range forwarded {
		for {
			token, remaining, found := strings.Cut(value, ",")
			addr, err := netip.ParseAddr(strings.Trim(token, " \t"))
			if err != nil {
				return peer, fmt.Errorf("forwarded hop %d: %w", len(hops)+1, err)
			}
			if addr.Zone() != "" {
				return peer, fmt.Errorf("forwarded hop %d has a scoped address", len(hops)+1)
			}
			hops = append(hops, addr.Unmap())
			if !found {
				break
			}
			value = remaining
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !trustedProxy(hops[i], policy.TrustedProxies) {
			return hops[i], nil
		}
	}
	return peer, nil
}

func trustedProxy(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Addr().Is4In6() {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-mappedIPv4PrefixBits)
		}
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
