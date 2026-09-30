package authx_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestManagedJWTKeyPolicyWithFallback(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verification := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "verification", Algorithm: "RS256", Use: "sig"}
	encryptionJSON, err := json.Marshal(jose.JSONWebKey{Key: &key.PublicKey, KeyID: "encryption", Algorithm: "RS256", Use: "enc"})
	if err != nil {
		t.Fatal(err)
	}
	var encryption map[string]any
	if err := json.Unmarshal(encryptionJSON, &encryption); err != nil {
		t.Fatal(err)
	}
	encryption["key_ops"] = []string{"decrypt"}
	response, err := json.Marshal(map[string]any{"keys": []any{verification, encryption}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(response) }))
	defer server.Close()
	method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), authx.ManagedJWTMethodOptions{
		Endpoint: server.URL,
		JWKSOptions: authentication.JWKSOptions{
			RefreshInterval:    time.Hour,
			VerificationPolicy: &authentication.JWKSVerificationPolicy{AllowedAlgorithms: []string{"RS256", "PS256"}},
		},
		ValidSigningMethods: []authx.JWTSigningMethod{authx.JWTSigningMethodRS256, authx.JWTSigningMethodPS256},
		FallbackPublicKey:   &key.PublicKey,
	})
	if shutdown != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := shutdown(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, kid string
		signing   jwt.SigningMethod
		rejected  bool
	}{
		{"correct algorithm", "verification", jwt.SigningMethodRS256, false},
		{"algorithm mismatch", "verification", jwt.SigningMethodPS256, true},
		{"excluded encryption key", "encryption", jwt.SigningMethodRS256, true},
		{"confirmed absence", "absent", jwt.SigningMethodRS256, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := jwt.NewWithClaims(tc.signing, jwt.MapClaims{"sub": "example"})
			token.Header["kid"] = tc.kid
			signed, err := token.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			_, err = authx.NewErrorAdapter(method).Authenticate(context.Background(), signed)
			if tc.rejected {
				if !errors.Is(err, authentication.ErrJWKSKeyRejected) {
					t.Fatalf("expected policy rejection without fallback, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("valid JWT rejected: %v", err)
			}
		})
	}
}
