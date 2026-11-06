package authx_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func ExampleTrustPolicy_Validate() {
	policy := authx.TrustPolicy{
		Algorithms: []string{"EdDSA"},
		Issuers:    []string{"https://issuer.example"},
		Audiences:  []string{"bookstore"},
	}
	fmt.Println(policy.Validate() == nil)
	// Output: true
}

func ExampleNewVerifier() {
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	keys := authentication.KeySourceMap{
		"key": private.Public(),
	}
	policy := authx.TrustPolicy{
		Algorithms: []string{"EdDSA"},
		Issuers:    []string{"https://issuer.example"},
		Audiences:  []string{"bookstore"},
	}
	verifier, err := authx.NewVerifier(keys, policy, authx.ClaimLimits{})
	if err != nil {
		panic(err)
	}
	token := jwt.NewWithClaims(
		jwt.SigningMethodEdDSA,
		jwt.MapClaims{
			"iss":         "https://issuer.example",
			"sub":         "user-42",
			"aud":         "bookstore",
			"exp":         time.Now().Add(time.Hour).Unix(),
			"permissions": []string{"books:read"},
		},
	)
	token.Header["kid"] = "key"
	raw, err := token.SignedString(private)
	if err != nil {
		panic(err)
	}
	principal, err := verifier.Authenticate(context.Background(), raw)
	if err != nil {
		panic(err)
	}
	fmt.Println(principal.Subject, principal.Has("books:read"))
	// Output: user-42 true
}
