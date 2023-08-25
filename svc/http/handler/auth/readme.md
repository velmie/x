# HTTP Auth package

This package provides functionality that can be used to implement an authorization middleware. It offers primitives with
which an authorization layer can be constructed.

## Example

```go

package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/velmie/x/svc/http/handler/auth"
)

func main() {

	authMethod := AuthMethodExample{}
	successHandler := SuccessHandlerExample{}
	errorHandler := ErrorHandlerExample{}

	authHandler := auth.Handler(
		auth.NewBearerTokenExtractor(),
		authMethod,
		successHandler,
		errorHandler,
		auth.VerifyRequired("role", auth.EqString("admin"), "access is allowed only with the 'admin' role"),
		auth.VerifyRequired("id", auth.Not(auth.EmptyString()), "id is required and cannot be empty"),
	)
	
	// ... use authHandler (http.Handler)
}

type AuthMethodExample struct{}

func (AuthMethodExample) Authenticate(ctx context.Context, token string) (auth.Entity, error) {
	// this just example
	// see https://github.com/velmie/x/tree/main/svc/authentication for JWT auth method
	return auth.Entity{
		"id":   "example-id",
		"role": "admin",
		"name": "Some User",
	}, nil
}

type SuccessHandlerExample struct{}

func (SuccessHandlerExample) HandleSuccess(entity auth.Entity, w http.ResponseWriter, r *http.Request) {
	fmt.Printf("here is authorized entity, you can use it and forward the call: %v", entity)
	// ...
}

type ErrorHandlerExample struct{}

func (ErrorHandlerExample) HandleError(err error, w http.ResponseWriter, r *http.Request) {
	fmt.Printf("oops, something went wrong: %s", err)
	// ...
}
```