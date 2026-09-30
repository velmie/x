package authx

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/velmie/x/authentication"
)

// Method specifies authentication method
type Method interface {
	Authenticate(ctx context.Context, token string) (authentication.Entity, error)
}

const (
	JWTSigningMethodES256 JWTSigningMethod = "ES256"
	JWTSigningMethodES384 JWTSigningMethod = "ES384"
	JWTSigningMethodES512 JWTSigningMethod = "ES512"
	JWTSigningMethodPS256 JWTSigningMethod = "PS256"
	JWTSigningMethodPS384 JWTSigningMethod = "PS384"
	JWTSigningMethodPS512 JWTSigningMethod = "PS512"
	JWTSigningMethodRS256 JWTSigningMethod = "RS256"
	JWTSigningMethodRS384 JWTSigningMethod = "RS384"
	JWTSigningMethodRS512 JWTSigningMethod = "RS512"
)

var (
	defaultJWTSigningMethods = []JWTSigningMethod{
		JWTSigningMethodES256,
		JWTSigningMethodES384,
		JWTSigningMethodES512,
		JWTSigningMethodPS256,
		JWTSigningMethodPS384,
		JWTSigningMethodPS512,
		JWTSigningMethodRS256,
		JWTSigningMethodRS384,
		JWTSigningMethodRS512,
	}
	defaultJWTMethodOptions = JWTMethodOptions{
		ValidSigningMethods: defaultJWTSigningMethods,
		JWKSOptions: JWKSOptions{
			Enabled:                  false,
			Endpoint:                 nil,
			RequestRateLimit:         5,
			RequestRateLimitDuration: time.Minute,
			MaxRetries:               100,
			RequestOnUnknownKID:      true,
		},
		JWTPublicKey: nil,
	}
)

// Logger receives original errors in structured error and cause fields.
// Implementations must support concurrent calls and redact sensitive values
// in error chains before output, preserving useful causes and source fields.
type Logger interface {
	Info(msg string, v ...any)
	Warn(msg string, v ...any)
	Error(msg string, v ...any)
	Debug(msg string, v ...any)
}

type JWTSigningMethod = string

type JWKSOptions struct {
	// Indicates if JWKS is enabled or not.
	Enabled bool
	// The endpoint URL for the JWKS server to fetch public keys.
	Endpoint *url.URL
	// The maximum number of requests that can be made to the JWKS server in a specific duration.
	RequestRateLimit int
	// The time duration within which the rate limit applies.
	RequestRateLimitDuration time.Duration
	// The maximum number of retries for a failed request to the JWKS server.
	MaxRetries int
	// If true, a request to JWKS server will be made when a Key ID is not found in the local cache.
	RequestOnUnknownKID bool
	// Source ready
	SourceReady chan<- struct{}
}

type JWTMethodOptions struct {
	// List of JWT signing methods that are considered valid.
	ValidSigningMethods []JWTSigningMethod
	// Configuration options specific to JWKS.
	JWKSOptions JWKSOptions
	// The public key to be used if not using JWKS.
	JWTPublicKey crypto.PublicKey

	Log Logger
}

func NewJWTMethod(opts ...JWTMethodOption) (*authentication.ViaJWT, error) {
	cfg := defaultJWTMethodOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := validateJWTMethodOptions(&cfg); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
	parser := jwt.NewParser(jwt.WithValidMethods(cfg.ValidSigningMethods))
	viaJWT := authentication.NewViaJWT(
		authentication.NewJWTv5Parser(parser),
		keySource(&cfg, cfg.Log),
	)

	return viaJWT, nil
}

func keySource(cfg *JWTMethodOptions, log Logger) authentication.KeySource {
	var source authentication.KeySource
	if cfg.JWKSOptions.Enabled {
		opts := cfg.JWKSOptions
		retryClient := retryablehttp.NewClient()
		// The source reports bounded diagnostics. Retry logs include raw URLs.
		retryClient.Logger = nil
		retryClient.RetryMax = opts.MaxRetries

		jwksOptions := &authentication.JWKSOptions{
			Client:              retryClient.StandardClient(),
			RequestOnUnknownKID: opts.RequestOnUnknownKID,
		}
		configureJWKSDiagnostics(jwksOptions, log)
		jwksOptions.SetRefreshRateLimit(opts.RequestRateLimit, opts.RequestRateLimitDuration)

		source = jwksNonBlocking(opts.Endpoint.String(), jwksOptions, cfg.JWKSOptions.SourceReady)
	}

	if cfg.JWTPublicKey != nil {
		givenKeySource := authentication.KeySourceSingle{PublicKey: cfg.JWTPublicKey}
		if source != nil {
			source = fallbackSource(
				namedKeySource{name: "JWKS", source: source},
				namedKeySource{name: "given fixed Key", source: givenKeySource},
				log,
				false,
			)
		} else {
			source = givenKeySource
		}
	}

	return source
}

type namedKeySource struct {
	name   string
	source authentication.KeySource
}

type fallbackKeySource struct {
	primary, fallback    namedKeySource
	log                  Logger
	confirmedAbsenceOnly bool
}

func fallbackSource(a, b namedKeySource, log Logger, confirmedAbsenceOnly bool) authentication.KeySource {
	return fallbackKeySource{primary: a, fallback: b, log: log, confirmedAbsenceOnly: confirmedAbsenceOnly}
}

func (s fallbackKeySource) FetchPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	return s.fetch(ctx, kid, "")
}

func (s fallbackKeySource) FetchPublicKeyForAlgorithm(ctx context.Context, kid, algorithm string) (crypto.PublicKey, error) {
	return s.fetch(ctx, kid, algorithm)
}

func (s fallbackKeySource) fetch(ctx context.Context, kid, algorithm string) (crypto.PublicKey, error) {
	key, err := fetchKeyForAlgorithm(ctx, s.primary.source, kid, algorithm)
	if err == nil {
		return key, nil
	}
	// JWKS returns the sentinel itself for a confirmed absent key. A wrapped
	// ErrKeyNotFound can instead indicate a failed lookup, such as rate limiting.
	useFallback := !s.confirmedAbsenceOnly || err == authentication.ErrKeyNotFound
	if s.log != nil {
		message, outcome := "JWT key lookup failed; using fallback", "fallback"
		if !useFallback {
			message, outcome = "JWT key lookup failed; fallback rejected", "rejected"
		}
		fields := []any{
			"operation", "fetch_key", "stage", "primary_source",
			"reason", keySourceFailureReason(err), "outcome", outcome,
			"source", s.primary.name, "fallback_source", s.fallback.name,
		}
		s.log.Warn(message, append(fields, keySourceErrorFields(err)...)...)
	}
	if !useFallback {
		return nil, err
	}
	return fetchKeyForAlgorithm(ctx, s.fallback.source, kid, algorithm)
}

func fetchKeyForAlgorithm(ctx context.Context, source authentication.KeySource, kid, algorithm string) (crypto.PublicKey, error) {
	if algorithm != "" {
		if aware, ok := source.(interface {
			FetchPublicKeyForAlgorithm(context.Context, string, string) (crypto.PublicKey, error)
		}); ok {
			return aware.FetchPublicKeyForAlgorithm(ctx, kid, algorithm)
		}
	}
	return source.FetchPublicKey(ctx, kid)
}

func keySourceFailureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, authentication.ErrJWKSRateLimited):
		return "rate_limited"
	case errors.Is(err, authentication.ErrJWKSKeyRejected):
		return "key_rejected"
	case errors.Is(err, authentication.ErrKeySetExpired):
		return "key_set_expired"
	case errors.Is(err, authentication.ErrKeyNotFound):
		if err != authentication.ErrKeyNotFound {
			return "key_lookup_failed"
		}
		return "key_not_found"
	default:
		return "key_source_failed"
	}
}

func jwksNonBlocking(endpoint string, options *authentication.JWKSOptions, ready chan<- struct{}) authentication.KeySource {
	var jwksSource atomic.Pointer[authentication.KeySourceJWKS]
	go func() {
		jwksSource.Store(authentication.NewKeySourceJWKS(endpoint, options))
		if ready != nil {
			ready <- struct{}{}
			close(ready)
		}
	}()

	return authentication.KeySourceFunc(func(ctx context.Context, kid string) (crypto.PublicKey, error) {
		source := jwksSource.Load()
		if source == nil {
			return nil, fmt.Errorf("jwks source is not ready yet")
		}

		return source.FetchPublicKey(ctx, kid)
	})
}

func validateJWKSOptions(opts *JWKSOptions) error {
	var errs []string

	if opts.Enabled {
		if opts.Endpoint == nil || opts.Endpoint.String() == "" {
			errs = append(errs, "if JWKS is enabled, Endpoint should not be empty")
		}
		if opts.RequestRateLimit < 0 {
			errs = append(errs, "RequestRateLimit should be greater then or equal to 0")
		}
		if opts.RequestRateLimitDuration < 0 {
			errs = append(errs, "RequestRateLimitDuration should be greater then or equal to 0")
		}
		if opts.MaxRetries <= 0 {
			errs = append(errs, "MaxRetries should be greater than 0")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

func validateJWTMethodOptions(opts *JWTMethodOptions) error {
	var errs []string

	if len(opts.ValidSigningMethods) == 0 {
		errs = append(errs, "ValidSigningMethods should not be empty")
	}
	if !opts.JWKSOptions.Enabled && opts.JWTPublicKey == nil {
		errs = append(errs, "if JWKS is disabled, JWTPublicKey should not be nil")
	}

	jwksErr := validateJWKSOptions(&opts.JWKSOptions)
	if jwksErr != nil {
		errs = append(errs, jwksErr.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

func configureJWKSDiagnostics(options *authentication.JWKSOptions, log Logger) {
	if options.DiagnosticFunc != nil || options.WarnFunc != nil || log == nil {
		return
	}
	options.DiagnosticFunc = func(event *authentication.JWKSError) {
		fields := []any{
			"operation", event.Operation(), "stage", event.Stage(),
			"reason", event.Reason(), "outcome", event.Outcome(),
			"status", event.HTTPStatus(), "source", "JWKS",
		}
		log.Warn("JWKS refresh", append(fields, keySourceErrorFields(event)...)...)
	}
}

func keySourceErrorFields(err error) []any {
	fields := []any{"error", err}
	var diagnostic *authentication.JWKSError
	if errors.As(err, &diagnostic) {
		if diagnostic.Field() != "" {
			fields = append(fields, "field", diagnostic.Field())
		}
		if cause := diagnostic.Unwrap(); cause != nil {
			fields = append(fields, "cause", cause)
		}
	}
	return fields
}
