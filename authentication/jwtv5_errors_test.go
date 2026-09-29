package authentication_test

import (
	"context"
	"crypto"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
)

func TestJWTv5ParserErrorCauses(t *testing.T) {
	key := []byte("test-signing-key")
	dependency := &keyDependencyError{}
	now := time.Unix(1700000000, 0)
	tests := []struct {
		name           string
		claims         jwt.MapClaims
		options        []jwt.ParserOption
		keyError       error
		malformed      bool
		wrongSignature bool
		category       error
		causes         []error
	}{
		{name: "expired", claims: jwt.MapClaims{"exp": now.Add(-time.Hour).Unix()}, category: authentication.ErrNotAuthenticated, causes: []error{jwt.ErrTokenExpired}},
		{name: "not yet valid", claims: jwt.MapClaims{"nbf": now.Add(time.Hour).Unix()}, category: authentication.ErrNotAuthenticated, causes: []error{jwt.ErrTokenNotValidYet}},
		{name: "issuer", claims: jwt.MapClaims{"iss": "other"}, options: []jwt.ParserOption{jwt.WithIssuer("expected")}, category: authentication.ErrNotAuthenticated, causes: []error{jwt.ErrTokenInvalidIssuer}},
		{name: "audience", claims: jwt.MapClaims{"aud": "other"}, options: []jwt.ParserOption{jwt.WithAudience("expected")}, category: authentication.ErrNotAuthenticated, causes: []error{jwt.ErrTokenInvalidAudience}},
		{name: "multiple claims", claims: jwt.MapClaims{"exp": now.Add(-time.Hour).Unix(), "nbf": now.Add(time.Hour).Unix()}, category: authentication.ErrNotAuthenticated, causes: []error{jwt.ErrTokenExpired, jwt.ErrTokenNotValidYet}},
		{name: "malformed", malformed: true, category: authentication.ErrBadToken, causes: []error{jwt.ErrTokenMalformed}},
		{name: "signature before claims", claims: jwt.MapClaims{"exp": now.Add(-time.Hour).Unix()}, wrongSignature: true, category: authentication.ErrBadToken, causes: []error{jwt.ErrTokenSignatureInvalid}},
		{name: "missing key", keyError: authentication.ErrKeyNotFound, category: authentication.ErrTokenUnverifiable, causes: []error{authentication.ErrKeyNotFound, jwt.ErrTokenUnverifiable}},
		{name: "cancellation during lookup", keyError: context.Canceled, category: authentication.ErrBadToken, causes: []error{context.Canceled}},
		{name: "deadline during lookup", keyError: context.DeadlineExceeded, category: authentication.ErrBadToken, causes: []error{context.DeadlineExceeded}},
		{name: "typed dependency", keyError: dependency, category: authentication.ErrBadToken, causes: []error{dependency}},
		{name: "unverifiable before nested claims", keyError: errors.Join(jwt.ErrTokenUnverifiable, jwt.ErrTokenExpired), category: authentication.ErrBadToken, causes: []error{jwt.ErrTokenUnverifiable, jwt.ErrTokenExpired}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := tc.claims
			if claims == nil {
				claims = jwt.MapClaims{"sub": "subject"}
			}
			signingKey := key
			if tc.wrongSignature {
				signingKey = []byte("different-key")
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			if tc.malformed {
				token = "..."
			}
			opts := append([]jwt.ParserOption{jwt.WithTimeFunc(func() time.Time { return now })}, tc.options...)
			parser := authentication.NewJWTv5Parser(jwt.NewParser(opts...))
			keys := authentication.KeySourceFunc(func(context.Context, string) (crypto.PublicKey, error) { return key, tc.keyError })
			for _, viaMethod := range []bool{false, true} {
				var got error
				if viaMethod {
					_, got = authentication.NewViaJWT(parser, keys).Authenticate(context.Background(), token)
				} else {
					_, got = parser.Parse(context.Background(), token, keys)
				}
				if !errors.Is(got, tc.category) {
					t.Errorf("viaMethod=%v category: got %v, want %v", viaMethod, got, tc.category)
				}
				for _, cause := range tc.causes {
					if !errors.Is(got, cause) {
						t.Errorf("viaMethod=%v missing cause %v in %v", viaMethod, cause, got)
					}
				}
				if tc.keyError == dependency {
					var typed *keyDependencyError
					if !errors.As(got, &typed) || typed != dependency {
						t.Errorf("typed cause lost: %v", got)
					}
				}
				if !viaMethod && tc.category != authentication.ErrTokenUnverifiable && errors.Unwrap(got) != tc.category {
					t.Errorf("single unwrap category changed: %v", errors.Unwrap(got))
				}
			}
		})
	}
}

type keyDependencyError struct{}

func (*keyDependencyError) Error() string { return "key dependency unavailable" }
