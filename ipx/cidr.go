package ipx

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/mikioh/ipaddr"
)

type DefaultCIDRParser struct{}

func NewDefaultCIDRParser() *DefaultCIDRParser {
	return &DefaultCIDRParser{}
}

func (d DefaultCIDRParser) Parse(cidr string) (Range, error) {
	c, err := ipaddr.Parse(cidr)
	if err != nil {
		return Range{}, err
	}
	return Range{
		Start: c.First().IP,
		End:   c.Last().IP,
	}, nil
}

// ParseTrustedProxies parses comma-separated canonical CIDR networks.
// IPv4-mapped IPv6 prefixes are normalized to IPv4. Empty entries are invalid.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	entries := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", entry, err)
		}
		if prefix != prefix.Masked() {
			return nil, fmt.Errorf("trusted proxy %q is not a canonical network", entry)
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < mappedIPv4PrefixBits {
				return nil, fmt.Errorf("trusted proxy %q has an incompatible mapped IPv4 prefix length", entry)
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-mappedIPv4PrefixBits)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}
