package authx_test

import (
	"context"
	"crypto"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestErrorAdapterPreservesParserCauses(t *testing.T) {
	dependency := &adapterDependencyError{}
	key := []byte("test-signing-key")
	cases := []struct {
		name     string
		claims   jwt.MapClaims
		keyError error
		category error
		cause    error
	}{
		{name: "expired", claims: jwt.MapClaims{"exp": int64(1)}, category: authx.ErrNotAuthenticated, cause: jwt.ErrTokenExpired},
		{name: "missing key", keyError: authentication.ErrKeyNotFound, category: authx.ErrNotAuthenticated, cause: authentication.ErrKeyNotFound},
		{name: "canceled lookup", keyError: context.Canceled, category: authx.ErrBadToken, cause: context.Canceled},
		{name: "dependency", keyError: dependency, category: authx.ErrBadToken, cause: dependency},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := tc.claims
			if claims == nil {
				claims = jwt.MapClaims{"sub": "subject"}
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			parser := authentication.NewJWTv5Parser(jwt.NewParser(jwt.WithTimeFunc(func() time.Time { return time.Unix(1700000000, 0) })))
			keys := authentication.KeySourceFunc(func(context.Context, string) (crypto.PublicKey, error) { return key, tc.keyError })
			method := authx.NewErrorAdapter(authentication.NewViaJWT(parser, keys))
			_, err = method.Authenticate(context.Background(), token)
			if !errors.Is(err, tc.category) || !errors.Is(err, tc.cause) {
				t.Fatalf("category or cause lost: %v", err)
			}
			if errors.Unwrap(err) != tc.category {
				t.Errorf("single unwrap category changed: %v", errors.Unwrap(err))
			}
			if tc.keyError == dependency {
				var typed *adapterDependencyError
				if !errors.As(err, &typed) || typed != dependency {
					t.Errorf("typed cause lost: %v", err)
				}
			}
		})
	}
}

type adapterDependencyError struct{}

func (*adapterDependencyError) Error() string { return "key dependency unavailable" }
