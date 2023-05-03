# Authentication

This package provides functionality for authentication tasks.

## Short introduction

The working horse of this package is the `Authenticate(token string) (Entity, error)` method which accepts arbitrary string token and returns a 
set of attributes represented by the `type Entity map[string]any` type.

## JWT authentication

The package provides JSON Web Token (JWT) authentication capabilities through the ViaJWT struct which depends upon
a JWT parser and key source for parsing and validating the token respectively.

Here is an example of how to use the ViaJWT:

```go
package main

import (
	"crypto"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
)

func main() {

	parser := authentication.NewJWTv5Parser(jwt.NewParser())
	keySource := authentication.KeySourceFunc(myPublicKey)

	jwtAuth := authentication.NewViaJWT(parser, keySource)

	const token = "eyJhbGciOiJFUzI1NiIsImtpZCI6IjB5eHE4Z0ViOThTUkxTWGlrNDFZY3p4UGhPcVZ5Ulc0ZG16VjBydVBtbnciLCJ0eXAiOiJ" +
		"KV1QifQ.eyJpYXQiOjE2ODMxMTU2MTQsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.i7vDxB_hUE-08n3vUCngyyiG6" +
		"qvvwR5rl1-vDsyqs5MwuXM8wIuAmPITJ3-JY7wCOxy-oSdZ-_joutqdy80mLg"

	entity, err := jwtAuth.Authenticate(token)
	if err != nil {
		// handle error
		// ...
	}
	fmt.Println(entity["name"]) // John Doe
	//...

}

func myPublicKey(kid string) (crypto.PublicKey, error) {
	var key crypto.PublicKey
	// do something to get the key using the kid (key id)
	// ...
	return key, nil
}
```

### Key Sources

The package provides different key source implementations to fetch public keys for JWT token validation.

#### KeySourceFunc

`KeySourceFunc` is a function type that implements the `KeySource` interface. This allows you to use a simple function
as a key source without having to create a separate struct implementing the `KeySource` interface.

#### KeySourceMap

`KeySourceMap` is a map of key IDs (kids) to public keys, implementing the `KeySource` interface.

#### KeySourceSingle

`KeySourceSingle` is a struct that implements the `KeySource` interface and returns a single public key, regardless of the input key ID (kid). 
This implementation is useful when you have a single public key for token validation.

##### Usage

```go
// Create a KeySourceMap with key IDs and their corresponding public keys
keySource := authentication.KeySourceMap{
	"keyID1": publicKey1,
	"keyID2": publicKey2,
}

// Create a KeySourceSingle with a single public key
keySource := authentication.KeySourceSingle{
	PublicKey: publicKey,
}

// Use a custom KeySourceFunc
keySource := authentication.KeySourceFunc(func(kid string) (crypto.PublicKey, error) {
	// Fetch the public key based on the key ID (kid)
})
```

### JWKS (JSON Web Key Set) Key Source

`KeySourceJWKS` is an implementation of the `KeySource` interface that fetches public keys from a remote JSON Web Key Set (JWKS) endpoint. 
JWKS is a JSON object that represents a set of keys containing the public keys used to verify any JSON Web Token (JWT) issued by the authorization server.


#### Features

- Fetches public keys from a remote JWKS endpoint using HTTP requests.
- Supports request rate limiting and warning functions.
- Allows fetching on unknown key IDs (kids) or not requesting on unknown kids, depending on configuration.
- Caches fetched keys to avoid unnecessary requests.
- Automatically refreshes the keys when the cache expires.

##### Usage

```go
package main

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"net/http"
	"time"
)

func main() {

	parser := authentication.NewJWTv5Parser(jwt.NewParser())
	
	
	jwksOptions := authentication.JWKSOptions{
		Client:              http.DefaultClient, // customize http client (default: http.DefaultClient)
		RefreshInterval:     3 * time.Minute,    // customize keys refresh interval (default: 1 * time.Minute)
		RequestOnUnknownKID: true, // whether to request unknown kid from JWKS endpoint (default: false)
		WarnFunc: func(msg string) {
			fmt.Printf("WARN: %s\n", msg) // optional warning function (default: nil)
		},
	}

	// enable rate limiting for requests to JWKS endpoint (default: no limit)
	jwksOptions.SetRefreshRateLimit(5, time.Minute) // 5 requests per minute
		
	keySource := authentication.NewKeySourceJWKS("https://example.com/.well-known/jwks.json", &jwksOptions)

	jwtAuth := authentication.NewViaJWT(parser, keySource)

	const token = "eyJhbGciOiJFUzI1NiIsImtpZCI6IjB5eHE4Z0ViOThTUkxTWGlrNDFZY3p4UGhPcVZ5Ulc0ZG16VjBydVBtbnciLCJ0eXAiOiJ" +
		"KV1QifQ.eyJpYXQiOjE2ODMxMTU2MTQsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.i7vDxB_hUE-08n3vUCngyyiG6" +
		"qvvwR5rl1-vDsyqs5MwuXM8wIuAmPITJ3-JY7wCOxy-oSdZ-_joutqdy80mLg"

	entity, err := jwtAuth.Authenticate(token)
	if err != nil {
		// handle error
		// ...
	}
	fmt.Println(entity["name"]) // John Doe
	//...

}
```