package authentication

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJWKSErrorPreservesEndpointCause(t *testing.T) {
	_, err := NewManagedKeySourceJWKS("https://user:secret@example.test/%zz?token=sensitive")
	var diagnostic *JWKSError
	var escape url.EscapeError
	if !errors.As(err, &diagnostic) || !errors.As(err, &escape) {
		t.Fatalf("endpoint diagnostic or URL cause lost: %v", err)
	}
	if diagnostic.Operation() != "jwks_config" || diagnostic.Stage() != "config" || diagnostic.Reason() != "invalid_endpoint" || diagnostic.Outcome() != "failed" || diagnostic.HTTPStatus() != 0 {
		t.Fatalf("unexpected endpoint diagnostic: %v", diagnostic)
	}
	assertSafeJWKSError(t, err)
}

func TestJWKSErrorPreservesRateLimitAndCategory(t *testing.T) {
	diagnostics := make(chan *JWKSError, 2)
	opts := &JWKSOptions{RequestOnUnknownKID: true, RefreshInterval: time.Hour,
		Client:         concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil }),
		DiagnosticFunc: func(e *JWKSError) { diagnostics <- e },
	}
	opts.SetRefreshRateLimit(1, time.Hour)
	source := NewKeySourceJWKS("https://example.test/keys", opts)
	defer source.Stop()
	_, err := source.FetchPublicKey(context.Background(), "sensitive\nforged-log")
	if errors.Unwrap(err) != ErrKeyNotFound || !errors.Is(err, ErrJWKSRateLimited) || !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("rate-limit/category cause lost: %v", err)
	}
	var diagnostic *JWKSError
	if !errors.As(err, &diagnostic) || diagnostic.Stage() != "limit" || diagnostic.Reason() != "rate_limit" {
		t.Fatalf("rate-limit diagnostic lost: %v", err)
	}
	if got := awaitJWKSDiagnostic(t, diagnostics); got != diagnostic {
		t.Fatalf("callback and lookup do not share the diagnostic: %v", got)
	}
	assertSafeJWKSError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := source.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatal("duplicate rate-limit diagnostic")
	}
}

func TestJWKSErrorPreservesPrimaryAndCloseCauses(t *testing.T) {
	readFailure := &responseDependencyError{message: "sensitive read\nforged-log"}
	closeFailure := &responseDependencyError{message: "secret close\nforged-log"}
	for _, tc := range []struct {
		name, stage, reason string
		status              int
		reader              func() io.Reader
		primary             error
		closeCanceled       bool
	}{
		{"status", "response", "http_status", 503, func() io.Reader { return strings.NewReader("sensitive response") }, nil, false},
		{"status_with_canceled_close", "response", "http_status", 503, func() io.Reader { return strings.NewReader("sensitive response") }, nil, true},
		{"read", "read", "body_read_failed", 200, func() io.Reader { return responseFailureBody{err: readFailure} }, readFailure, false},
		{"json", "decode", "invalid_json", 200, func() io.Reader { return strings.NewReader(`{"keys":`) }, nil, false},
		{"success", "close", "body_close_failed", 200, func() io.Reader { return strings.NewReader(concurrencyJWKS) }, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var closeCause error = closeFailure
			if tc.closeCanceled {
				closeCause = context.Canceled
			}
			diagnostics := make(chan *JWKSError, 2)
			var warnings atomic.Int32
			source, err := NewManagedKeySourceJWKS("https://user:secret@example.test/keys?token=sensitive", &JWKSOptions{
				RefreshInterval: time.Hour,
				Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: shutdownBody{Reader: tc.reader(), close: func() error { return closeCause }}}, nil
				}),
				DiagnosticFunc: func(e *JWKSError) { diagnostics <- e },
				WarnFunc:       func(string) { warnings.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer source.Stop()
			err = source.Start(context.Background())
			diagnostic := awaitJWKSDiagnostic(t, diagnostics)
			if diagnostic.Stage() != tc.stage || diagnostic.Reason() != tc.reason || diagnostic.Operation() != "jwks_refresh" {
				t.Fatalf("primary diagnostic lost: %v", diagnostic)
			}
			if !errors.Is(diagnostic, closeCause) {
				t.Fatalf("callback lost close cause: %v", diagnostic)
			}
			if tc.name == "success" {
				if err != nil || diagnostic.Outcome() != "warning" {
					t.Fatalf("close failure rejected valid keys: %v, %v", err, diagnostic)
				}
				if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || diagnostic.Outcome() != "failed" || !errors.Is(err, closeCause) {
					t.Fatalf("returned error lost close cause: %v", err)
				}
				if tc.primary != nil && (!errors.Is(err, tc.primary) || !errors.Is(diagnostic, tc.primary)) {
					t.Fatalf("primary cause lost: %v", err)
				}
				if tc.name == "json" {
					var syntax *json.SyntaxError
					if !errors.As(err, &syntax) {
						t.Fatal("JSON cause lost")
					}
				}
				if tc.status == 503 && diagnostic.HTTPStatus() != 503 {
					t.Fatal("HTTP status lost")
				}
				assertSafeJWKSError(t, err)
			}
			assertSafeJWKSError(t, diagnostic)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := source.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if warnings.Load() != 0 || len(diagnostics) != 0 {
				t.Fatal("duplicate diagnostic or WarnFunc invoked despite DiagnosticFunc")
			}
		})
	}
}

func TestJWKSShutdownWaitsForStructuredDiagnostic(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var source *KeySourceJWKS
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: shutdownBody{Reader: strings.NewReader(concurrencyJWKS), close: func() error { return io.ErrUnexpectedEOF }}}, nil
		}),
		DiagnosticFunc: func(*JWKSError) { source.Stop(); close(entered); <-release; close(finished) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	if err := source.Start(context.Background()); err != nil && !errors.Is(err, ErrKeySourceStopped) && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	awaitRefreshSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := source.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("structured callback was not awaited: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := source.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("callback still running")
	}
}

func TestJWKSCancellationStillReportsCloseFailure(t *testing.T) {
	diagnostics := make(chan *JWKSError, 2)
	closeFailure := &responseDependencyError{message: "sensitive close"}
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: responseFailureBody{err: context.Canceled, closeErr: closeFailure}}, nil
		}),
		DiagnosticFunc: func(e *JWKSError) { diagnostics <- e },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	err = source.Start(context.Background())
	if !errors.Is(err, context.Canceled) || !errors.Is(err, closeFailure) {
		t.Fatalf("cancellation or close cause lost: %v", err)
	}
	diagnostic := awaitJWKSDiagnostic(t, diagnostics)
	if diagnostic.Stage() != "close" || diagnostic.Outcome() != "warning" || !errors.Is(diagnostic, closeFailure) {
		t.Fatalf("close failure was suppressed with cancellation: %v", diagnostic)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := source.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatal("duplicate diagnostic")
	}
}

func assertSafeJWKSError(t *testing.T, err error) {
	t.Helper()
	for _, forbidden := range []string{"user", "secret", "sensitive", "forged-log", "\n", "%zz", "?token="} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("unsafe diagnostic: %q", err.Error())
		}
	}
}

func awaitJWKSDiagnostic(t *testing.T, ch <-chan *JWKSError) *JWKSError {
	t.Helper()
	select {
	case diagnostic := <-ch:
		return diagnostic
	case <-time.After(2 * time.Second):
		t.Fatal("missing diagnostic")
		return nil
	}
}
