package main

import (
	"context"
	"fmt"
	"github.com/velmie/x/svc/authentication"
	"net/url"
	"time"
)

func main() {
	jwksURL, _ := url.Parse("https://example.com/.well-known/jwks.json")

	ready := make(chan struct{})

	auth, err := authentication.NewJWTMethod(
		authentication.WithJWKSSource(jwksURL),
		authentication.WithJWKSSourceReadySignal(ready),
	)

	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		// timeout error
	}

	entity, err := auth.Authenticate(context.Background(), "some-jwt")
	if err != nil {
		// ...
	}

	fmt.Println(entity) // map[string]any filled with JWT claims
}
}
