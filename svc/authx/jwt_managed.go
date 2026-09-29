package authx

import (
	"context"
	"crypto"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
)

// ManagedJWTMethodOptions configures JWT authentication with an owned JWKS lifetime.
// Applications supply transport and refresh policy directly through JWKSOptions.
type ManagedJWTMethodOptions struct {
	Endpoint    string
	JWKSOptions authentication.JWKSOptions
	// Nil uses the default algorithms. An explicitly empty list is invalid.
	ValidSigningMethods []JWTSigningMethod
	// FallbackPublicKey is used only for a confirmed absent key in a fresh snapshot.
	FallbackPublicKey crypto.PublicKey
	// Log receives bounded JWKS diagnostics when JWKSOptions.WarnFunc is unset.
	Log Logger
}

// NewManagedJWTMethod loads the initial keys before returning a ready method.
// Canceling ctx stops managed refreshes and prevents subsequent key lookups.
// The context must remain live for the lifetime of the method.
func NewManagedJWTMethod(ctx context.Context, opts ManagedJWTMethodOptions) (*authentication.ViaJWT, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("JWKS endpoint is required")
	}
	methods := opts.ValidSigningMethods
	if methods == nil {
		methods = defaultJWTSigningMethods
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("ValidSigningMethods should not be empty")
	}
	sourceOptions := opts.JWKSOptions
	if sourceOptions.WarnFunc == nil && opts.Log != nil {
		sourceOptions.WarnFunc = func(message string) {
			opts.Log.Warn("JWKS refresh", "operation", "refresh_keys", "diagnostic", message)
		}
	}
	source, err := authentication.NewManagedKeySourceJWKS(opts.Endpoint, &sourceOptions)
	if err != nil {
		return nil, fmt.Errorf("configure JWKS source: %w", err)
	}
	if err := source.Start(ctx); err != nil {
		source.Stop()
		return nil, fmt.Errorf("start JWKS source: %w", err)
	}
	parser := jwt.NewParser(jwt.WithValidMethods(append([]string(nil), methods...)))
	var keys authentication.KeySource = source
	if opts.FallbackPublicKey != nil {
		keys = fallbackSource(
			namedKeySource{name: "JWKS", source: source},
			namedKeySource{name: "given fixed Key", source: authentication.KeySourceSingle{PublicKey: opts.FallbackPublicKey}},
			opts.Log,
			true,
		)
	}
	return authentication.NewViaJWT(authentication.NewJWTv5Parser(parser), keys), nil
}
