package authx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestManagedJWKSStructuredLogs(t *testing.T) {
	sensitive := "credential-secret\nforged-entry"
	for _, tc := range []struct {
		name, stage, reason, outcome  string
		status                        int
		readErr, closeErr, requestErr error
		ready                         bool
		body, field                   string
	}{
		{name: "network", stage: "request", reason: "request_failed", outcome: "failed", requestErr: errors.New("connection refused credential=" + sensitive)},
		{name: "read", stage: "read", reason: "body_read_failed", outcome: "failed", status: 200, readErr: errors.New("read interrupted credential=" + sensitive)},
		{name: "read and close", stage: "read", reason: "body_read_failed", outcome: "failed", status: 200, readErr: errors.New("read interrupted credential=" + sensitive), closeErr: errors.New("close interrupted credential=" + sensitive)},
		{name: "HTTP", stage: "response", reason: "http_status", outcome: "failed", status: 503},
		{name: "HTTP with canceled close", stage: "response", reason: "http_status", outcome: "failed", status: 503, closeErr: context.Canceled},
		{name: "close", stage: "close", reason: "body_close_failed", outcome: "warning", status: 200, closeErr: errors.New("close interrupted credential=" + sensitive), ready: true},
		{name: "metadata", stage: "validate", reason: "invalid_metadata", outcome: "failed", status: 200, body: `{"keys":[{"kid":7}]}`, field: "keys[0].kid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := &diagnosticLogger{warnings: make(chan struct{}, 8), redact: []string{sensitive}}
			body := tc.body
			if body == "" {
				body = `{"keys":[]}`
			}
			client := diagnosticHTTPClient(func(*http.Request) (*http.Response, error) {
				if tc.requestErr != nil {
					return nil, tc.requestErr
				}
				return &http.Response{StatusCode: tc.status, Body: diagnosticBody{reader: strings.NewReader(body), readErr: tc.readErr, closeErr: tc.closeErr}}, nil
			})
			options := authentication.JWKSOptions{Client: client, RefreshInterval: time.Hour}
			if tc.field != "" {
				options.VerificationPolicy = &authentication.JWKSVerificationPolicy{AllowedAlgorithms: []string{"ES256"}}
			}
			method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), authx.ManagedJWTMethodOptions{
				Endpoint:    "https://user-secret:password-secret@issuer.example/keys?token=query-secret",
				JWKSOptions: options, Log: logger,
			})
			if shutdown == nil {
				t.Fatalf("source cleanup missing: %v", err)
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if closeErr := shutdown(cleanup); closeErr != nil {
				t.Fatal(closeErr)
			}
			if (err == nil) != tc.ready || (method != nil) != tc.ready {
				t.Fatalf("unexpected startup result: method=%v err=%v", method, err)
			}
			logger.mu.Lock()
			defer logger.mu.Unlock()
			if len(logger.entries) != 1 {
				t.Fatalf("expected one event, got %v", logger.entries)
			}
			log := logger.entries[0]
			record := logger.records[0]
			logged, ok := record["error"].(error)
			if !ok || record["source"] != "JWKS" {
				t.Fatalf("lost diagnostic object or source: %v", record)
			}
			if tc.field != "" && record["field"] != tc.field {
				t.Fatalf("logger lost validation field %q: %v", tc.field, record)
			}
			for _, original := range []error{tc.requestErr, tc.readErr, tc.closeErr} {
				if original == nil {
					continue
				}
				if !errors.Is(logged, original) {
					t.Errorf("logged diagnostic lost original cause: %v", record)
				}
				cause, ok := record["cause"].(error)
				if !ok || !errors.Is(cause, original) {
					t.Errorf("logger cannot inspect original cause: %v", record)
				}
				useful := strings.Split(original.Error(), " credential=")[0]
				if !strings.Contains(log, useful) {
					t.Errorf("redaction erased failure detail %q: %s", useful, log)
				}
			}
			for _, field := range []string{"operation jwks_refresh", "stage " + tc.stage, "reason " + tc.reason, "outcome " + tc.outcome} {
				if !strings.Contains(log, field) {
					t.Errorf("missing structured field %q in %s", field, log)
				}
			}
			if tc.status == 503 && !strings.Contains(log, "status 503") {
				t.Errorf("missing HTTP status: %s", log)
			}
			for _, marker := range []string{"credential-secret", "forged-entry", "user-secret", "password-secret", "query-secret", "issuer.example", "keys\":[]"} {
				if strings.Contains(log, marker) {
					t.Errorf("sensitive value %q in log %s", marker, log)
				}
			}
		})
	}
}

func TestManagedJWKSDiagnosticCallbackPrecedence(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(map[bool]string{false: "warning", true: "structured"}[structured], func(t *testing.T) {
			logger := &diagnosticLogger{warnings: make(chan struct{}, 8)}
			warnings, diagnostics := 0, 0
			opts := authentication.JWKSOptions{
				Client:   diagnosticHTTPClient(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-cause") }),
				WarnFunc: func(string) { warnings++ },
			}
			if structured {
				opts.DiagnosticFunc = func(event *authentication.JWKSError) {
					diagnostics++
					if event.Stage() != "request" || event.Reason() != "request_failed" {
						t.Errorf("wrong diagnostic: %v", event)
					}
				}
			}
			_, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), authx.ManagedJWTMethodOptions{Endpoint: "https://issuer.example/jwks", JWKSOptions: opts, Log: logger})
			if err == nil || shutdown == nil {
				t.Fatalf("expected startup failure and cleanup: %v", err)
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := shutdown(cleanup); err != nil {
				t.Fatal(err)
			}
			expectedWarnings, expectedDiagnostics := 1, 0
			if structured {
				expectedWarnings, expectedDiagnostics = 0, 1
			}
			if warnings != expectedWarnings || diagnostics != expectedDiagnostics {
				t.Fatalf("callbacks: warnings=%d diagnostics=%d", warnings, diagnostics)
			}
			logger.mu.Lock()
			defer logger.mu.Unlock()
			if len(logger.entries) != 0 {
				t.Fatalf("user callback must suppress logger: %v", logger.entries)
			}
		})
	}
}

type diagnosticHTTPClient func(*http.Request) (*http.Response, error)

func (f diagnosticHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

type diagnosticBody struct {
	reader            io.Reader
	readErr, closeErr error
}

func (b diagnosticBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.reader.Read(p)
}
func (b diagnosticBody) Close() error { return b.closeErr }
