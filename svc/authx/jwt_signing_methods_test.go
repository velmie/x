package authx_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestJWTMethodSigningAllowlist(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tokens := make(map[string]string)
	for _, signingMethod := range []jwt.SigningMethod{jwt.SigningMethodRS256, jwt.SigningMethodPS256} {
		token, err := jwt.NewWithClaims(signingMethod, jwt.MapClaims{"sub": "test-subject"}).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		tokens[signingMethod.Alg()] = token
	}
	for _, tc := range []struct {
		name    string
		allowed []authx.JWTSigningMethod
	}{
		{name: "default"},
		{name: "RS256 only", allowed: []authx.JWTSigningMethod{authx.JWTSigningMethodRS256}},
		{name: "PS256 only", allowed: []authx.JWTSigningMethod{authx.JWTSigningMethodPS256}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := []authx.JWTMethodOption{authx.WithJWTPublicKey(key.Public())}
			if tc.allowed != nil {
				options = append(options, authx.WithJWTSigningMethods(tc.allowed))
			}
			method, err := authx.NewJWTMethod(options...)
			if err != nil {
				t.Fatal(err)
			}
			for algorithm, token := range tokens {
				t.Run(algorithm, func(t *testing.T) {
					entity, err := method.Authenticate(context.Background(), token)
					allowed := tc.allowed == nil || tc.allowed[0] == algorithm
					if allowed {
						if err != nil || entity["sub"] != "test-subject" {
							t.Fatalf("allowed algorithm rejected: entity=%v err=%v", entity, err)
						}
					} else if !errors.Is(err, authentication.ErrBadToken) || entity != nil {
						t.Fatalf("disallowed algorithm accepted: entity=%v err=%v", entity, err)
					}
				})
			}
		})
	}
	if _, err := authx.NewJWTMethod(authx.WithJWTPublicKey(key.Public()), authx.WithJWTSigningMethods([]authx.JWTSigningMethod{})); err == nil {
		t.Fatal("empty allowlist must remain invalid")
	}
}
