package connectx_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/connectx"
	"github.com/velmie/x/svc/http/requestauth"
	"github.com/velmie/x/svc/identity"
)

func ExampleAuthGate() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gate := connectx.AuthGate(exampleGateAuthenticator{}, logger, connectx.ErrorOptions{})
	header := http.Header{}
	header.Set("Authorization", "Bearer example-token")
	ctx, err := gate(context.Background(), connect.Spec{}, connect.Peer{}, header)
	if err != nil {
		panic(err)
	}
	fmt.Println(requestauth.Principal(ctx).Subject)
	// Output: user-42
}

type exampleGateAuthenticator struct{}

func (exampleGateAuthenticator) Authenticate(_ context.Context, token string) (identity.Principal, error) {
	if token != "example-token" {
		return identity.Principal{}, identity.ErrInvalidCredentials
	}
	return identity.Principal{
		Issuer:  "https://issuer.example",
		Subject: "user-42",
	}, nil
}
