package requestauth

import (
	"context"
	"fmt"
	"net/http"
)

//go:generate go run go.uber.org/mock/mockgen@v0.2.0 -source auth.go -destination ./mock/auth.go

// Entity is used in order to provide authenticated entity attributes
type Entity map[string]any

// Method specifies authentication method
type Method interface {
	Authenticate(ctx context.Context, token string) (Entity, error)
}

// Injector returns the request copy with injected entity
type Injector interface {
	InjectAuth(entity Entity, w http.ResponseWriter, r *http.Request) (*http.Request, error)
}

// TokenExtractor retrieves string token from the given request
type TokenExtractor interface {
	Extract(r *http.Request) (token string, err error)
}

// NewPipeline creates authentication pipeline function
func NewPipeline(
	extractor TokenExtractor,
	method Method,
	injector Injector,
	assertions ...*Assertion,
) func(w http.ResponseWriter, r *http.Request) (*http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request) (*http.Request, error) {
		token, err := extractor.Extract(r)
		if err != nil {
			return r, fmt.Errorf("cannot extract token: %w", err)
		}

		ctx := r.Context()
		entity, err := method.Authenticate(ctx, token)
		if err != nil {
			return r, fmt.Errorf("cannot authenticate token: %w", err)
		}

		for _, a := range assertions {
			aErr, verified := a.assert(entity)
			if aErr != nil {
				return r, fmt.Errorf("failed to execute assertion: %w", err)
			}
			if !verified {
				return r, fmt.Errorf("%s: %w", a.Description, ErrVerification)
			}
		}

		authorizedRequest, err := injector.InjectAuth(entity, w, r)
		if err != nil {
			return r, fmt.Errorf("cannot authorize request: %w", err)
		}
		return authorizedRequest, nil
	}
}
