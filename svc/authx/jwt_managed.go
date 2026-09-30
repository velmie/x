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
	// Log receives JWKS diagnostics and original causes when both source callbacks
	// are unset. The logger must redact sensitive values before output.
	Log Logger
}

// NewManagedJWTMethod loads the initial keys before returning a ready method.
// Canceling ctx stops managed refreshes and prevents subsequent key lookups.
// The context must remain live for the lifetime of the method.
func NewManagedJWTMethod(ctx context.Context, opts ManagedJWTMethodOptions) (*authentication.ViaJWT, error) {
	method, _, err := NewManagedJWTMethodWithShutdown(ctx, opts)
	return method, err
}

// NewManagedJWTMethodWithShutdown loads the initial keys and returns the method
// with a function that cancels its source and waits for owned work to finish.
// The context must remain live for the lifetime of the method.
// The shutdown function is non-nil once a source exists, even if startup fails.
// The caller should invoke it outside diagnostic callbacks, with a context that
// bounds the wait. It does not wait for application handlers or close the client.
func NewManagedJWTMethodWithShutdown(ctx context.Context, opts ManagedJWTMethodOptions) (*authentication.ViaJWT, func(context.Context) error, error) {
	if opts.Endpoint == "" {
		return nil, nil, fmt.Errorf("JWKS endpoint is required")
	}
	methods := opts.ValidSigningMethods
	if methods == nil {
		methods = defaultJWTSigningMethods
	}
	if len(methods) == 0 {
		return nil, nil, fmt.Errorf("ValidSigningMethods should not be empty")
	}
	sourceOptions := opts.JWKSOptions
	configureJWKSDiagnostics(&sourceOptions, opts.Log)
	source, err := authentication.NewManagedKeySourceJWKS(opts.Endpoint, &sourceOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("configure JWKS source: %w", err)
	}
	if err := source.Start(ctx); err != nil {
		source.Stop()
		return nil, source.Shutdown, fmt.Errorf("start JWKS source: %w", err)
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
	return authentication.NewViaJWT(authentication.NewJWTv5Parser(parser), keys), source.Shutdown, nil
}
