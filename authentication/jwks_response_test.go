package authentication

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJWKSInvalidResponsePreservesKeys(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"keys":null}`, `{"keys":{}}`, `[]`, `{"keys":`, `{"keys":[{"kty":"unsupported","kid":"sensitive"}]}`} {
		t.Run(body, func(t *testing.T) {
			source := responseTestSource(t)
			source.client = concurrencyClient(func(*http.Request) (*http.Response, error) { return responseWithBody(body), nil })
			if err := source.requestKeys(context.Background()); err == nil {
				t.Fatal("invalid response accepted")
			}
			if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
				t.Fatalf("valid cached key lost: %v", err)
			}
		})
	}
}

func TestJWKSValidRotationRemovesAbsentKeys(t *testing.T) {
	for _, body := range []string{`{"keys":[]}`, strings.ReplaceAll(concurrencyJWKS, "known", "rotated")} {
		source := responseTestSource(t)
		source.client = concurrencyClient(func(*http.Request) (*http.Response, error) { return responseWithBody(body), nil })
		if err := source.requestKeys(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("removed key remains: %v", err)
		}
		if strings.Contains(body, "rotated") {
			if _, err := source.FetchPublicKey(context.Background(), "rotated"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestJWKSDiagnosticsExcludeSensitiveValues(t *testing.T) {
	dependency := &responseDependencyError{message: "sensitive-token\nforged-log"}
	tests := []struct {
		name, stage, reason, body string
		status                    int
		failure                   error
		readFailure               bool
	}{
		{name: "status", stage: "response", reason: "http_status", status: 503, body: dependency.message},
		{name: "read", stage: "read", reason: "body_read_failed", status: 200, failure: dependency, readFailure: true},
		{name: "decode", stage: "decode", reason: "invalid_json", status: 200, body: `{"keys": "` + dependency.message},
		{name: "schema", stage: "validate", reason: "invalid_envelope", status: 200, body: `{"keys":null}`},
		{name: "key decode", stage: "decode", reason: "invalid_keys", status: 200, body: `{"keys":[{"kty":"sensitive-token"}]}`},
		{name: "network", stage: "request", reason: "request_failed", failure: dependency},
		{name: "context", stage: "request", reason: "request_failed", failure: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := responseTestSource(t)
			source.url = "https://username:password@example.test/keys?token=sensitive-token"
			source.client = concurrencyClient(func(*http.Request) (*http.Response, error) {
				if tc.failure != nil && !tc.readFailure {
					return nil, tc.failure
				}
				response := responseWithBody(tc.body)
				response.StatusCode = tc.status
				if tc.readFailure {
					response.Body = responseFailureBody{err: tc.failure}
				}
				return response, nil
			})
			_, _, err := source.loadKeys(context.Background())
			if err == nil {
				t.Fatal("expected failure")
			}
			message := err.Error()
			for _, forbidden := range []string{"sensitive-token", "forged-log", "username", "password", "?token=", "\n"} {
				if strings.Contains(message, forbidden) {
					t.Errorf("unsafe diagnostic %q", message)
				}
			}
			for _, field := range []string{"operation=jwks_refresh", "stage=" + tc.stage, "reason=" + tc.reason, "outcome=failed"} {
				if !strings.Contains(message, field) {
					t.Errorf("missing %q in %q", field, message)
				}
			}
			if tc.failure != nil && !errors.Is(err, tc.failure) {
				t.Errorf("original cause lost: %v", err)
			}
			if tc.failure == dependency {
				var typed *responseDependencyError
				if !errors.As(err, &typed) || typed != dependency {
					t.Error("typed cause lost")
				}
			}
		})
	}
}

func TestJWKSRejectsStatusBeforeReadingBody(t *testing.T) {
	body := &responseTrackedBody{}
	source := responseTestSource(t)
	source.client = concurrencyClient(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 503, Body: body}, nil })
	if err := source.requestKeys(context.Background()); err == nil {
		t.Fatal("expected status error")
	}
	if body.read {
		t.Error("non-success body was read")
	}
	if !body.closed {
		t.Error("body was not closed")
	}
}

func TestJWKSResponseSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     int64
		body      string
		wantError bool
	}{
		{name: "over limit", limit: int64(len(concurrencyJWKS)), body: concurrencyJWKS + " ", wantError: true},
		{name: "at limit", limit: int64(len(concurrencyJWKS)), body: concurrencyJWKS},
		{name: "unlimited", body: concurrencyJWKS + strings.Repeat(" ", 1<<20)},
		{name: "largest limit", limit: maxInt64, body: concurrencyJWKS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := true
			var bytesRead int
			source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, MaxResponseBytes: tc.limit, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
				if initial {
					initial = false
					return concurrencyResponse(), nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(&responseCountingReader{Reader: strings.NewReader(tc.body), count: &bytesRead})}, nil
			})})
			defer source.Stop()
			err := source.requestKeys(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if tc.wantError && !strings.Contains(err.Error(), "reason=response_too_large") {
				t.Fatalf("unexpected error %v", err)
			}
			if tc.limit > 0 && tc.limit < maxInt64 && int64(bytesRead) > tc.limit+1 {
				t.Fatalf("read %d bytes with limit %d", bytesRead, tc.limit)
			}
			if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
				t.Fatalf("cached key lost: %v", err)
			}
		})
	}
}

func TestJWKSRefreshWarningsAreSafeAndOncePerFailure(t *testing.T) {
	warnings := make(chan string, 4)
	source := NewKeySourceJWKS("https://user:secret@example.test/keys?secret=value", &JWKSOptions{RefreshInterval: time.Hour, WarnFunc: func(message string) { warnings <- message }, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: responseFailureBody{err: errors.New("secret"), closeErr: errors.New("secret-close")}}, nil
	})})
	defer source.Stop()
	select {
	case message := <-warnings:
		if !strings.Contains(message, "stage=response reason=http_status outcome=failed status=503") || strings.Contains(message, "secret") {
			t.Fatalf("unsafe or incomplete warning %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("initial failure was not logged")
	}
	if err := source.requestKeys(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	select {
	case message := <-warnings:
		if !strings.Contains(message, "reason=http_status") {
			t.Fatalf("unexpected warning %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit refresh failure was not logged")
	}
	select {
	case message := <-warnings:
		t.Fatalf("duplicate warning %q", message)
	default:
	}
}

func TestJWKSCloseWarningExcludesCause(t *testing.T) {
	warnings := make(chan string, 1)
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, WarnFunc: func(message string) { warnings <- message }, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: responseCloseFailureBody{Reader: strings.NewReader(concurrencyJWKS)}}, nil
	})})
	defer source.Stop()
	select {
	case message := <-warnings:
		if message != "operation=jwks_refresh stage=close reason=body_close_failed outcome=warning" {
			t.Fatalf("unsafe warning %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("missing close warning")
	}
}

func TestJWKSRateLimitErrorExcludesKeyID(t *testing.T) {
	options := &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil })}
	options.SetRefreshRateLimit(1, time.Hour)
	source := NewKeySourceJWKS("https://example.test/keys", options)
	defer source.Stop()
	_, err := source.FetchPublicKey(context.Background(), "sensitive-key\nforged-log")
	if errors.Unwrap(err) != ErrKeyNotFound || strings.Contains(err.Error(), "sensitive-key") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("unsafe rate-limit error %v", err)
	}
}

func responseTestSource(t *testing.T) *KeySourceJWKS {
	t.Helper()
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil })})
	t.Cleanup(source.Stop)
	return source
}
func responseWithBody(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}

type responseDependencyError struct{ message string }

func (e *responseDependencyError) Error() string { return e.message }

type responseFailureBody struct {
	err      error
	closeErr error
}

func (b responseFailureBody) Read([]byte) (int, error) { return 0, b.err }
func (b responseFailureBody) Close() error             { return b.closeErr }

type responseTrackedBody struct{ read, closed bool }

func (b *responseTrackedBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *responseTrackedBody) Close() error             { b.closed = true; return nil }

type responseCountingReader struct {
	io.Reader
	count *int
}

func (r *responseCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	*r.count += n
	return n, err
}

type responseCloseFailureBody struct{ io.Reader }

func (responseCloseFailureBody) Close() error { return errors.New("sensitive-close\nforged-log") }
