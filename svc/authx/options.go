package authx

import (
	"crypto"
	"net/url"
	"time"
)

// JWTMethodOption is a function type used to modify the properties of JWTMethodOptions.
type JWTMethodOption func(opts *JWTMethodOptions)

// WithJWKSSource sets the JWKS endpoint URL and enables JWKS.
// If this option is used, JWKS will be enabled, and JWT tokens will be validated using
// public keys fetched from the given JWKS endpoint.
func WithJWKSSource(endpoint *url.URL) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.Enabled = true
		opts.JWKSOptions.Endpoint = endpoint
	}
}

// WithJWTSigningMethods sets the JWT signing methods that are considered valid.
// This option allows customization of the accepted JWT signing algorithms.
func WithJWTSigningMethods(methods []JWTSigningMethod) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.ValidSigningMethods = methods
	}
}

// WithJWKSRequestRateLimit sets the maximum number of requests that can be made
// to the JWKS server within a specific duration.
func WithJWKSRequestRateLimit(rateLimit int) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.RequestRateLimit = rateLimit
	}
}

// WithJWKSSourceReadySignal sets chanel which is closed once the source is ready (keys initialized)
func WithJWKSSourceReadySignal(c chan<- struct{}) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.SourceReady = c
	}
}

// WithJWKSDisabledRateLimit disables JWKS request rate limiting
func WithJWKSDisabledRateLimit(rateLimit int) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.RequestRateLimit = 0
		opts.JWKSOptions.RequestRateLimitDuration = 0
	}
}

// WithJWKSRequestRateLimitDuration sets the time duration within which the JWKS request rate limit applies.
func WithJWKSRequestRateLimitDuration(duration time.Duration) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.RequestRateLimitDuration = duration
	}
}

// WithJWKSMaxRetries sets the maximum number of retries for a failed request to the JWKS server.
func WithJWKSMaxRetries(maxRetries int) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.MaxRetries = maxRetries
	}
}

// WithJWKSRequestOnUnknownKID sets whether a request to JWKS server will be made when
// a Key ID is not found in the local cache.
func WithJWKSRequestOnUnknownKID(requestOnUnknown bool) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWKSOptions.RequestOnUnknownKID = requestOnUnknown
	}
}

// WithJWTPublicKey sets the public key to be used if not using JWKS.
// Use this option when validating JWT tokens using a specific public key instead of fetching from JWKS.
func WithJWTPublicKey(publicKey crypto.PublicKey) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.JWTPublicKey = publicKey
	}
}

// WithLogger sets the logger for JWT methods.
// This option allows you to pass a custom logger for JWT processing tasks.
func WithLogger(log Logger) JWTMethodOption {
	return func(opts *JWTMethodOptions) {
		opts.Log = log
	}
}
