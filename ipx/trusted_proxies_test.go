package ipx_test

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/velmie/x/ipx"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := ipx.ParseTrustedProxies(" 192.0.2.0/24, 2001:db8::/32, ::ffff:198.51.100.0/120 ")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("198.51.100.0/24")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, input := range []string{"", "192.0.2.0/24,", ",192.0.2.0/24", "192.0.2.1/24", "::ffff:0:0/95", "bad"} {
		if _, err := ipx.ParseTrustedProxies(input); err == nil {
			t.Errorf("%q accepted", input)
		}
	}
	_, err = ipx.ParseTrustedProxies("192.0.2.0/24, bad")
	if err == nil || !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "ParsePrefix") {
		t.Fatalf("missing original parse cause: %v", err)
	}
}

func ExampleParseTrustedProxies() {
	prefixes, err := ipx.ParseTrustedProxies("192.0.2.0/24, ::ffff:198.51.100.0/120")
	if err != nil {
		panic(err)
	}
	fmt.Println(prefixes)
	// Output: [192.0.2.0/24 198.51.100.0/24]
}
