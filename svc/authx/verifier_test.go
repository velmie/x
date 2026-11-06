package authx_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
	"github.com/velmie/x/svc/identity"
)

func TestVerifierClaimsAndPermissions(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := verifierPolicy("EdDSA")
	verifier := newVerifier(t, authentication.KeySourceMap{"key": public}, policy, authx.ClaimLimits{})
	cases := []struct {
		name   string
		change func(jwt.MapClaims)
		want   string
	}{
		{
			name:   "missing issuer",
			change: func(c jwt.MapClaims) { delete(c, "iss") },
			want:   "iss",
		},
		{
			name:   "unknown issuer",
			change: func(c jwt.MapClaims) { c["iss"] = "other" },
			want:   "iss",
		},
		{
			name:   "missing subject",
			change: func(c jwt.MapClaims) { delete(c, "sub") },
			want:   "sub",
		},
		{
			name:   "blank subject",
			change: func(c jwt.MapClaims) { c["sub"] = " \t" },
			want:   "sub",
		},
		{
			name:   "long subject",
			change: func(c jwt.MapClaims) { c["sub"] = strings.Repeat("é", 65) },
			want:   "sub",
		},
		{
			name:   "missing audience",
			change: func(c jwt.MapClaims) { delete(c, "aud") },
			want:   "aud",
		},
		{
			name:   "wrong audience",
			change: func(c jwt.MapClaims) { c["aud"] = "other" },
			want:   "aud",
		},
		{
			name:   "missing expiration",
			change: func(c jwt.MapClaims) { delete(c, "exp") },
			want:   "exp",
		},
		{
			name:   "bad expiration",
			change: func(c jwt.MapClaims) { c["exp"] = "tomorrow" },
			want:   "exp",
		},
		{
			name:   "future not before",
			change: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() },
			want:   "not valid yet",
		},
		{
			name:   "permission list type",
			change: func(c jwt.MapClaims) { c["permissions"] = "read" },
			want:   "permissions",
		},
		{
			name:   "null permissions",
			change: func(c jwt.MapClaims) { c["permissions"] = nil },
			want:   "permissions",
		},
		{
			name:   "permission item type",
			change: func(c jwt.MapClaims) { c["permissions"] = []any{"read", 42} },
			want:   "permissions[1]",
		},
		{
			name:   "empty permission",
			change: func(c jwt.MapClaims) { c["permissions"] = []string{""} },
			want:   "permissions[0]",
		},
		{
			name:   "padded permission",
			change: func(c jwt.MapClaims) { c["permissions"] = []string{" read"} },
			want:   "permissions[0]",
		},
		{
			name:   "long permission",
			change: func(c jwt.MapClaims) { c["permissions"] = []string{strings.Repeat("é", 33)} },
			want:   "permissions[0]",
		},
		{
			name:   "too many permissions",
			change: func(c jwt.MapClaims) { c["permissions"] = make([]string, identity.MaxPermissions+1) },
			want:   "permissions",
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				claims := verifierClaims()
				tc.change(claims)
				raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims)
				principal, err := verifier.Authenticate(context.Background(), raw)
				if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() || principal.Subject != "" || principal.Permissions != nil {
					t.Fatalf("principal=%+v err=%v", principal, err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("diagnostic missing field/rule %q: %v", tc.want, err)
				}
				if strings.Contains(err.Error(), raw) || strings.Contains(err.Error(), " subject-secret ") {
					t.Fatalf("diagnostic exposed credential or subject: %v", err)
				}
			},
		)
	}
	claims := verifierClaims()
	claims["aud"] = []string{"other", "alternative"}
	claims["iat"] = time.Now().Add(time.Hour).Unix()
	permissions := make([]string, identity.MaxPermissions)
	for i := range permissions {
		permissions[i] = strings.Repeat("p", identity.MaxPermissionBytes)
	}
	claims["permissions"] = permissions
	principal, err := verifier.Authenticate(
		context.Background(),
		verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims),
	)
	if err != nil || principal.Issuer != " issuer " || principal.Subject != " subject-secret " || len(principal.Permissions) != 32 {
		t.Fatalf("exact identity or upper bounds lost: %+v, %v", principal, err)
	}
}

func TestVerifierLeewayAndTrustDomains(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := verifierPolicy("EdDSA")
	keys := authentication.KeySourceMap{"key": public}
	strict := newVerifier(t, keys, policy, authx.ClaimLimits{})
	claims := verifierClaims()
	claims["nbf"] = time.Now().Add(30 * time.Second).Unix()
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims)
	if _, err := strict.Authenticate(context.Background(), raw); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	policy.Leeway = time.Minute
	flexible := newVerifier(t, keys, policy, authx.ClaimLimits{})
	if _, err := flexible.Authenticate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	claims["nbf"] = time.Now().Add(-time.Hour).Unix()
	claims["exp"] = time.Now().Add(-30 * time.Second).Unix()
	raw = verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims)
	if _, err := strict.Authenticate(context.Background(), raw); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, err := flexible.Authenticate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPolicy := verifierPolicy("EdDSA")
	otherPolicy.Issuers = []string{"other issuer"}
	other := newVerifier(t, authentication.KeySourceMap{"key": otherPublic}, otherPolicy, authx.ClaimLimits{})
	claims = verifierClaims()
	claims["iss"] = "other issuer"
	raw = verifierToken(t, jwt.SigningMethodEdDSA, private, "key", claims)
	if _, err := other.Authenticate(context.Background(), raw); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("independent trust domain accepted foreign key", err)
	}
}

func TestVerifierAllSupportedKeyFamilies(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ec256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ec384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ec521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPublic, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method  jwt.SigningMethod
		public  crypto.PublicKey
		private any
	}{
		{
			method:  jwt.SigningMethodRS256,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodRS384,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodRS512,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodPS256,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodPS384,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodPS512,
			public:  &rsaKey.PublicKey,
			private: rsaKey,
		},
		{
			method:  jwt.SigningMethodES256,
			public:  &ec256.PublicKey,
			private: ec256,
		},
		{
			method:  jwt.SigningMethodES384,
			public:  &ec384.PublicKey,
			private: ec384,
		},
		{
			method:  jwt.SigningMethodES512,
			public:  &ec521.PublicKey,
			private: ec521,
		},
		{
			method:  jwt.SigningMethodEdDSA,
			public:  edPublic,
			private: edPrivate,
		},
	} {
		t.Run(
			tc.method.Alg(),
			func(t *testing.T) {
				verifier := newVerifier(t, authentication.KeySourceMap{"key": tc.public}, verifierPolicy(tc.method.Alg()), authx.ClaimLimits{})
				raw := verifierToken(t, tc.method, tc.private, "key", verifierClaims())
				if principal, err := verifier.Authenticate(context.Background(), raw); err != nil || !principal.Valid() {
					t.Fatalf("principal=%+v err=%v", principal, err)
				}
			},
		)
	}
}

func TestVerifierSourceClassificationAndAlgorithmForwarding(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", verifierClaims())
	dependency := &verifierDependencyError{}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{
			name: "missing",
			err:  authentication.ErrKeyNotFound,
			want: identity.ErrInvalidCredentials,
		},
		{
			name: "rejected",
			err:  authentication.ErrKeyRejected,
			want: identity.ErrInvalidCredentials,
		},
		{
			name: "rate-limited confirmed miss",
			err:  fmt.Errorf("%w: %w", authentication.ErrKeyNotFound, authentication.ErrJWKSRateLimited),
			want: identity.ErrInvalidCredentials,
		},
		{
			name: "invalid provider document",
			err:  fmt.Errorf("provider document: %w", authentication.ErrJWKSKeyRejected),
			want: identity.ErrUnavailable,
		},
		{
			name: "dependency deadline with live request",
			err:  context.DeadlineExceeded,
			want: identity.ErrUnavailable,
		},
		{
			name: "dependency",
			err:  dependency,
			want: identity.ErrUnavailable,
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				source := &verifierAwareSource{
					key: public,
					err: tc.err,
				}
				verifier := newVerifier(t, source, verifierPolicy("EdDSA"), authx.ClaimLimits{})
				principal, err := verifier.Authenticate(context.Background(), raw)
				if !errors.Is(err, tc.want) || !errors.Is(err, tc.err) || principal.Valid() || source.algorithm != "EdDSA" {
					t.Fatalf("principal=%+v algorithm=%q error=%v", principal, source.algorithm, err)
				}
				if tc.err == dependency {
					var actual *verifierDependencyError
					if !errors.As(err, &actual) || actual != dependency {
						t.Fatalf("dependency type lost: %v", err)
					}
				}
			},
		)
	}
}

func TestVerifierCancellationRetainsSourceFailure(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dependency := &verifierDependencyError{}
	source := authentication.KeySourceFunc(
		func(context.Context, string) (crypto.PublicKey, error) {
			cancel()
			return nil, dependency
		},
	)
	verifier := newVerifier(t, source, verifierPolicy("EdDSA"), authx.ClaimLimits{})
	raw := verifierToken(t, jwt.SigningMethodEdDSA, private, "key", verifierClaims())
	principal, err := verifier.Authenticate(ctx, raw)
	var cause *verifierDependencyError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &cause) || cause != dependency || principal.Valid() {
		t.Fatalf("cancellation lost source failure: %v", err)
	}
}

func TestVerifierConfigurationAndInputLimits(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := authentication.KeySourceMap{"key": public}
	for _, algorithm := range []string{"none", "HS256", "unknown"} {
		if err := verifierPolicy(algorithm).Validate(); err == nil {
			t.Fatalf("accepted algorithm %q", algorithm)
		}
	}
	for _, policy := range []authx.TrustPolicy{
		{},
		{
			Algorithms: []string{"EdDSA"},
			Issuers:    []string{"issuer"},
			Audiences:  []string{"audience"},
			Leeway:     -time.Second,
		},
	} {
		if _, err := authx.NewVerifier(keys, policy, authx.ClaimLimits{}); err == nil {
			t.Fatalf("accepted invalid policy: %+v", policy)
		}
	}
	for _, limits := range []authx.ClaimLimits{
		{
			MaxTokenBytes: -1,
		},
		{
			MaxKeyIDBytes: -1,
		},
		{
			MaxIssuerBytes: -1,
		},
		{
			MaxSubjectBytes: -1,
		},
		{
			MaxPermissions: -1,
		},
		{
			MaxPermissionBytes: -1,
		},
		{
			MaxTokenBytes: authx.DefaultMaxTokenBytes + 1,
		},
		{
			MaxKeyIDBytes: authx.DefaultMaxKeyIDBytes + 1,
		},
		{
			MaxIssuerBytes: identity.MaxIssuerBytes + 1,
		},
		{
			MaxSubjectBytes: identity.MaxSubjectBytes + 1,
		},
		{
			MaxPermissions: identity.MaxPermissions + 1,
		},
		{
			MaxPermissionBytes: identity.MaxPermissionBytes + 1,
		},
	} {
		if _, err := authx.NewVerifier(keys, verifierPolicy("EdDSA"), limits); err == nil {
			t.Fatalf("accepted invalid limits: %+v", limits)
		}
	}
	verifier := newVerifier(t, keys, verifierPolicy("EdDSA"), authx.ClaimLimits{})
	for _, kid := range []any{"", strings.Repeat("k", authx.DefaultMaxKeyIDBytes+1), 42} {
		raw := verifierToken(t, jwt.SigningMethodEdDSA, private, kid, verifierClaims())
		principal, err := verifier.Authenticate(context.Background(), raw)
		if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() || !strings.Contains(err.Error(), "kid") {
			t.Fatalf("bad kid was not rejected: %v", err)
		}
	}
	principal, err := verifier.Authenticate(context.Background(), strings.Repeat("a", authx.DefaultMaxTokenBytes+1))
	if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() {
		t.Fatalf("oversized token accepted: %v", err)
	}
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodNone} {
		var signingKey any = []byte("test secret")
		if method == jwt.SigningMethodNone {
			signingKey = jwt.UnsafeAllowNoneSignatureType
		}
		raw := verifierToken(t, method, signingKey, "key", verifierClaims())
		principal, err := verifier.Authenticate(context.Background(), raw)
		if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() {
			t.Fatalf("disallowed algorithm %s accepted: %v", method.Alg(), err)
		}
	}
}

func TestVerifierRejectsInvalidClaimsUTF8(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(t, authentication.KeySourceMap{"key": public}, verifierPolicy("EdDSA"), authx.ClaimLimits{})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","kid":"key"}`))
	claims := []byte(
		fmt.Sprintf(
			`{"iss":" issuer ","sub":"%s","aud":"audience","exp":%d}`,
			string([]byte{0xff}),
			time.Now().Add(time.Hour).Unix(),
		),
	)
	encoded := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	signature, err := jwt.SigningMethodEdDSA.Sign(encoded, private)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Authenticate(context.Background(), encoded+"."+base64.RawURLEncoding.EncodeToString(signature))
	if !errors.Is(err, identity.ErrInvalidCredentials) || principal.Valid() || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid claims UTF-8 accepted: %v", err)
	}
}

func verifierPolicy(algorithm string) authx.TrustPolicy {
	return authx.TrustPolicy{
		Algorithms: []string{algorithm},
		Issuers:    []string{" issuer "},
		Audiences:  []string{"audience", "alternative"},
	}
}

func verifierClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": " issuer ",
		"sub": " subject-secret ",
		"aud": "audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func verifierToken(t *testing.T, method jwt.SigningMethod, key any, kid any, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newVerifier(t *testing.T, keys authentication.KeySource, policy authx.TrustPolicy, limits authx.ClaimLimits) *authx.Verifier {
	t.Helper()
	verifier, err := authx.NewVerifier(keys, policy, limits)
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

type verifierAwareSource struct {
	key       crypto.PublicKey
	err       error
	algorithm string
}

func (s *verifierAwareSource) FetchPublicKey(context.Context, string) (crypto.PublicKey, error) {
	return nil, errors.New("algorithm-aware lookup not used")
}

func (s *verifierAwareSource) FetchPublicKeyForAlgorithm(_ context.Context, _ string, algorithm string) (crypto.PublicKey, error) {
	s.algorithm = algorithm
	return s.key, s.err
}

type verifierDependencyError struct{}

func (*verifierDependencyError) Error() string { return "connection refused" }
