package connectx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/http/requestauth"
	"github.com/velmie/x/svc/http/server"
	"github.com/velmie/x/svc/identity"
)

const defaultAuthOperation = "authenticate_connect_request"

// AuthGate authenticates headers before Connect receives or decodes a message.
// It attaches the established principal using requestauth.WithPrincipal. An
// empty opts.Message selects a safe default; a nonempty message must be safe for
// every rejection. Rejected calls bypass Connect interceptors. Wrap the handler
// with HTTP recovery because Connect's interceptor recovery does not cover gates.
// The logger receives the original error and logs it as is.
func AuthGate(verifier requestauth.Authenticator, logger *slog.Logger, opts ErrorOptions) connect.RequestGateFunc {
	return func(ctx context.Context, _ connect.Spec, _ connect.Peer, header http.Header) (context.Context, error) {
		principal, err := requestauth.Authenticate(ctx, header, verifier)
		if err == nil {
			return requestauth.WithPrincipal(ctx, principal), nil
		}
		code := connect.CodeInternal
		message := "internal server error"
		reason := "internal_error"
		level := slog.LevelError
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			err = fmt.Errorf("authenticate request: %w: %w", ctx.Err(), err)
			code = connect.CodeCanceled
			message = "request canceled"
			reason = "request_canceled"
			level = slog.LevelInfo
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			err = fmt.Errorf("authenticate request: %w: %w", ctx.Err(), err)
			code = connect.CodeDeadlineExceeded
			message = "request deadline exceeded"
			reason = "deadline_exceeded"
			level = slog.LevelInfo
		case errors.Is(err, identity.ErrInvalidCredentials):
			code = connect.CodeUnauthenticated
			message = "Valid bearer credentials are required."
			reason = requestauth.ReasonInvalidCredentials
			level = slog.LevelInfo
			if errors.Is(err, requestauth.ErrMissingToken) {
				reason = requestauth.ReasonMissingCredentials
			} else if errors.Is(err, requestauth.ErrInvalidToken) {
				reason = requestauth.ReasonMalformedCredentials
			}
		case errors.Is(err, identity.ErrUnavailable):
			code = connect.CodeUnavailable
			message = "Authentication is temporarily unavailable."
			reason = requestauth.ReasonDependencyFailed
		}
		if opts.Message != "" {
			message = opts.Message
		}
		operation := server.Operation(ctx)
		if operation == "" {
			operation = defaultAuthOperation
		}
		logger.LogAttrs(
			ctx,
			level,
			"authentication rejected",
			slog.String("operation", operation),
			slog.String("component", "connectx"),
			slog.String("stage", "authenticate"),
			slog.String("reason", reason),
			slog.String("outcome", "rejected"),
			slog.Any("error", err),
		)
		return ctx, connect.NewError(
			code,
			&publicError{
				message: message,
				cause:   err,
			},
		)
	}
}
