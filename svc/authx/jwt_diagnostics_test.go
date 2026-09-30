package authx_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/authx"
)

func TestJWKSDiagnosticsExcludeEndpointAndResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("body-secret-marker\ninjected-log-marker"))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL + "/keys?credential=query-secret-marker")
	if err != nil {
		t.Fatal(err)
	}
	endpoint.User = url.UserPassword("user-secret-marker", "password-secret-marker")
	logger := &diagnosticLogger{warnings: make(chan struct{}, 8), redact: []string{endpoint.String(), endpoint.Redacted()}}
	ready := make(chan struct{})
	key, err := parseECDSAPublicKeyFromPrivateKey(ecdsaPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	method, err := authx.NewJWTMethod(
		authx.WithJWKSSource(endpoint), authx.WithJWKSMaxRetries(1),
		authx.WithJWKSSourceReadySignal(ready), authx.WithLogger(logger),
		authx.WithJWTPublicKey(&key.PublicKey),
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("constructor did not finish")
	}
	select {
	case <-logger.warnings:
	case <-time.After(3 * time.Second):
		t.Fatal("JWKS failure was not reported")
	}
	if _, err := method.Authenticate(context.Background(), validToken); err != nil {
		t.Fatalf("legacy fallback failed: %v", err)
	}
	logger.mu.Lock()
	logs := strings.Join(logger.entries, "\n")
	logger.mu.Unlock()
	for _, marker := range []string{"body-secret-marker", "injected-log-marker", "query-secret-marker", "user-secret-marker", "password-secret-marker"} {
		if strings.Contains(logs, marker) {
			t.Errorf("diagnostics exposed %q", marker)
		}
	}
	if !strings.Contains(logs, "400") || !strings.Contains(logs, "fallback") {
		t.Errorf("diagnostics lost HTTP status or fallback outcome: %s", logs)
	}
}

type diagnosticLogger struct {
	mu       sync.Mutex
	entries  []string
	records  []map[string]any
	redact   []string
	warnings chan struct{}
}

func (l *diagnosticLogger) record(level, message string, fields ...any) {
	l.mu.Lock()
	record := make(map[string]any)
	for i := 0; i+1 < len(fields); i += 2 {
		if key, ok := fields[i].(string); ok {
			record[key] = fields[i+1]
		}
	}
	l.records = append(l.records, record)
	text := fmt.Sprint(level, " ", message, " ", fields)
	for _, value := range l.redact {
		text = strings.ReplaceAll(text, value, "[redacted]")
	}
	l.entries = append(l.entries, strconv.Quote(text))
	l.mu.Unlock()
	if level == "warn" {
		select {
		case l.warnings <- struct{}{}:
		default:
		}
	}
}

func (l *diagnosticLogger) Info(message string, fields ...any) { l.record("info", message, fields...) }
func (l *diagnosticLogger) Warn(message string, fields ...any) { l.record("warn", message, fields...) }
func (l *diagnosticLogger) Error(message string, fields ...any) {
	l.record("error", message, fields...)
}
func (l *diagnosticLogger) Debug(message string, fields ...any) {
	l.record("debug", message, fields...)
}
