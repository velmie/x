package connectx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/connectx"
	"github.com/velmie/x/svc/http/requestauth"
	"github.com/velmie/x/svc/http/server"
	"github.com/velmie/x/svc/identity"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestAuthGateClassifiesFailuresAndPreservesCause(t *testing.T) {
	dependency := &gateDependencyError{}
	unexpected := errors.New("unexpected verifier failure")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		header http.Header
		cause  error
		code   connect.Code
		reason string
		level  string
	}{
		{
			name:   "missing",
			code:   connect.CodeUnauthenticated,
			reason: requestauth.ReasonMissingCredentials,
			level:  "INFO",
		},
		{
			name:   "duplicate",
			header: http.Header{"Authorization": {"Bearer one", "Bearer two"}},
			code:   connect.CodeUnauthenticated,
			reason: requestauth.ReasonMalformedCredentials,
			level:  "INFO",
		},
		{
			name:   "invalid",
			header: http.Header{"Authorization": {"Bearer credential-canary"}},
			cause:  fmt.Errorf("%w: sub: expected nonblank", identity.ErrInvalidCredentials),
			code:   connect.CodeUnauthenticated,
			reason: requestauth.ReasonInvalidCredentials,
			level:  "INFO",
		},
		{
			name:   "dependency timeout while request is live",
			header: http.Header{"Authorization": {"Bearer credential-canary"}},
			cause:  fmt.Errorf("%w: %w: %w", identity.ErrUnavailable, context.DeadlineExceeded, dependency),
			code:   connect.CodeUnavailable,
			reason: requestauth.ReasonDependencyFailed,
			level:  "ERROR",
		},
		{
			name:   "unclassified",
			header: http.Header{"Authorization": {"Bearer credential-canary"}},
			cause:  unexpected,
			code:   connect.CodeInternal,
			reason: "internal_error",
			level:  "ERROR",
		},
		{
			name:   "request canceled",
			ctx:    canceled,
			header: http.Header{"Authorization": {"Bearer credential-canary"}},
			cause:  fmt.Errorf("%w: %w", identity.ErrUnavailable, dependency),
			code:   connect.CodeCanceled,
			reason: "request_canceled",
			level:  "INFO",
		},
		{
			name:   "request deadline",
			ctx:    deadline,
			header: http.Header{"Authorization": {"Bearer credential-canary"}},
			cause:  context.DeadlineExceeded,
			code:   connect.CodeDeadlineExceeded,
			reason: "deadline_exceeded",
			level:  "INFO",
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				var output bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&output, nil))
				verifier := gateAuthenticator(
					func(context.Context, string) (identity.Principal, error) {
						return identity.Principal{}, tc.cause
					},
				)
				gate := connectx.AuthGate(
					verifier,
					logger,
					connectx.ErrorOptions{
						Message: "authentication rejected",
					},
				)
				ctx := tc.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				ctx = server.WithOperation(ctx, "books.create")
				_, err := gate(ctx, connect.Spec{}, connect.Peer{}, tc.header)
				var rpc *connect.Error
				if !errors.As(err, &rpc) || rpc.Code() != tc.code || rpc.Message() != "authentication rejected" {
					t.Fatalf("code/message: %v", err)
				}
				if tc.cause != nil && !errors.Is(err, tc.cause) {
					t.Fatalf("cause lost: %v", err)
				}
				if errors.Is(tc.cause, dependency) {
					var got *gateDependencyError
					if !errors.As(err, &got) || got != dependency {
						t.Fatalf("dependency type lost: %v", err)
					}
				}
				for _, expected := range []string{tc.level, tc.reason, "books.create", "authenticate", "rejected"} {
					if !strings.Contains(output.String(), expected) {
						t.Fatalf("missing %q in log %s", expected, output.String())
					}
				}
				if strings.Contains(output.String(), "credential-canary") || strings.Contains(rpc.Message(), "credential-canary") {
					t.Fatalf("credential exposed: %s / %s", output.String(), rpc.Message())
				}
			},
		)
	}
}

func TestAuthGateDefaultPublicMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		code    connect.Code
		message string
	}{
		{
			name:    "invalid",
			cause:   identity.ErrInvalidCredentials,
			code:    connect.CodeUnauthenticated,
			message: "Valid bearer credentials are required.",
		},
		{
			name:    "unavailable",
			cause:   identity.ErrUnavailable,
			code:    connect.CodeUnavailable,
			message: "Authentication is temporarily unavailable.",
		},
		{
			name:    "internal",
			cause:   errors.New("private cause"),
			code:    connect.CodeInternal,
			message: "internal server error",
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				verifier := gateAuthenticator(
					func(context.Context, string) (identity.Principal, error) {
						return identity.Principal{}, tc.cause
					},
				)
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				gate := connectx.AuthGate(verifier, logger, connectx.ErrorOptions{})
				_, err := gate(context.Background(), connect.Spec{}, connect.Peer{}, http.Header{"Authorization": {"Bearer token"}})
				var rpc *connect.Error
				if !errors.As(err, &rpc) || rpc.Code() != tc.code || rpc.Message() != tc.message {
					t.Fatalf("unexpected public failure: %v", err)
				}
			},
		)
	}
}

func TestAuthGateDoesNotReadRejectedMessage(t *testing.T) {
	var calls atomic.Int32
	verifier := gateAuthenticator(
		func(context.Context, string) (identity.Principal, error) {
			calls.Add(1)
			return identity.Principal{}, identity.ErrInvalidCredentials
		},
	)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := connect.NewUnaryHandler(
		"/books.v1.Books/Create",
		func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			t.Fatal("handler ran after rejected credentials")
			return nil, nil
		},
		connect.WithRequestGate(connectx.AuthGate(verifier, logger, connectx.ErrorOptions{})),
	)
	request := httptest.NewRequest(http.MethodPost, "/books.v1.Books/Create", unreadableBody{})
	request.Header.Set("Content-Type", "application/proto")
	request.Header.Set("Authorization", "Bearer token")
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != http.StatusUnauthorized || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d", writer.Code, calls.Load())
	}
}

func TestAuthGateConnectGRPCAndGRPCWeb(t *testing.T) {
	const procedure = "/books.v1.Books/Create"
	var intercepts atomic.Int32
	var handled atomic.Int32
	verifier := gateAuthenticator(
		func(_ context.Context, token string) (identity.Principal, error) {
			principal := identity.Principal{
				Issuer:  "issuer",
				Subject: "subject",
			}
			if token == "writer" {
				principal.Permissions = []string{"books:write"}
			}
			return principal, nil
		},
	)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	interceptor := connect.UnaryInterceptorFunc(
		func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				intercepts.Add(1)
				return next(ctx, req)
			}
		},
	)
	handler := connect.NewUnaryHandler(
		procedure,
		func(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			handled.Add(1)
			principal := requestauth.Principal(ctx)
			if principal.Subject != "subject" || !principal.Has("books:write") {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("permission denied"))
			}
			return connect.NewResponse(&emptypb.Empty{}), nil
		},
		connect.WithRequestGate(connectx.AuthGate(verifier, logger, connectx.ErrorOptions{})),
		connect.WithInterceptors(interceptor),
	)
	testServer := httptest.NewUnstartedServer(handler)
	testServer.EnableHTTP2 = true
	testServer.StartTLS()
	defer testServer.Close()
	for _, tc := range []struct {
		name string
		opts []connect.ClientOption
	}{
		{
			name: "connect",
		},
		{
			name: "grpc",
			opts: []connect.ClientOption{connect.WithGRPC()},
		},
		{
			name: "grpc-web",
			opts: []connect.ClientOption{connect.WithGRPCWeb()},
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				client := connect.NewClient[emptypb.Empty, emptypb.Empty](testServer.Client(), testServer.URL+procedure, tc.opts...)
				beforeInterceptor := intercepts.Load()
				beforeHandler := handled.Load()
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
				if connect.CodeOf(err) != connect.CodeUnauthenticated || intercepts.Load() != beforeInterceptor || handled.Load() != beforeHandler {
					t.Fatalf("rejection did not bypass interceptor/handler: %v", err)
				}
				reader := connect.NewRequest(&emptypb.Empty{})
				reader.Header().Set("Authorization", "Bearer reader")
				_, err = client.CallUnary(context.Background(), reader)
				if connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("application authorization was bypassed: %v", err)
				}
				writer := connect.NewRequest(&emptypb.Empty{})
				writer.Header().Set("Authorization", "Bearer writer")
				if _, err := client.CallUnary(context.Background(), writer); err != nil {
					t.Fatalf("authorized call failed: %v", err)
				}
				if intercepts.Load() != beforeInterceptor+2 || handled.Load() != beforeHandler+2 {
					t.Fatal("accepted calls did not reach interceptor and handler")
				}
			},
		)
	}
}

type gateAuthenticator func(context.Context, string) (identity.Principal, error)

func (f gateAuthenticator) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	return f(ctx, token)
}

type unreadableBody struct{}

func (unreadableBody) Read([]byte) (int, error) { panic("Connect decoded a rejected message") }

type gateDependencyError struct{}

func (*gateDependencyError) Error() string { return "key provider timeout" }
