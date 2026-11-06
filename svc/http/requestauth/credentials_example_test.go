package requestauth_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/velmie/x/svc/http/requestauth"
	"github.com/velmie/x/svc/identity"
)

func ExampleBearerToken() {
	header := http.Header{}
	header.Set("Authorization", "Bearer example-token")
	token, err := requestauth.BearerToken(header)
	fmt.Println(token, err)
	// Output: example-token <nil>
}

func ExampleStrictBearerTokenExtractor() {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "bearer example-token")
	token, err := (requestauth.StrictBearerTokenExtractor{}).Extract(request)
	fmt.Println(token, err)
	// Output: example-token <nil>
}

func ExampleAuthenticate() {
	header := http.Header{}
	header.Set("Authorization", "Bearer example-token")
	principal, err := requestauth.Authenticate(context.Background(), header, exampleAuthenticator{})
	fmt.Println(principal.Subject, err)
	// Output: user-42 <nil>
}

func ExampleRequire() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	operation := requestauth.PrincipalHandler(
		func(w http.ResponseWriter, _ *http.Request, principal identity.Principal) {
			fmt.Fprint(w, principal.Subject)
		},
	)
	protected := requestauth.Require(
		exampleAuthenticator{},
		logger,
		operation,
	)
	request := httptest.NewRequest(http.MethodGet, "/books", nil)
	request.Header.Set("Authorization", "Bearer example-token")
	writer := httptest.NewRecorder()
	protected.ServeHTTP(writer, request)
	fmt.Println(writer.Code, writer.Body.String())
	// Output: 200 user-42
}

func ExampleWithPrincipal() {
	ctx := requestauth.WithPrincipal(
		context.Background(),
		identity.Principal{
			Issuer:  "https://issuer.example",
			Subject: "user-42",
		},
	)
	fmt.Println(requestauth.Principal(ctx).Subject)
	// Output: user-42
}

func ExamplePrincipalHandler() {
	handler := requestauth.PrincipalHandler(
		func(w http.ResponseWriter, _ *http.Request, principal identity.Principal) {
			fmt.Fprint(w, principal.Subject)
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := requestauth.WithPrincipal(
		request.Context(),
		identity.Principal{
			Issuer:  "https://issuer.example",
			Subject: "user-42",
		},
	)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request.WithContext(ctx))
	fmt.Println(writer.Body.String())
	// Output: user-42
}

type exampleAuthenticator struct{}

func (exampleAuthenticator) Authenticate(_ context.Context, token string) (identity.Principal, error) {
	if token != "example-token" {
		return identity.Principal{}, identity.ErrInvalidCredentials
	}
	return identity.Principal{
		Issuer:  "https://issuer.example",
		Subject: "user-42",
	}, nil
}
