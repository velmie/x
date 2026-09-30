package authx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestManagedJWTMethodWithShutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(jwksHandler))
	defer server.Close()
	method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), authx.ManagedJWTMethodOptions{
		Endpoint:            server.URL,
		JWKSOptions:         authentication.JWKSOptions{RefreshInterval: time.Hour},
		ValidSigningMethods: []authx.JWTSigningMethod{authx.JWTSigningMethodES256},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer shutdown(ctx)
	adapted := authx.NewErrorAdapter(method)
	if _, err := adapted.Authenticate(context.Background(), validToken); err != nil {
		t.Fatalf("ready method rejected JWT: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := adapted.Authenticate(context.Background(), validToken); !errors.Is(err, authentication.ErrKeySourceStopped) {
		t.Fatalf("shutdown method did not reject JWT: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("repeated shutdown: %v", err)
	}
}

func TestManagedJWTMethodShutdownAfterInitialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), authx.ManagedJWTMethodOptions{
		Endpoint:    server.URL,
		JWKSOptions: authentication.JWKSOptions{WarnFunc: func(string) { close(entered); <-release }},
	})
	if err == nil || method != nil || shutdown == nil {
		t.Fatalf("startup failure must return cleanup: method=%v shutdown=%v err=%v", method, shutdown != nil, err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("warning did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown must wait for warning: %v", err)
	}
	close(release)
	released = true
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown after warning finished: %v", err)
	}
}

func TestManagedJWTMethodShutdownConfigurationFailure(t *testing.T) {
	for _, opts := range []authx.ManagedJWTMethodOptions{
		{},
		{Endpoint: "https://issuer.example/jwks", ValidSigningMethods: []authx.JWTSigningMethod{}},
		{Endpoint: "%zz"},
	} {
		method, shutdown, err := authx.NewManagedJWTMethodWithShutdown(context.Background(), opts)
		if err == nil || method != nil || shutdown != nil {
			t.Fatalf("configuration failure created lifecycle: method=%v shutdown=%v err=%v", method, shutdown != nil, err)
		}
	}
}
