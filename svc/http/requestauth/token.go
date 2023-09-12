package requestauth

import (
	"fmt"
	"net/http"
)

type BearerTokenExtractor struct{}

func NewBearerTokenExtractor() BearerTokenExtractor {
	return BearerTokenExtractor{}
}

func (BearerTokenExtractor) Extract(r *http.Request) (token string, err error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf(
			"no value present in the Authorization header: %w",
			ErrMissingToken,
		)
	}

	const (
		schema    = "Bearer "
		schemaLen = len(schema)
	)
	if len(authHeader) <= schemaLen {
		return "", fmt.Errorf(
			"the value in the Authorization header is not a Bearer token: %w",
			ErrInvalidToken,
		)
	}

	if authHeader[:schemaLen] != schema {
		return "", fmt.Errorf(
			"the value in the Authorization header is not a Bearer token: %w",
			ErrInvalidToken,
		)
	}

	return authHeader[schemaLen:], nil
}
