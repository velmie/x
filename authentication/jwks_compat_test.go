package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"testing"
	"time"
)

// This positional literal was valid for consumers of authentication v1.2.0.
var _ = JWKSVerificationPolicy{
	[]string{"ES256"},
}

var _ func([]byte, JWKSVerificationPolicy, JWKSKeyIDPolicy) (*StaticJWKS, error) = NewStaticJWKS
var _ JWKSKeyIDPolicy = JWKSOptions{}.KeyIDPolicy

func TestManagedKeyIDRulesAreIndependent(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verification := policyJWK(t, &key.PublicKey, "same", "sig", "ES256")
	unrelated := policyJWK(t, &key.PublicKey, "same", "enc", "ES256")
	for _, tc := range []struct {
		name       string
		keyIDs     JWKSKeyIDPolicy
		rejectsIDs bool
	}{
		{
			name: "zero retains legacy filtering",
		},
		{
			name: "required alone does not reject duplicates",
			keyIDs: JWKSKeyIDPolicy{
				Required: true,
			},
		},
		{
			name: "limit alone does not reject duplicates",
			keyIDs: JWKSKeyIDPolicy{
				MaxBytes: 4,
			},
		},
		{
			name: "duplicate rejection alone",
			keyIDs: JWKSKeyIDPolicy{
				RejectDuplicates: true,
			},
			rejectsIDs: true,
		},
		{
			name: "all key ID rules",
			keyIDs: JWKSKeyIDPolicy{
				Required:         true,
				MaxBytes:         4,
				RejectDuplicates: true,
			},
			rejectsIDs: true,
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				for _, keys := range [][]map[string]any{{verification, unrelated}, {unrelated, verification}} {
					body := policyDocument(t, keys...)
					source, err := NewManagedKeySourceJWKS(
						"https://example.test/keys",
						&JWKSOptions{
							Client: concurrencyClient(
								func(*http.Request) (*http.Response, error) {
									return responseWithBody(body), nil
								},
							),
							VerificationPolicy: &JWKSVerificationPolicy{
								AllowedAlgorithms: []string{"ES256"},
							},
							KeyIDPolicy:     tc.keyIDs,
							RefreshInterval: time.Hour,
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					err = source.Start(context.Background())
					if tc.rejectsIDs && !errors.Is(err, ErrJWKSKeyRejected) || !tc.rejectsIDs && err != nil {
						t.Fatalf("unexpected duplicate handling: %v", err)
					}
					if err := source.Shutdown(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			},
		)
	}
}

func TestManagedVerificationPolicyPreservesDuplicateFilteringWithoutKeyIDPolicy(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verification := policyJWK(t, &key.PublicKey, "same", "sig", "ES256")
	unrelated := policyJWK(t, &key.PublicKey, "same", "enc", "ES256")
	for _, keys := range [][]map[string]any{{verification, unrelated}, {unrelated, verification}} {
		body := policyDocument(t, keys...)
		source, err := NewManagedKeySourceJWKS(
			"https://example.test/keys",
			&JWKSOptions{
				Client: concurrencyClient(
					func(*http.Request) (*http.Response, error) {
						return responseWithBody(body), nil
					},
				),
				VerificationPolicy: &JWKSVerificationPolicy{
					AllowedAlgorithms: []string{"ES256"},
				},
				RefreshInterval: time.Hour,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := source.Start(context.Background()); err != nil {
			t.Fatalf("published verification policy behavior changed: %v", err)
		}
		if _, err := source.FetchPublicKeyForAlgorithm(context.Background(), "same", "ES256"); err != nil {
			t.Fatalf("published verification lookup behavior changed: %v", err)
		}
		if err := source.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKeyIDPolicyDefaultsAndConfiguration(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	body := []byte(policyDocument(t, policyJWK(t, &key.PublicKey, "", "sig", "ES256")))
	if _, err := NewStaticJWKS(body, policy, JWKSKeyIDPolicy{}); err != nil {
		t.Fatalf("default must preserve optional key IDs: %v", err)
	}
	if _, err := NewStaticJWKS(
		body,
		policy,
		JWKSKeyIDPolicy{
			Required: true,
		},
	); !errors.Is(err, ErrJWKSKeyRejected) {
		t.Fatalf("required key ID not enforced: %v", err)
	}
	if _, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			KeyIDPolicy: JWKSKeyIDPolicy{
				Required: true,
			},
		},
	); err == nil {
		t.Fatal("key ID policy without verification policy accepted")
	}
	if _, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			VerificationPolicy: &policy,
			KeyIDPolicy: JWKSKeyIDPolicy{
				MaxBytes: -1,
			},
		},
	); err == nil {
		t.Fatal("negative key ID limit accepted")
	}
}

func TestStaticJWKSRejectsDuplicatesRegardlessOfManagedPolicy(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := policyJWK(t, &key.PublicKey, "same", "sig", "ES256")
	body := []byte(policyDocument(t, public, public))
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	for _, rejectDuplicates := range []bool{false, true} {
		_, err := NewStaticJWKS(
			body,
			policy,
			JWKSKeyIDPolicy{
				RejectDuplicates: rejectDuplicates,
			},
		)
		var diagnostic *JWKSError
		if !errors.Is(err, ErrJWKSKeyRejected) || !errors.As(err, &diagnostic) || diagnostic.Field() != "keys[1].kid" {
			t.Fatalf("duplicate ID not rejected with RejectDuplicates=%v: %v", rejectDuplicates, err)
		}
	}
}
