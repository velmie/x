package ipx_test

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/velmie/x/ipx"
)

func TestResolveClientAddress(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	policy := ipx.ProxyPolicy{
		TrustedProxies: trusted,
	}
	for _, tc := range []struct {
		name      string
		peer      string
		forwarded []string
		policy    ipx.ProxyPolicy
		want      string
		wantError bool
	}{
		{
			name:      "untrusted peer ignores malformed forwarding",
			peer:      "192.0.2.4",
			forwarded: []string{"private-spoof"},
			policy:    policy,
			want:      "192.0.2.4",
		},
		{
			name:      "zero policy trusts no peer",
			peer:      "10.0.0.1",
			forwarded: []string{"198.51.100.8"},
			want:      "10.0.0.1",
		},
		{
			name:      "right to left through multiple fields",
			peer:      "10.0.0.3",
			forwarded: []string{"198.51.100.8, 10.0.0.1", "10.0.0.2"},
			policy:    policy,
			want:      "198.51.100.8",
		},
		{
			name:      "untrusted private hop is not skipped",
			peer:      "10.0.0.3",
			forwarded: []string{"198.51.100.8, 192.168.1.2, 10.0.0.2"},
			policy:    policy,
			want:      "192.168.1.2",
		},
		{
			name:      "untrusted loopback hop is not skipped",
			peer:      "10.0.0.3",
			forwarded: []string{"198.51.100.8, 127.0.0.1"},
			policy:    policy,
			want:      "127.0.0.1",
		},
		{
			name:      "malformed early hop falls back",
			peer:      "10.0.0.3",
			forwarded: []string{"private-invalid, 198.51.100.8"},
			policy:    policy,
			want:      "10.0.0.3",
			wantError: true,
		},
		{
			name:      "all trusted fall back",
			peer:      "10.0.0.3",
			forwarded: []string{"10.0.0.1, 10.0.0.2"},
			policy:    policy,
			want:      "10.0.0.3",
		},
		{
			name:   "absent forwarding",
			peer:   "10.0.0.3",
			policy: policy,
			want:   "10.0.0.3",
		},
		{
			name:      "empty forwarding field is malformed",
			peer:      "10.0.0.3",
			forwarded: []string{""},
			policy:    policy,
			want:      "10.0.0.3",
			wantError: true,
		},
		{
			name:      "empty chain token is malformed",
			peer:      "10.0.0.3",
			forwarded: []string{"198.51.100.8,,10.0.0.2"},
			policy:    policy,
			want:      "10.0.0.3",
			wantError: true,
		},
		{
			name:      "mapped peer and prefix",
			peer:      "::ffff:10.0.0.3",
			forwarded: []string{"::ffff:198.51.100.8"},
			policy: ipx.ProxyPolicy{
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("::ffff:10.0.0.0/104")},
			},
			want: "198.51.100.8",
		},
		{
			name:      "ipv6 trust",
			peer:      "2001:db8:1::3",
			forwarded: []string{"2001:db8:2::4, 2001:db8:1::2"},
			policy: ipx.ProxyPolicy{
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("2001:db8:1::/48")},
			},
			want: "2001:db8:2::4",
		},
		{
			name:      "scoped forwarded hop",
			peer:      "10.0.0.3",
			forwarded: []string{"fe80::1%eth0"},
			policy:    policy,
			want:      "10.0.0.3",
			wantError: true,
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				peer := netip.MustParseAddr(tc.peer)
				got, err := ipx.ResolveClientAddress(peer, tc.forwarded, tc.policy)
				if (err != nil) != tc.wantError || got != netip.MustParseAddr(tc.want) {
					t.Fatalf("got %v, %v; want %s, error=%v", got, err, tc.want, tc.wantError)
				}
			},
		)
	}
}

func TestResolveClientAddressBounds(t *testing.T) {
	peer := netip.MustParseAddr("10.0.0.1")
	forwarded := []string{"1.2.3.4", "5.6.7.8"}
	base := ipx.ProxyPolicy{
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		MaxForwardedBytes: 15,
		MaxForwardedHops:  2,
	}
	got, err := ipx.ResolveClientAddress(peer, forwarded, base)
	if err != nil || got.String() != "5.6.7.8" {
		t.Fatalf("exact bounds: %v, %v", got, err)
	}
	tooFewBytes := base
	tooFewBytes.MaxForwardedBytes = 14
	tooFewHops := base
	tooFewHops.MaxForwardedHops = 1
	for _, policy := range []ipx.ProxyPolicy{tooFewBytes, tooFewHops} {
		got, err = ipx.ResolveClientAddress(peer, forwarded, policy)
		if err == nil || got != peer {
			t.Fatalf("expected peer and bounds error, got %v, %v", got, err)
		}
	}
}

func TestProxyPolicyInvalidAndPeerValidation(t *testing.T) {
	for _, policy := range []ipx.ProxyPolicy{
		{
			MaxForwardedBytes: -1,
		},
		{
			MaxForwardedHops: -1,
		},
		{
			TrustedProxies: []netip.Prefix{{}},
		},
		{
			TrustedProxies: []netip.Prefix{netip.MustParsePrefix("::ffff:10.0.0.0/80")},
		},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("accepted policy %#v", policy)
		}
		peer := netip.MustParseAddr("192.0.2.1")
		if got, err := ipx.ResolveClientAddress(peer, nil, policy); err == nil || got != peer {
			t.Fatalf("invalid policy: %v, %v", got, err)
		}
	}
	for _, peer := range []netip.Addr{netip.Addr{}, netip.MustParseAddr("fe80::1%eth0")} {
		got, err := ipx.ResolveClientAddress(peer, nil, ipx.ProxyPolicy{})
		if err == nil || got.IsValid() {
			t.Fatalf("invalid peer: %v, %v", got, err)
		}
	}
}

func TestResolveClientAddressRetainsNativeParseError(t *testing.T) {
	peer := netip.MustParseAddr("10.0.0.1")
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	_, native := netip.ParseAddr("private-input")
	got, err := ipx.ResolveClientAddress(peer, []string{"private-input"}, policy)
	if got != peer || !errors.Is(err, native) || err.Error() != "forwarded hop 1: "+native.Error() {
		t.Fatalf("native cause: %v, %v", got, err)
	}
}

func TestResolveClientAddressConcurrentPolicy(t *testing.T) {
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	for i := 0; i < 20; i++ {
		t.Run(
			string(rune('a'+i)),
			func(t *testing.T) {
				t.Parallel()
				peer := netip.MustParseAddr("10.0.0.1")
				got, err := ipx.ResolveClientAddress(peer, []string{"198.51.100.4"}, policy)
				if err != nil || got.String() != "198.51.100.4" {
					t.Fatalf("got %v, %v", got, err)
				}
			},
		)
	}
}

func FuzzResolveClientAddress(f *testing.F) {
	for _, value := range []string{"", "1.2.3.4", "bad, 10.0.0.1", "fe80::1%eth0", ",,"} {
		f.Add(value)
	}
	peer := netip.MustParseAddr("10.0.0.1")
	policy := ipx.ProxyPolicy{
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		MaxForwardedBytes: 128,
		MaxForwardedHops:  4,
	}
	f.Fuzz(
		func(t *testing.T, value string) {
			got, err := ipx.ResolveClientAddress(peer, []string{value}, policy)
			if !got.IsValid() || err != nil && got != peer {
				t.Fatalf("invalid fallback: %v, %v", got, err)
			}
		},
	)
}

func TestResolveClientAddressDefaultLimits(t *testing.T) {
	peer := netip.MustParseAddr("10.0.0.1")
	policy := ipx.ProxyPolicy{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	exactBytes := strings.Repeat(" ", 4096-len("1.2.3.4")) + "1.2.3.4"
	got, err := ipx.ResolveClientAddress(peer, []string{exactBytes}, policy)
	if err != nil || got.String() != "1.2.3.4" {
		t.Fatalf("exact default byte limit: %v, %v", got, err)
	}
	got, err = ipx.ResolveClientAddress(peer, []string{" " + exactBytes}, policy)
	if err == nil || got != peer {
		t.Fatalf("over default byte limit: %v, %v", got, err)
	}
	values := make([]string, 16)
	values[0] = "198.51.100.5"
	for i := 1; i < len(values); i++ {
		values[i] = "10.0.0.2"
	}
	got, err = ipx.ResolveClientAddress(peer, values, policy)
	if err != nil || got.String() != "198.51.100.5" {
		t.Fatalf("exact default hop limit: %v, %v", got, err)
	}
	got, err = ipx.ResolveClientAddress(peer, append(values, "10.0.0.3"), policy)
	if err == nil || got != peer {
		t.Fatalf("over default hop limit: %v, %v", got, err)
	}
	untrusted := netip.MustParseAddr("192.0.2.1")
	got, err = ipx.ResolveClientAddress(untrusted, []string{strings.Repeat("x", 8192)}, policy)
	if err != nil || got != untrusted {
		t.Fatalf("untrusted peer must ignore oversized data: %v, %v", got, err)
	}
}
