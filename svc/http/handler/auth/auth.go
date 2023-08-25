package auth

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

type SuccessHandler interface {
	HandleSuccess(entity Entity, w http.ResponseWriter, r *http.Request)
}

type TokenExtractor interface {
	Extract(r *http.Request) (token string, err error)
}

type ErrorHandler interface {
	HandleError(err error, w http.ResponseWriter, r *http.Request)
}

func Handler(
	extractor TokenExtractor,
	method Method,
	successHandler SuccessHandler,
	errHandler ErrorHandler,
	assertions ...*Assertion,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := extractor.Extract(r)
		if err != nil {
			errHandler.HandleError(err, w, r)
			return
		}

		ctx := r.Context()
		entity, err := method.Authenticate(ctx, token)
		if err != nil {
			errHandler.HandleError(err, w, r)
			return
		}

		for _, a := range assertions {
			aErr, verified := a.assert(entity)
			if aErr != nil {
				errHandler.HandleError(aErr, w, r)
				return
			}
			if !verified {
				errHandler.HandleError(fmt.Errorf("%s: %w", a.Description, ErrVerification), w, r)
				return
			}
		}

		successHandler.HandleSuccess(entity, w, r)
	}
}
