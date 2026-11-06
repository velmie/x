package authx_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
	"github.com/velmie/x/svc/identity"
)

func TestVerifierIdentityAndKeyIDBoundaries(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := strings.Repeat("i", identity.MaxIssuerBytes)
	subject := strings.Repeat("s", identity.MaxSubjectBytes)
	kid := strings.Repeat("k", authx.DefaultMaxKeyIDBytes)
	policy := verifierPolicy("EdDSA")
	policy.Issuers = []string{issuer}
	verifier := newVerifier(t, authentication.KeySourceMap{kid: public}, policy, authx.ClaimLimits{})
	claims := verifierClaims()
	claims["iss"] = issuer
	claims["sub"] = subject
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, kid, claims)
	principal, err := verifier.Authenticate(context.Background(), raw)
	if err != nil || principal.Issuer != issuer || principal.Subject != subject {
		t.Fatalf("maximum identity rejected: %v", err)
	}
	claims["sub"] = subject + "x"
	raw = verifierToken(t, jwt.SigningMethodEdDSA, private, kid, claims)
	principal, err = verifier.Authenticate(context.Background(), raw)
	if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() {
		t.Fatalf("oversized subject accepted: %v", err)
	}
	claims["sub"] = subject
	claims["iss"] = issuer + "x"
	raw = verifierToken(t, jwt.SigningMethodEdDSA, private, kid, claims)
	principal, err = verifier.Authenticate(context.Background(), raw)
	if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() {
		t.Fatalf("oversized issuer accepted: %v", err)
	}
}

func TestVerifierTighterLimitsAndSnapshots(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := verifierPolicy("EdDSA")
	policy.Issuers = []string{"iss"}
	policy.Audiences = []string{"aud"}
	limits := authx.ClaimLimits{
		MaxKeyIDBytes:      3,
		MaxIssuerBytes:     3,
		MaxSubjectBytes:    3,
		MaxPermissions:     1,
		MaxPermissionBytes: 3,
	}
	verifier := newVerifier(t, authentication.KeySourceMap{"key": public}, policy, limits)
	policy.Algorithms[0] = "none"
	policy.Issuers[0] = "changed"
	policy.Audiences[0] = "changed"
	claims := verifierClaims()
	claims["iss"] = "iss"
	claims["sub"] = "sub"
	claims["aud"] = "aud"
	claims["permissions"] = []string{"abc"}
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims)
	if _, err := verifier.Authenticate(context.Background(), raw); err != nil {
		t.Fatalf("tight boundary or copied policy: %v", err)
	}
	tooLongKID := verifierToken(t, jwt.SigningMethodEdDSA, private, "long", claims)
	if _, err := verifier.Authenticate(context.Background(), tooLongKID); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("tighter key ID bound not applied: %v", err)
	}
	for _, change := range []func(jwt.MapClaims){
		func(c jwt.MapClaims) { c["iss"] = "long" },
		func(c jwt.MapClaims) { c["sub"] = "long" },
		func(c jwt.MapClaims) { c["permissions"] = []string{"long"} },
		func(c jwt.MapClaims) { c["permissions"] = []string{"a", "b"} },
	} {
		copyClaims := make(jwt.MapClaims, len(claims))
		for k, v := range claims {
			copyClaims[k] = v
		}
		change(copyClaims)
		token := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", copyClaims)
		if _, err := verifier.Authenticate(context.Background(), token); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("tighter limit not applied: %v", err)
		}
	}
	// Use the same token size with a matching trust policy to test the exact bound.
	matching := verifierPolicy("EdDSA")
	matching.Issuers = []string{"iss"}
	matching.Audiences = []string{"aud"}
	exact := newVerifier(
		t,
		authentication.KeySourceMap{"key": public},
		matching,
		authx.ClaimLimits{
			MaxTokenBytes: len(raw),
		},
	)
	if _, err := exact.Authenticate(context.Background(), raw); err != nil {
		t.Fatalf("token at configured limit rejected: %v", err)
	}
	below := newVerifier(
		t,
		authentication.KeySourceMap{"key": public},
		matching,
		authx.ClaimLimits{
			MaxTokenBytes: len(raw) - 1,
		},
	)
	if _, err := below.Authenticate(context.Background(), raw); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("token above configured limit accepted: %v", err)
	}
}

func TestVerifierMaximumTokenLength(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(
		t,
		authentication.KeySourceSingle{
			PublicKey: public,
		},
		verifierPolicy("EdDSA"),
		authx.ClaimLimits{},
	)
	claims := verifierClaims()
	claims["padding"] = ""
	var boundary string
	for _, kid := range []string{"k", "kk", "kkk"} {
		base := verifierToken(t, jwt.SigningMethodEdDSA, private, kid, claims)
		padding := (authx.DefaultMaxTokenBytes - len(base)) * 3 / 4
		for delta := -8; delta <= 8; delta++ {
			claims["padding"] = strings.Repeat("p", padding+delta)
			candidate := verifierToken(t, jwt.SigningMethodEdDSA, private, kid, claims)
			if len(candidate) == authx.DefaultMaxTokenBytes {
				boundary = candidate
				break
			}
		}
		if boundary != "" {
			break
		}
		claims["padding"] = ""
	}
	if boundary == "" {
		t.Fatal("could not construct token at the maximum size")
	}
	if _, err := verifier.Authenticate(context.Background(), boundary); err != nil {
		t.Fatalf("token at maximum size rejected: %v", err)
	}
	if _, err := verifier.Authenticate(context.Background(), boundary+"x"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("token over maximum size accepted: %v", err)
	}
}

func TestVerifierEnforcesStaticJWKAlgorithmBinding(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(
		jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{
					Key:       &key.PublicKey,
					KeyID:     "key",
					Use:       "sig",
					Algorithm: "RS256",
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := authentication.NewStaticJWKS(
		document,
		authentication.JWKSVerificationPolicy{
			AllowedAlgorithms: []string{"RS256", "PS256"},
		},
		authentication.JWKSKeyIDPolicy{
			Required:         true,
			MaxBytes:         authx.DefaultMaxKeyIDBytes,
			RejectDuplicates: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := verifierPolicy("RS256")
	policy.Algorithms = append(policy.Algorithms, "PS256")
	verifier := newVerifier(t, keys, policy, authx.ClaimLimits{})
	good := verifierToken(t, jwt.SigningMethodRS256, key, "key", verifierClaims())
	if _, err := verifier.Authenticate(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	bad := verifierToken(t, jwt.SigningMethodPS256, key, "key", verifierClaims())
	principal, err := verifier.Authenticate(context.Background(), bad)
	if !errors.Is(err, identity.ErrInvalidCredentials) || !errors.Is(err, authentication.ErrKeyRejected) || principal.Valid() {
		t.Fatalf("JWK algorithm binding bypassed: %v", err)
	}
}

func TestVerifierConcurrentAuthentication(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(t, authentication.KeySourceMap{"key": public}, verifierPolicy("EdDSA"), authx.ClaimLimits{})
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", verifierClaims())
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := verifier.Authenticate(context.Background(), raw); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
}
