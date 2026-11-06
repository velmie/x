// Package requestauth authenticates HTTP credentials and carries verified
// identity across the request boundary. Application authorization remains
// with the called operation.
package requestauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/velmie/x/svc/http/response"
	"github.com/velmie/x/svc/http/server"
	"github.com/velmie/x/svc/identity"
)

const (
	// MaxAuthorizationBytes bounds one Authorization field value.
	MaxAuthorizationBytes = 8200
	// MaxBearerTokenBytes bounds an extracted bearer token.
	MaxBearerTokenBytes = 8192

	// ReasonMissingCredentials identifies an absent Authorization header.
	ReasonMissingCredentials = "missing_credentials"
	// ReasonMalformedCredentials identifies invalid credential framing.
	ReasonMalformedCredentials = "malformed_credentials"
	// ReasonInvalidCredentials identifies failed credential verification.
	ReasonInvalidCredentials = "invalid_credentials"
	// ReasonDependencyFailed identifies unavailable authentication dependencies.
	ReasonDependencyFailed = "auth_dependency_failed"

	statusRequestCanceled = 499
	defaultOperation      = "authenticate_http_request"
)

// Authenticator establishes identity from a credential. Implementations classify
// expected rejection with identity.ErrInvalidCredentials and dependency failure
// with identity.ErrUnavailable. The logger receives the original error and logs it as is.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (identity.Principal, error)
}

// StrictBearerTokenExtractor adapts BearerToken to the existing pipeline API.
type StrictBearerTokenExtractor struct{}

// Extract extracts a strict bearer credential from r.
func (StrictBearerTokenExtractor) Extract(r *http.Request) (string, error) {
	return BearerToken(r.Header)
}

// BearerToken extracts exactly one bounded bearer credential. The scheme is
// case-insensitive and the value must contain exactly two whitespace-separated
// fields. Missing and malformed framing match ErrMissingToken and ErrInvalidToken.
func BearerToken(header http.Header) (string, error) {
	var value string
	count := 0
	for name, values := range header {
		if !strings.EqualFold(name, "Authorization") {
			continue
		}
		for _, candidate := range values {
			count++
			if count > 1 {
				return "", fmt.Errorf("authorization: expected exactly one field: %w", ErrInvalidToken)
			}
			value = candidate
		}
	}
	if count == 0 {
		return "", fmt.Errorf("authorization: expected a bearer credential: %w", ErrMissingToken)
	}
	if len(value) > MaxAuthorizationBytes {
		return "", fmt.Errorf("authorization: expected at most %d bytes: %w", MaxAuthorizationBytes, ErrInvalidToken)
	}
	fields := strings.Fields(value)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", fmt.Errorf("authorization: expected one bearer credential: %w", ErrInvalidToken)
	}
	if len(fields[1]) > MaxBearerTokenBytes || strings.Contains(fields[1], ",") {
		return "", fmt.Errorf("authorization: expected one bearer token of at most %d bytes: %w", MaxBearerTokenBytes, ErrInvalidToken)
	}
	return fields[1], nil
}

// Authenticate extracts and verifies credentials without logging or responding.
// Extraction errors also match identity.ErrInvalidCredentials. It returns a zero
// principal on every failure and preserves authenticator causes for errors.Is/As.
func Authenticate(ctx context.Context, header http.Header, verifier Authenticator) (identity.Principal, error) {
	token, err := BearerToken(header)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("%w: %w", identity.ErrInvalidCredentials, err)
	}
	principal, err := verifier.Authenticate(ctx, token)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("authenticate bearer credential: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return identity.Principal{}, fmt.Errorf("authenticate bearer credential: %w", err)
	}
	return principal, nil
}

// Require authenticates before invoking next and writes and logs rejections.
// It does not read the request body. Invalid credentials receive a Bearer
// challenge; dependency and unexpected failures do not. The logger receives the
// original error and logs it as is.
func Require(verifier Authenticator, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			principal, err := Authenticate(r.Context(), r.Header, verifier)
			if err == nil {
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
				return
			}
			reject(w, r, logger, err)
		},
	)
}

// WithPrincipal attaches an identity established by a trusted boundary.
func WithPrincipal(ctx context.Context, principal identity.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// Principal returns the established identity and panics if the gate was omitted.
func Principal(ctx context.Context) identity.Principal {
	return ctx.Value(principalKey{}).(identity.Principal)
}

// PrincipalHandler passes the established principal explicitly to an operation.
func PrincipalHandler(next func(w http.ResponseWriter, r *http.Request, principal identity.Principal)) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			next(w, r, Principal(r.Context()))
		},
	)
}

func reject(w http.ResponseWriter, r *http.Request, logger *slog.Logger, cause error) {
	w.Header().Del("WWW-Authenticate")
	status := http.StatusInternalServerError
	code := response.ErrCodeInternalServerError
	title := "internal server error"
	reason := "internal_error"
	level := slog.LevelError
	switch {
	case errors.Is(r.Context().Err(), context.Canceled):
		cause = fmt.Errorf("authenticate request: %w: %w", r.Context().Err(), cause)
		status = statusRequestCanceled
		code = response.ErrCodeRequestCanceled
		title = "request canceled"
		reason = "request_canceled"
		level = slog.LevelInfo
	case errors.Is(r.Context().Err(), context.DeadlineExceeded):
		cause = fmt.Errorf("authenticate request: %w: %w", r.Context().Err(), cause)
		status = http.StatusGatewayTimeout
		code = response.ErrCodeDeadlineExceeded
		title = "request deadline exceeded"
		reason = "deadline_exceeded"
		level = slog.LevelInfo
	case errors.Is(cause, identity.ErrInvalidCredentials):
		status = http.StatusUnauthorized
		code = response.ErrCodeUnauthorized
		title = "Valid bearer credentials are required."
		reason = ReasonInvalidCredentials
		level = slog.LevelInfo
		w.Header().Set("WWW-Authenticate", "Bearer")
		if errors.Is(cause, ErrMissingToken) {
			reason = ReasonMissingCredentials
		} else if errors.Is(cause, ErrInvalidToken) {
			reason = ReasonMalformedCredentials
		}
	case errors.Is(cause, identity.ErrUnavailable):
		status = http.StatusServiceUnavailable
		code = response.ErrCodeAuthUnavailable
		title = "Authentication is temporarily unavailable."
		reason = ReasonDependencyFailed
	}
	operation := server.Operation(r.Context())
	if operation == "" {
		operation = defaultOperation
	}
	attributes := []slog.Attr{
		slog.String("operation", operation),
		slog.String("component", "requestauth"),
		slog.String("stage", "authenticate"),
		slog.String("reason", reason),
		slog.String("outcome", "rejected"),
		slog.Any("error", cause),
	}
	body := response.Error(
		&response.HTTPError{
			Code:   code,
			Title:  title,
			Target: response.TargetCommon,
		},
	)
	writeErr := response.WriteErrors(w, r, status, body)
	if writeErr != nil {
		attributes = append(attributes, slog.Any("response_error", writeErr))
	}
	logger.LogAttrs(
		r.Context(),
		level,
		"authentication rejected",
		attributes...,
	)
}

type principalKey struct{}
