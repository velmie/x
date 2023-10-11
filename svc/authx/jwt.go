package authx

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

type Logger interface {
	Info(v ...any)
	Warning(v ...any)
	Error(v ...any)
	Debug(v ...any)
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
		return nil, fmt.Errorf("invalid options: %s", err)
	}
	validMethods := jwt.WithValidMethods(defaultJWTSigningMethods)
	if len(cfg.ValidSigningMethods) > 0 {
		validMethods = jwt.WithValidMethods(defaultJWTSigningMethods)
	}
	parser := jwt.NewParser(validMethods)
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
		if log != nil {
			retryClient.Logger = &loggerKVAdapter{log}
		}
		retryClient.RetryMax = opts.MaxRetries

		jwksOptions := &authentication.JWKSOptions{
			Client:              retryClient.StandardClient(),
			RequestOnUnknownKID: opts.RequestOnUnknownKID,
		}
		if log != nil {
			jwksOptions.WarnFunc = func(msg string) {
				log.Warning(fmt.Sprintf("JWKS: %s", msg))
			}
		}
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

func fallbackSource(a, b namedKeySource, log Logger) authentication.KeySource {
	return authentication.KeySourceFunc(func(ctx context.Context, kid string) (crypto.PublicKey, error) {
		key, err := a.source.FetchPublicKey(ctx, kid)
		if err == nil {
			return key, nil
		}
		if log != nil {
			log.Warning(
				fmt.Sprintf("failed to fetch key using the first key source '%s', fallback to the second key source '%s': %s",
					a.name,
					b.name,
					err,
				),
			)
		}
		return b.source.FetchPublicKey(ctx, kid)
	})
}

func jwksNonBlocking(endpoint string, options *authentication.JWKSOptions, ready chan<- struct{}) authentication.KeySource {
	var jwksSource *authentication.KeySourceJWKS
	go func() {
		jwksSource = authentication.NewKeySourceJWKS(endpoint, options)
		if ready != nil {
			ready <- struct{}{}
			close(ready)
		}
	}()

	return authentication.KeySourceFunc(func(ctx context.Context, kid string) (crypto.PublicKey, error) {
		if jwksSource == nil {
			return nil, fmt.Errorf("jwks source is not ready yet")
		}

		return jwksSource.FetchPublicKey(ctx, kid)
	})
}

var _ retryablehttp.LeveledLogger = (*loggerKVAdapter)(nil)

type loggerKVAdapter struct {
	log Logger
}

func (l loggerKVAdapter) Error(msg string, keysAndValues ...any) {
	l.log.Error(fmt.Sprintf("%s %s", msg, keysAndValuesToString(keysAndValues)))
}

func (l loggerKVAdapter) Info(msg string, keysAndValues ...any) {
	l.log.Info(fmt.Sprintf("%s %s", msg, keysAndValuesToString(keysAndValues)))
}

func (l loggerKVAdapter) Debug(msg string, keysAndValues ...any) {
	l.log.Debug(fmt.Sprintf("%s %s", msg, keysAndValuesToString(keysAndValues)))
}

func (l loggerKVAdapter) Warn(msg string, keysAndValues ...any) {
	l.log.Warning(fmt.Sprintf("%s %s", msg, keysAndValuesToString(keysAndValues)))
}

func keysAndValuesToString(keysAndValues []any) string {
	fields := make([]string, 0, (len(keysAndValues)+2)/2)
	for i := 0; i < len(keysAndValues); i += 2 {
		var key string
		if k, ok := keysAndValues[i].(string); ok {
			key = k
		} else {
			key = fmt.Sprintf("%v", keysAndValues[i])
		}
		fields = append(fields, fmt.Sprintf("%s = %v", key, keysAndValues[i+1]))
	}
	return strings.Join(fields, ", ")
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
