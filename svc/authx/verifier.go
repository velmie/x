// Package authx integrates authentication mechanisms with service identity.
// Verifier applies an explicit JWT trust policy to a borrowed key source.
// Construction starts no workers and does not own source shutdown.
package authx

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/identity"
)

const (
	// DefaultMaxTokenBytes bounds the encoded JWT accepted by the verifier.
	DefaultMaxTokenBytes = 8192
	// DefaultMaxKeyIDBytes bounds the token's key identifier.
	DefaultMaxKeyIDBytes = 128
)

// TrustPolicy identifies one trust domain sharing a key source. Use separate
// verifiers for issuers whose keys must not authenticate each other's tokens.
type TrustPolicy struct {
	Algorithms []string
	Issuers    []string
	Audiences  []string
	Leeway     time.Duration
}

// Validate checks the policy without opening resources. Algorithms must be
// supported asymmetric JWT methods. Issuers and audiences must be nonblank.
func (p TrustPolicy) Validate() error {
	if len(p.Algorithms) == 0 {
		return errors.New("Algorithms: expected at least one asymmetric JWT algorithm")
	}
	for i, algorithm := range p.Algorithms {
		switch algorithm {
		case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA":
		default:
			return fmt.Errorf("Algorithms[%d]: expected a supported asymmetric JWT algorithm", i)
		}
	}
	if len(p.Issuers) == 0 {
		return errors.New("Issuers: expected at least one issuer")
	}
	for i, issuer := range p.Issuers {
		if !identity.ValidIssuer(issuer) {
			return fmt.Errorf("Issuers[%d]: expected nonblank UTF-8 of at most %d bytes", i, identity.MaxIssuerBytes)
		}
	}
	if len(p.Audiences) == 0 {
		return errors.New("Audiences: expected at least one audience")
	}
	for i, audience := range p.Audiences {
		if !utf8.ValidString(audience) || strings.TrimSpace(audience) == "" {
			return fmt.Errorf("Audiences[%d]: expected nonblank UTF-8", i)
		}
	}
	if p.Leeway < 0 {
		return errors.New("Leeway: expected a nonnegative duration")
	}
	return nil
}

// ClaimLimits can tighten the organization limits; zero fields use defaults.
// Negative values and values above the documented defaults are invalid.
type ClaimLimits struct {
	MaxTokenBytes      int
	MaxKeyIDBytes      int
	MaxIssuerBytes     int
	MaxSubjectBytes    int
	MaxPermissions     int
	MaxPermissionBytes int
}

// Verifier authenticates JWTs into the shared service principal. It borrows the
// key source, is safe for concurrent use when the source is, and owns no workers.
type Verifier struct {
	keys    authentication.KeySource
	parser  *jwt.Parser
	issuers []string
	limits  ClaimLimits
}

// NewVerifier validates and snapshots policy and limits, borrowing keys. All
// configured issuers share that key source. It performs no I/O.
func NewVerifier(keys authentication.KeySource, policy TrustPolicy, limits ClaimLimits) (*Verifier, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validate trust policy: %w", err)
	}
	limits, err := effectiveClaimLimits(limits)
	if err != nil {
		return nil, fmt.Errorf("validate claim limits: %w", err)
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods(slices.Clone(policy.Algorithms)),
		jwt.WithAudience(slices.Clone(policy.Audiences)...),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(policy.Leeway),
	)
	return &Verifier{
		keys:    keys,
		parser:  parser,
		issuers: slices.Clone(policy.Issuers),
		limits:  limits,
	}, nil
}

// Authenticate verifies token and returns its principal or a classified failure.
// Credential failures match identity.ErrInvalidCredentials. Key-source failures
// match identity.ErrUnavailable, preserving their causes for errors.Is/As. The
// caller's logger receives the original error and logs it as is.
func (v *Verifier) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	if err := ctx.Err(); err != nil {
		return identity.Principal{}, err
	}
	if token == "" || len(token) > v.limits.MaxTokenBytes {
		return identity.Principal{}, invalidCredentials("token: expected 1 to %d bytes", v.limits.MaxTokenBytes)
	}
	claims := jwt.MapClaims{}
	_, err := v.parser.ParseWithClaims(
		token,
		claims,
		func(parsed *jwt.Token) (any, error) {
			kid, ok := parsed.Header["kid"].(string)
			if !ok || kid == "" || len(kid) > v.limits.MaxKeyIDBytes {
				return nil, invalidCredentials("header.kid: expected a nonempty string of at most %d bytes", v.limits.MaxKeyIDBytes)
			}
			key, err := fetchKeyForAlgorithm(ctx, v.keys, kid, parsed.Method.Alg())
			if err == nil {
				return key, nil
			}
			if errors.Is(err, authentication.ErrKeyNotFound) || errors.Is(err, authentication.ErrKeyRejected) {
				return nil, fmt.Errorf("%w: verify JWT key: %w", identity.ErrInvalidCredentials, err)
			}
			return nil, fmt.Errorf("%w: fetch JWT verification key: %w", identity.ErrUnavailable, err)
		},
	)
	if err != nil {
		if ctx.Err() != nil {
			return identity.Principal{}, fmt.Errorf("verify JWT: %w: %w", ctx.Err(), err)
		}
		if errors.Is(err, identity.ErrUnavailable) {
			return identity.Principal{}, fmt.Errorf("verify JWT: %w", err)
		}
		return identity.Principal{}, fmt.Errorf("%w: verify JWT: %w", identity.ErrInvalidCredentials, err)
	}
	// encoding/json replaces invalid UTF-8. Check the signed payload before
	// accepting any decoded identity text.
	segments := strings.SplitN(token, ".", 3)
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return identity.Principal{}, fmt.Errorf("%w: decode JWT claims: %w", identity.ErrInvalidCredentials, err)
	}
	if !utf8.Valid(payload) {
		return identity.Principal{}, invalidCredentials("claims: expected valid UTF-8")
	}
	issuer, ok := claims["iss"].(string)
	if !ok || !identity.ValidIssuer(issuer) || len(issuer) > v.limits.MaxIssuerBytes {
		return identity.Principal{}, invalidCredentials("iss: expected nonblank UTF-8 of at most %d bytes", v.limits.MaxIssuerBytes)
	}
	if !slices.Contains(v.issuers, issuer) {
		return identity.Principal{}, invalidCredentials("iss: expected an issuer in the trust policy")
	}
	subject, ok := claims["sub"].(string)
	if !ok || !identity.ValidSubject(subject) || len(subject) > v.limits.MaxSubjectBytes {
		return identity.Principal{}, invalidCredentials("sub: expected nonblank UTF-8 of at most %d bytes", v.limits.MaxSubjectBytes)
	}
	principal := identity.Principal{
		Issuer:  issuer,
		Subject: subject,
	}
	if raw, present := claims["permissions"]; present {
		principal.Permissions, err = v.permissions(raw)
		if err != nil {
			return identity.Principal{}, err
		}
	}
	return principal, nil
}

func (v *Verifier) permissions(raw any) ([]string, error) {
	values, ok := raw.([]any)
	if !ok || len(values) > v.limits.MaxPermissions {
		return nil, invalidCredentials("permissions: expected an array of at most %d strings", v.limits.MaxPermissions)
	}
	permissions := make([]string, 0, len(values))
	for i, raw := range values {
		value, ok := raw.(string)
		if !ok || value == "" || len(value) > v.limits.MaxPermissionBytes || strings.TrimSpace(value) != value {
			return nil, invalidCredentials(
				"permissions[%d]: expected an unpadded string of 1 to %d bytes",
				i,
				v.limits.MaxPermissionBytes,
			)
		}
		permissions = append(permissions, value)
	}
	return permissions, nil
}

func effectiveClaimLimits(limits ClaimLimits) (ClaimLimits, error) {
	fields := []struct {
		name    string
		value   *int
		maximum int
	}{
		{
			name:    "MaxTokenBytes",
			value:   &limits.MaxTokenBytes,
			maximum: DefaultMaxTokenBytes,
		},
		{
			name:    "MaxKeyIDBytes",
			value:   &limits.MaxKeyIDBytes,
			maximum: DefaultMaxKeyIDBytes,
		},
		{
			name:    "MaxIssuerBytes",
			value:   &limits.MaxIssuerBytes,
			maximum: identity.MaxIssuerBytes,
		},
		{
			name:    "MaxSubjectBytes",
			value:   &limits.MaxSubjectBytes,
			maximum: identity.MaxSubjectBytes,
		},
		{
			name:    "MaxPermissions",
			value:   &limits.MaxPermissions,
			maximum: identity.MaxPermissions,
		},
		{
			name:    "MaxPermissionBytes",
			value:   &limits.MaxPermissionBytes,
			maximum: identity.MaxPermissionBytes,
		},
	}
	for _, field := range fields {
		if *field.value < 0 || *field.value > field.maximum {
			return ClaimLimits{}, fmt.Errorf("%s: expected zero or a value from 1 to %d", field.name, field.maximum)
		}
		if *field.value == 0 {
			*field.value = field.maximum
		}
	}
	return limits, nil
}

func invalidCredentials(format string, args ...any) error {
	return fmt.Errorf("%w: %s", identity.ErrInvalidCredentials, fmt.Sprintf(format, args...))
}
