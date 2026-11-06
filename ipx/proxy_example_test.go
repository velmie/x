package ipx_test

import (
	"fmt"
	"net/netip"

	"github.com/velmie/x/ipx"
)

func ExampleProxyPolicy_Validate() {
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	fmt.Println(policy.Validate())
	// Output: <nil>
}

func ExampleResolveClientAddress() {
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	if err := policy.Validate(); err != nil {
		fmt.Println(err)
		return
	}
	peer := netip.MustParseAddr("10.0.0.3")
	client, err := ipx.ResolveClientAddress(peer, []string{"198.51.100.7, 10.0.0.2"}, policy)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(client)
	// Output: 198.51.100.7
}

func ExampleResolveClientAddress_fallback() {
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	peer := netip.MustParseAddr("10.0.0.3")
	client, err := ipx.ResolveClientAddress(peer, []string{"malformed"}, policy)
	fmt.Println(client, err != nil)
	// Output: 10.0.0.3 true
}
