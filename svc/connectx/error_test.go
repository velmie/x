package connectx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/connectx"
	"google.golang.org/protobuf/types/known/emptypb"
)

type domainError struct{}

func (*domainError) Error() string { return "CANARY_CAUSE" }

func TestCode(t *testing.T) {
	tests := []struct {
		category apperror.Category
		code     connect.Code
		message  string
	}{
		{
			category: apperror.Internal,
			code:     connect.CodeInternal,
			message:  "internal server error",
		},
		{
			category: apperror.Invalid,
			code:     connect.CodeInvalidArgument,
			message:  "invalid",
		},
		{
			category: apperror.Forbidden,
			code:     connect.CodePermissionDenied,
			message:  "forbidden",
		},
		{
			category: apperror.NotFound,
			code:     connect.CodeNotFound,
			message:  "not found",
		},
		{
			category: apperror.Conflict,
			code:     connect.CodeAborted,
			message:  "conflict",
		},
		{
			category: apperror.Precondition,
			code:     connect.CodeFailedPrecondition,
			message:  "precondition",
		},
		{
			category: apperror.Unavailable,
			code:     connect.CodeUnavailable,
			message:  "unavailable",
		},
		{
			category: 255,
			code:     connect.CodeInternal,
			message:  "internal server error",
		},
	}
	for _, tt := range tests {
		if got := connectx.Code(tt.category); got != tt.code {
			t.Errorf("Code(%v) = %v, want %v", tt.category, got, tt.code)
		}
		err := &apperror.Error{
			Category: tt.category,
			Code:     "PUBLIC_CODE",
		}
		if got := connectx.FromApplicationError(err, connectx.ErrorOptions{}).Message(); got != tt.message {
			t.Errorf("message for %v = %q, want %q", tt.category, got, tt.message)
		}
	}
}

func TestFromApplicationErrorTraversalAndIdentity(t *testing.T) {
	cause := &domainError{}
	left := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "LEFT",
		Cause:    cause,
	}
	right := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "RIGHT",
	}
	unknown := &apperror.Error{
		Category: 255,
		Code:     "HIDDEN",
	}
	outer := &apperror.Error{
		Category: apperror.Conflict,
		Code:     "OUTER",
		Cause:    errors.Join(left, right),
	}
	tests := []struct {
		name    string
		err     error
		code    connect.Code
		message string
	}{
		{
			name:    "outer",
			err:     outer,
			code:    connect.CodeAborted,
			message: "conflict",
		},
		{
			name:    "left first",
			err:     errors.Join(fmt.Errorf("load: %w", left), right),
			code:    connect.CodeNotFound,
			message: "not found",
		},
		{
			name:    "reverse",
			err:     errors.Join(right, left),
			code:    connect.CodeInvalidArgument,
			message: "invalid",
		},
		{
			name:    "unknown first",
			err:     errors.Join(unknown, right),
			code:    connect.CodeInternal,
			message: "internal server error",
		},
		{
			name:    "unclassified",
			err:     errors.New("CANARY_UNCLASSIFIED"),
			code:    connect.CodeInternal,
			message: "internal server error",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				got := connectx.FromApplicationError(tt.err, connectx.ErrorOptions{})
				if got.Code() != tt.code || got.Message() != tt.message || !errors.Is(got, tt.err) {
					t.Fatalf("code %v, message %q, identity %v", got.Code(), got.Message(), errors.Is(got, tt.err))
				}
				if len(got.Details()) != 0 || strings.Contains(got.Error(), "CANARY") {
					t.Fatalf("public error contains details or diagnostics: %v", got)
				}
			},
		)
	}
	converted := connectx.FromApplicationError(outer, connectx.ErrorOptions{})
	var found *domainError
	if !errors.Is(converted, left) || !errors.As(converted, &found) || found != cause {
		t.Fatal("sentinel identity or typed cause is not reachable")
	}
}

func TestMessageOverrideForAllOutcomes(t *testing.T) {
	failures := []error{
		&apperror.Error{
			Category: apperror.NotFound,
			Code:     "NOT_FOUND",
			Message:  "application message",
		},
		&apperror.Error{
			Category: apperror.Internal,
			Message:  "CANARY_MESSAGE",
		},
		&apperror.Error{
			Category: 255,
			Message:  "CANARY_MESSAGE",
		},
		errors.New("CANARY_CAUSE"),
	}
	for _, failure := range failures {
		got := connectx.FromApplicationError(
			failure,
			connectx.ErrorOptions{
				Message: "fixed public message",
			},
		)
		if got.Message() != "fixed public message" || !errors.Is(got, failure) {
			t.Errorf("got %v for %v", got, failure)
		}
	}
}

func TestHandlerClientRoundTrip(t *testing.T) {
	const procedure = "/example.v1.Books/Read"
	cause := &domainError{}
	source := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "CANARY_CODE",
		Message:  "CANARY_MESSAGE",
		Violations: []apperror.Violation{
			{
				Field:   "CANARY_FIELD",
				Rule:    "CANARY_RULE",
				Param:   "CANARY_PARAM",
				Message: "CANARY_VIOLATION",
			},
		},
		Cause: cause,
	}
	opts := connectx.ErrorOptions{
		Message: "fixed public message",
	}
	serverError := connectx.FromApplicationError(source, opts)
	if !errors.Is(serverError, source) || !errors.Is(serverError, cause) {
		t.Fatal("server error lost original cause")
	}
	handler := connect.NewUnaryHandler(
		procedure,
		func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			return nil, serverError
		},
	)
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	protocols := []struct {
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
	}
	for _, protocol := range protocols {
		t.Run(
			protocol.name,
			func(t *testing.T) {
				client := connect.NewClient[emptypb.Empty, emptypb.Empty](server.Client(), server.URL+procedure, protocol.opts...)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
				var received *connect.Error
				if !errors.As(err, &received) {
					t.Fatalf("expected connect error, got %v", err)
				}
				if received.Code() != connect.CodeInvalidArgument || received.Message() != "fixed public message" {
					t.Fatalf("client code %v, message %q", received.Code(), received.Message())
				}
				if len(received.Details()) != 0 || strings.Contains(received.Error(), "CANARY") {
					t.Fatalf("client error exposed private data: %v", received)
				}
			},
		)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+procedure, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	wire, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || strings.Contains(string(wire), "CANARY") ||
		!strings.Contains(string(wire), "fixed public message") {
		t.Fatalf("wire status %d, body %s", response.StatusCode, wire)
	}
}

func TestRoundTripDefaultMessageAndInternalSuppression(t *testing.T) {
	const procedure = "/example.v1.Books/Read"
	tests := []struct {
		name    string
		err     error
		code    connect.Code
		message string
	}{
		{
			name: "internal",
			err: &apperror.Error{
				Category: apperror.Internal,
				Code:     "CANARY_CODE",
				Message:  "CANARY_MESSAGE",
				Violations: []apperror.Violation{
					{
						Field: "CANARY_FIELD",
						Rule:  "CANARY_RULE",
					},
				},
				Cause: errors.New("CANARY_CAUSE"),
			},
			code:    connect.CodeInternal,
			message: "internal server error",
		},
		{
			name: "unknown",
			err: &apperror.Error{
				Category: 255,
				Code:     "CANARY_CODE",
				Message:  "CANARY_MESSAGE",
			},
			code:    connect.CodeInternal,
			message: "internal server error",
		},
		{
			name:    "unclassified",
			err:     errors.New("CANARY_CAUSE"),
			code:    connect.CodeInternal,
			message: "internal server error",
		},
		{
			name: "not found",
			err: &apperror.Error{
				Category: apperror.NotFound,
				Code:     "CANARY_CODE",
				Message:  "book not found",
				Cause:    errors.New("CANARY_CAUSE"),
			},
			code:    connect.CodeNotFound,
			message: "book not found",
		},
		{
			name: "not found default",
			err: &apperror.Error{
				Category: apperror.NotFound,
				Code:     "CANARY_CODE",
			},
			code:    connect.CodeNotFound,
			message: "not found",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				handler := connect.NewUnaryHandler(
					procedure,
					func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
						return nil, connectx.FromApplicationError(tt.err, connectx.ErrorOptions{})
					},
				)
				server := httptest.NewServer(handler)
				t.Cleanup(server.Close)
				client := connect.NewClient[emptypb.Empty, emptypb.Empty](server.Client(), server.URL+procedure)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
				var received *connect.Error
				if !errors.As(err, &received) || received.Code() != tt.code || received.Message() != tt.message {
					t.Fatalf("client error: %v", err)
				}
				if len(received.Details()) != 0 || strings.Contains(received.Error(), "CANARY") {
					t.Fatalf("client error exposed private data: %v", received)
				}
			},
		)
	}
}

func TestConcurrentConversions(t *testing.T) {
	cause := errors.New("cause")
	source := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "INVALID",
		Violations: []apperror.Violation{
			{
				Field: "quantity",
				Rule:  apperror.RuleGTE,
				Param: "1",
			},
		},
		Cause: cause,
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := connectx.FromApplicationError(source, connectx.ErrorOptions{})
			if got.Code() != connect.CodeInvalidArgument || !errors.Is(got, cause) || len(got.Details()) != 0 {
				t.Error("conversion lost code or cause")
			}
		}()
	}
	wg.Wait()
	if source.Violations[0].Param != "1" || source.Cause != cause {
		t.Fatal("conversion mutated original")
	}
}
