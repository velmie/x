package authentication

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

func ExampleNewStaticJWKS() {
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"EdDSA"},
	}
	keyIDs := JWKSKeyIDPolicy{
		Required:         true,
		MaxBytes:         128,
		RejectDuplicates: true,
	}
	keys, err := NewStaticJWKS([]byte(exampleJWKS()), policy, keyIDs)
	if err != nil {
		panic(err)
	}
	_, err = keys.FetchPublicKeyForAlgorithm(context.Background(), "example", "EdDSA")
	fmt.Println(err == nil)
	// Output: true
}

func exampleJWKS() string {
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	encoded := base64.RawURLEncoding.EncodeToString(public)
	return `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"example","alg":"EdDSA","x":"` + encoded + `"}]}`
}
