package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
)

func TestJWKSVerificationPolicyRejectsAmbiguousAndInvalidKeys(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := policyJWK(t, &ec.PublicKey, "known", "sig", "ES256")
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second := policyJWK(t, &other.PublicKey, "known", "sig", "ES256")
	cases := []struct {
		name string
		keys []map[string]any
	}{
		{"duplicate", []map[string]any{public, second}},
		{"duplicate_reversed", []map[string]any{second, public}},
		{"private", []map[string]any{policyJWK(t, ec, "known", "sig", "ES256")}},
		{"symmetric", []map[string]any{policyJWK(t, []byte("test-only-key"), "known", "sig", "HS256")}},
		{"zero_rsa", []map[string]any{{"kty": "RSA", "kid": "known", "n": "AA", "e": "AA", "alg": "RS256"}}},
		{"bad_ec", []map[string]any{{"kty": "EC", "kid": "known", "crv": "P-256", "x": "AA", "y": "AA", "alg": "ES256"}}},
		{"short_ed25519", []map[string]any{{"kty": "OKP", "kid": "known", "crv": "Ed25519", "x": "AA", "alg": "EdDSA"}}},
		{"long_ed25519", []map[string]any{{"kty": "OKP", "kid": "known", "crv": "Ed25519", "x": strings.Repeat("A", 44), "alg": "EdDSA"}}},
		{"encryption_only", []map[string]any{policyJWK(t, &ec.PublicKey, "known", "enc", "ES256")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := policySource(t, policyDocument(t, tc.keys...), []string{"ES256", "RS256", "EdDSA"})
			if err := source.Start(context.Background()); !errors.Is(err, ErrJWKSKeyRejected) {
				t.Fatalf("accepted invalid snapshot: %v", err)
			}
		})
	}
}

func TestJWKSVerificationPolicyUsageAndMetadata(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		change   func(map[string]any)
		accepted bool
	}{
		{"absent_optional_metadata", func(k map[string]any) { delete(k, "use"); delete(k, "alg"); delete(k, "kid") }, true},
		{"verify_operation", func(k map[string]any) { k["key_ops"] = []string{"verify"} }, true},
		{"decrypt_operation", func(k map[string]any) { delete(k, "use"); k["key_ops"] = []string{"decrypt"} }, false},
		{"inconsistent_usage", func(k map[string]any) { k["key_ops"] = []string{"decrypt"} }, false},
		{"duplicate_operations", func(k map[string]any) { k["key_ops"] = []string{"verify", "verify"} }, false},
		{"null_operations", func(k map[string]any) { k["key_ops"] = nil }, false},
		{"null_operation_item", func(k map[string]any) { delete(k, "use"); k["key_ops"] = []any{"verify", nil} }, false},
		{"unrelated_operations", func(k map[string]any) { delete(k, "use"); k["key_ops"] = []string{"verify", "decrypt"} }, false},
		{"wrong_operations_type", func(k map[string]any) { k["key_ops"] = "verify" }, false},
		{"wrong_use_type", func(k map[string]any) { k["use"] = 1 }, false},
		{"null_algorithm", func(k map[string]any) { k["alg"] = nil }, false},
		{"incompatible_curve_algorithm", func(k map[string]any) { k["alg"] = "ES384" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jwk := policyJWK(t, &key.PublicKey, "known", "sig", "ES256")
			tc.change(jwk)
			source := policySource(t, policyDocument(t, jwk), []string{"ES256", "ES384"})
			err := source.Start(context.Background())
			if tc.accepted {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrJWKSKeyRejected) {
				t.Fatalf("metadata not rejected: %v", err)
			}
		})
	}
}

func TestJWKSVerificationPolicyBindsTokenAlgorithm(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	source := policySource(t, policyDocument(t, policyJWK(t, &key.PublicKey, "known", "sig", "RS256")), []string{"RS256", "PS256"})
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	method := NewViaJWT(NewJWTv5Parser(jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "PS256"}))), source)
	for _, signing := range []jwt.SigningMethod{jwt.SigningMethodRS256, jwt.SigningMethodPS256} {
		token := jwt.NewWithClaims(signing, jwt.MapClaims{"sub": "fixture"})
		token.Header["kid"] = "known"
		encoded, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = method.Authenticate(context.Background(), encoded)
		if signing == jwt.SigningMethodRS256 {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("algorithm binding bypassed: %v", err)
		}
	}
	if _, err := source.FetchPublicKeyForAlgorithm(context.Background(), "known", ""); !errors.Is(err, ErrJWKSKeyRejected) {
		t.Fatal(err)
	}
	for _, algorithms := range [][]string{{"RS256"}, {"RS256", "PS256"}} {
		jwk := policyJWK(t, &key.PublicKey, "known", "sig", "")
		source := policySource(t, policyDocument(t, jwk), algorithms)
		err := source.Start(context.Background())
		if len(algorithms) == 1 {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("ambiguous missing alg accepted: %v", err)
		}
	}
	jwk := policyJWK(t, &key.PublicKey, "known", "sig", "RS256")
	jwk["e"] = "AQAAAAAAAQAB" // An oversized exponent must not wrap to 65537.
	source = policySource(t, policyDocument(t, jwk), []string{"RS256"})
	if err := source.Start(context.Background()); !errors.Is(err, ErrJWKSKeyRejected) {
		t.Fatalf("oversized exponent accepted: %v", err)
	}
}

func TestJWKSVerificationPolicyKeepsRejectedIDsDistinctFromAbsence(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source := policySource(t, policyDocument(t,
		policyJWK(t, &key.PublicKey, "known", "sig", "ES256"),
		policyJWK(t, &key.PublicKey, "encryption", "enc", "ES256")), []string{"ES256"})
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "encryption"); !errors.Is(err, ErrJWKSKeyRejected) || errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "absent"); err != ErrKeyNotFound {
		t.Fatal(err)
	}
}

func TestJWKSVerificationPolicyFailureRetainsSnapshotAndAge(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clock := newJWKSClock()
	valid := policyJWK(t, &key.PublicKey, "known", "sig", "ES256")
	documents := make(chan string, 5)
	documents <- policyDocument(t, valid)
	documents <- policyDocument(t, valid, valid)
	documents <- policyDocument(t, policyJWK(t, &key.PublicKey, "excluded", "enc", "ES256"))
	documents <- `{"keys":[]}`
	documents <- `{"keys":[]}`
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		RefreshInterval: time.Hour, MaxCacheAge: time.Minute, RequestOnUnknownKID: true,
		VerificationPolicy: &JWKSVerificationPolicy{AllowedAlgorithms: []string{"ES256"}},
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(<-documents))}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	source.now = clock.Now
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(30 * time.Second)
	for i := 0; i < 2; i++ {
		if _, err := source.FetchPublicKey(context.Background(), "absent"); !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatal(err)
		}
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(30 * time.Second)
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySetExpired) {
		t.Fatalf("invalid snapshot extended age: %v", err)
	}
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != ErrKeyNotFound {
		t.Fatal("valid empty snapshot did not clear keys", err)
	}
}

func TestJWKSVerificationPolicyAcceptsEd25519AndPreservesLegacy(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source := policySource(t, policyDocument(t, policyJWK(t, public, "known", "sig", "EdDSA")), []string{"EdDSA"})
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"sub": "fixture"})
	token.Header["kid"] = "known"
	raw, err := token.SignedString(private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewViaJWT(NewJWTv5Parser(jwt.NewParser()), source).Authenticate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []any{[]byte("test-only-key"), private} {
		body := policyDocument(t, policyJWK(t, key, "known", "sig", ""))
		legacy := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		defer legacy.Stop()
		if _, err := legacy.FetchPublicKeyForAlgorithm(context.Background(), "known", "ignored"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJWKSVerificationPolicyRejectsInvalidEd25519Points(t *testing.T) {
	for _, tc := range []struct {
		name, coordinate string
	}{
		{"identity", "01" + strings.Repeat("00", 31)},
		{"small_order", strings.Repeat("00", 32)},
		{"off_curve", "ef" + strings.Repeat("ff", 30) + "7f"},
		{"noncanonical", "f6" + strings.Repeat("ff", 30) + "7f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coordinate, err := hex.DecodeString(tc.coordinate)
			if err != nil {
				t.Fatal(err)
			}
			key := map[string]any{"kty": "OKP", "kid": "known", "crv": "Ed25519", "alg": "EdDSA", "x": base64.RawURLEncoding.EncodeToString(coordinate)}
			source := policySource(t, policyDocument(t, key), []string{"EdDSA"})
			if err := source.Start(context.Background()); !errors.Is(err, ErrJWKSKeyRejected) {
				t.Fatalf("invalid Ed25519 point accepted: %v", err)
			}
		})
	}
}

func TestJWKSVerificationPolicyConfiguration(t *testing.T) {
	for _, algorithms := range [][]string{nil, {}, {"none"}, {"HS256"}, {"unknown"}} {
		opts := &JWKSOptions{VerificationPolicy: &JWKSVerificationPolicy{AllowedAlgorithms: algorithms}}
		if _, err := NewManagedKeySourceJWKS("https://example.test/keys", opts); !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("bad policy accepted: %v", err)
		}
		legacy := NewKeySourceJWKS("https://example.test/keys", opts)
		if _, err := legacy.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("invalid policy masked as absence: %v", err)
		}
		legacy.Stop()
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	algorithms := []string{"ES256"}
	source := policySource(t, policyDocument(t, policyJWK(t, &key.PublicKey, "known", "sig", "ES256")), algorithms)
	algorithms[0] = "RS256"
	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("caller mutation changed policy: %v", err)
	}
}

func policyJWK(t *testing.T, key any, kid, use, alg string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(jose.JSONWebKey{Key: key, KeyID: kid, Use: use, Algorithm: alg})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func policyDocument(t *testing.T, keys ...map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func policySource(t *testing.T, body string, algorithms []string) *KeySourceJWKS {
	t.Helper()
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		RefreshInterval: time.Hour, VerificationPolicy: &JWKSVerificationPolicy{AllowedAlgorithms: algorithms},
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Stop)
	return source
}
