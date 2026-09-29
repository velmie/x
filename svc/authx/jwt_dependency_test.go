package authx_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/svc/authx"
)

func TestJWTMethodSignedTokens(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	method, err := authx.NewJWTMethod(authx.WithJWTPublicKey(key.Public()))
	if err != nil {
		t.Fatal(err)
	}
	sign := func(key *ecdsa.PrivateKey) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"sub": "test-subject"}).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	for _, tc := range []struct {
		name  string
		token string
		valid bool
	}{
		{name: "valid signature", token: sign(key), valid: true},
		{name: "invalid signature", token: sign(otherKey)},
		{name: "malformed", token: "..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entity, err := method.Authenticate(context.Background(), tc.token)
			if tc.valid {
				if err != nil || entity["sub"] != "test-subject" {
					t.Fatalf("valid token authentication: entity=%v err=%v", entity, err)
				}
			} else if err == nil {
				t.Fatal("invalid token was accepted")
			}
		})
	}
}
