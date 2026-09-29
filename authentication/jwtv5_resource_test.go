package authentication_test

import (
	"context"
	"crypto"
	"runtime"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
)

func TestJWTv5ParserMalformedTokenAllocation(t *testing.T) {
	const attempts = 8
	input := strings.Repeat(".", 1<<20)
	parser := authentication.NewJWTv5Parser(jwt.NewParser())
	keys := authentication.KeySourceFunc(func(context.Context, string) (crypto.PublicKey, error) {
		t.Fatal("malformed token must be rejected before key lookup")
		return nil, nil
	})
	// Warm up formatting and parser paths before measuring. Do not run this
	// test in parallel: TotalAlloc measures allocations across the process.
	_, _ = parser.Parse(context.Background(), input, keys)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < attempts; i++ {
		if token, err := parser.Parse(context.Background(), input, keys); err == nil || token != nil {
			t.Fatal("malformed token was accepted")
		}
	}
	runtime.ReadMemStats(&after)
	allocated := (after.TotalAlloc - before.TotalAlloc) / attempts
	// The vulnerable splitter allocates about 16 MiB per 1 MiB input on
	// 64-bit platforms. This generous bound detects amplification without
	// depending on exact allocation counts or execution speed.
	if allocated > 2<<20 {
		t.Fatalf("malformed token allocated %d bytes per parse; limit is %d", allocated, 2<<20)
	}
	t.Logf("malformed token allocated %d bytes per parse", allocated)
}
