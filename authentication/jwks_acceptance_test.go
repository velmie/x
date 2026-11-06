package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreshCheckUsesSameKeyPolicyWithoutChangingCacheOrLogging(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	known := policyJWK(t, &key.PublicKey, "known", "sig", "ES256")
	unrelated := policyJWK(t, &key.PublicKey, "other", "enc", "ES256")
	duplicate := policyJWK(t, &key.PublicKey, "known", "enc", "ES256")
	documents := make(chan string, 3)
	documents <- policyDocument(t, known)
	documents <- policyDocument(t, known, unrelated)
	documents <- policyDocument(t, known, duplicate)
	var diagnostics atomic.Int32
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(*http.Request) (*http.Response, error) {
					return responseWithBody(<-documents), nil
				},
			),
			VerificationPolicy: &JWKSVerificationPolicy{
				AllowedAlgorithms: []string{"ES256"},
			},
			KeyIDPolicy: JWKSKeyIDPolicy{
				Required:         true,
				MaxBytes:         128,
				RejectDuplicates: true,
			},
			RefreshInterval: time.Hour,
			DiagnosticFunc: func(*JWKSError) {
				diagnostics.Add(1)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); err != nil {
		t.Fatalf("fresh set with unrelated key rejected: %v", err)
	}
	var diagnostic *JWKSError
	if err := source.Check(context.Background()); !errors.Is(err, ErrJWKSKeyRejected) ||
		!errors.As(err, &diagnostic) || diagnostic.Field() != "keys[1].kid" {
		t.Fatalf("fresh duplicate escaped policy: %v", err)
	}
	if _, err := source.FetchPublicKeyForAlgorithm(context.Background(), "known", "ES256"); err != nil {
		t.Fatalf("fresh checks replaced or invalidated cache: %v", err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "other"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("fresh check warmed cache: %v", err)
	}
	if diagnostics.Load() != 0 {
		t.Fatal("fresh check emitted a diagnostic callback")
	}
}

func TestManagedJWKSKeyIDPolicyIsCopiedAndEnforced(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		kid  string
		want bool
	}{
		{
			name: "missing",
		},
		{
			name: "exact byte bound",
			kid:  "1234",
			want: true,
		},
		{
			name: "above byte bound",
			kid:  "12345",
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				policy := JWKSVerificationPolicy{
					AllowedAlgorithms: []string{"ES256"},
				}
				keyIDs := JWKSKeyIDPolicy{
					Required: true,
					MaxBytes: 4,
				}
				body := policyDocument(t, policyJWK(t, &key.PublicKey, tc.kid, "sig", "ES256"))
				source, err := NewManagedKeySourceJWKS(
					"https://example.test/keys",
					&JWKSOptions{
						Client: concurrencyClient(
							func(*http.Request) (*http.Response, error) {
								return responseWithBody(body), nil
							},
						),
						VerificationPolicy: &policy,
						KeyIDPolicy:        keyIDs,
						RefreshInterval:    time.Hour,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				defer source.Shutdown(context.Background())
				keyIDs.Required = false
				keyIDs.MaxBytes = 0
				err = source.Start(context.Background())
				if tc.want {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var diagnostic *JWKSError
				if !errors.Is(err, ErrJWKSKeyRejected) || errors.Is(err, ErrKeyRejected) ||
					!errors.As(err, &diagnostic) || diagnostic.Field() != "keys[0].kid" {
					t.Fatalf("remote key ID bound not enforced: %v", err)
				}
			},
		)
	}
}

func TestStaticJWKSRejectsPrivateAndSymmetricKeys(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	keyIDs := JWKSKeyIDPolicy{
		Required: true,
	}
	for _, jwk := range []map[string]any{
		policyJWK(t, key, "private", "sig", "ES256"),
		policyJWK(t, []byte(strings.Repeat("s", 32)), "symmetric", "sig", ""),
	} {
		_, err := NewStaticJWKS([]byte(policyDocument(t, jwk)), policy, keyIDs)
		if !errors.Is(err, ErrJWKSKeyRejected) || errors.Is(err, ErrKeyRejected) {
			t.Fatalf("private or symmetric material accepted: %v", err)
		}
	}
}

func TestManagedJWKSReportsProviderCancellationWhenLifetimeIsLive(t *testing.T) {
	diagnostics := make(chan *JWKSError, 1)
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(*http.Request) (*http.Response, error) {
					return nil, context.Canceled
				},
			),
			AllowInitialFetchFailure: true,
			RefreshInterval:          time.Hour,
			DiagnosticFunc: func(diagnostic *JWKSError) {
				diagnostics <- diagnostic
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("independent provider cancellation stopped lifecycle: %v", err)
	}
	select {
	case diagnostic := <-diagnostics:
		if !errors.Is(diagnostic, context.Canceled) || diagnostic.Stage() != "request" {
			t.Fatalf("provider cause was lost: %v", diagnostic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("initial provider failure was not reported")
	}
}
