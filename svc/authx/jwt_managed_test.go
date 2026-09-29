package authx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestManagedJWTMethodReadyAndLifetime(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		jwksHandler(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	method, err := authx.NewManagedJWTMethod(ctx, authx.ManagedJWTMethodOptions{
		Endpoint:            server.URL,
		JWKSOptions:         authentication.JWKSOptions{RefreshInterval: time.Hour},
		ValidSigningMethods: []authx.JWTSigningMethod{authx.JWTSigningMethodES256},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("constructor completed without one initial load: %d", requests.Load())
	}
	methodWithErrors := authx.NewErrorAdapter(method)
	if _, err := methodWithErrors.Authenticate(context.Background(), validToken); err != nil {
		t.Fatalf("returned method was not ready: %v", err)
	}
	cancel()
	if _, err := methodWithErrors.Authenticate(context.Background(), validToken); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached key remained usable after lifetime cancellation: %v", err)
	}
}

func TestManagedJWTMethodReportsInitialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	logger := &diagnosticLogger{warnings: make(chan struct{}, 1)}
	method, err := authx.NewManagedJWTMethod(context.Background(), authx.ManagedJWTMethodOptions{Endpoint: server.URL, Log: logger})
	if err == nil || method != nil {
		t.Fatalf("initial failure was not returned: method=%v err=%v", method, err)
	}
	select {
	case <-logger.warnings:
	case <-time.After(3 * time.Second):
		t.Fatal("managed startup failure was not logged")
	}
	logger.mu.Lock()
	logs := strings.Join(logger.entries, "\n")
	logger.mu.Unlock()
	if !strings.Contains(logs, "503") || !strings.Contains(logs, "http_status") {
		t.Fatalf("startup diagnostic lost failure details: %s", logs)
	}
}

func TestManagedJWTMethodCancelsInitialLoad(t *testing.T) {
	arrived := make(chan struct{})
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := authx.NewManagedJWTMethod(ctx, authx.ManagedJWTMethodOptions{Endpoint: server.URL})
		done <- err
	}()
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("initial load did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("initial cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("constructor did not honor lifetime cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("initial HTTP request did not stop")
	}
}

func TestManagedJWTMethodSigningOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(jwksHandler))
	defer server.Close()
	for _, tc := range []struct {
		name           string
		methods        []authx.JWTSigningMethod
		invalidOptions bool
		rejectedToken  bool
	}{
		{name: "defaults"},
		{name: "explicit empty", methods: []authx.JWTSigningMethod{}, invalidOptions: true},
		{name: "restricted", methods: []authx.JWTSigningMethod{authx.JWTSigningMethodRS256}, rejectedToken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			method, err := authx.NewManagedJWTMethod(ctx, authx.ManagedJWTMethodOptions{Endpoint: server.URL, ValidSigningMethods: tc.methods})
			if tc.invalidOptions {
				if err == nil {
					t.Fatal("explicit empty allowlist accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = method.Authenticate(context.Background(), validToken)
			if (err != nil) != tc.rejectedToken {
				t.Fatalf("unexpected signing policy result: %v", err)
			}
		})
	}
}

func ExampleNewManagedJWTMethod() {
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	sourceOptions := authentication.JWKSOptions{
		Client:                 &http.Client{Timeout: 5 * time.Second},
		RefreshInterval:        time.Minute,
		MaxResponseBytes:       1 << 20,
		MaxCacheAge:            10 * time.Minute,
		RequestOnUnknownKID:    true,
		ReserveRefreshCapacity: true,
	}
	sourceOptions.SetRefreshRateLimit(5, time.Minute)
	method, err := authx.NewManagedJWTMethod(lifetime, authx.ManagedJWTMethodOptions{
		Endpoint:            "https://issuer.example/jwks",
		ValidSigningMethods: []authx.JWTSigningMethod{authx.JWTSigningMethodRS256},
		JWKSOptions:         sourceOptions,
	})
	if err != nil {
		return
	} // Report startup failure at the application boundary.
	_ = method // Install the ready method in the application's authentication boundary.
}
