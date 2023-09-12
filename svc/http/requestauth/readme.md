# HTTP Auth package

This package provides functionality that can be used to implement an authorization middleware. It offers primitives with
which an authorization layer can be constructed.

## Example

```go
package main

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/velmie/x/svc/http/requestauth"
	"net/http"
)

func main() {

	tokenExtractor := &FakeTokenExtractor{}
	authMethod := &FakeMethod{}
	injector := &FakeAuthInjector{}

	authPipeline := requestauth.NewPipeline(tokenExtractor, authMethod, injector)

	httpHandlerExample := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestWithInjectedAuth, err := authPipeline(w, r)
		if err != nil {
			// handle error
		}

		// call some next handler
		// next.ServeHTTP(w, requestWithInjectedAuth)
	})

	ginHandlerExample := gin.HandlerFunc(func(c *gin.Context) {
		requestWithInjectedAuth, err := authPipeline(w, r)
		if err != nil {
			// handle error
		}

		// set authenticated request into gin.Context
		c.Request = requestWithInjectedAuth
		// ...
	})

}

type FakeAuthInjector struct{}

func (f FakeAuthInjector) InjectAuth(entity requestauth.Entity, w http.ResponseWriter, r *http.Request) (*http.Request, error) {
	// do something with the request to inject authenticated entity...
	ctx := r.Context()

	// ctx = context.WithValue(ctx, ...)

	// use r.WithContext...
	return r.WithContext(ctx), nil
}

type FakeTokenExtractor struct{}

func (f FakeTokenExtractor) Extract(r *http.Request) (token string, err error) {
	// do something with the request to extract token
	return "fake-token", nil
}

type FakeMethod struct{}

func (FakeMethod) Authenticate(ctx context.Context, token string) (requestauth.Entity, error) {
	// this just example
	// see https://github.com/velmie/x/tree/main/svc/authx for JWT auth method
	return requestauth.Entity{
		"id":   "example-id",
		"role": "admin",
		"name": "Some User",
	}, nil
}
```