package authx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velmie/x/authentication"
	"github.com/velmie/x/svc/authx"
)

func TestManagedJWTMethodFallbackRequiresConfirmedAbsence(t *testing.T) {
	key, err := parseECDSAPublicKeyFromPrivateKey(ecdsaPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dependency := errors.New("dependency-secret-marker\ninjected-log-marker")
	for _, tc := range []struct {
		name    string
		failure error
		limited bool
		empty   bool
		allow   bool
	}{
		{name: "confirmed absence after refresh", allow: true},
		{name: "valid empty snapshot", empty: true, allow: true},
		{name: "dependency failure", failure: dependency},
		{name: "rate limit", limited: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			client := managedHTTPClient(func(req *http.Request) (*http.Response, error) {
				if requests.Add(1) > 1 && tc.failure != nil {
					return nil, tc.failure
				}
				if tc.empty {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"keys":[]}`))}, nil
				}
				return managedJWKSResponse(req), nil
			})
			sourceOptions := authentication.JWKSOptions{Client: client, RefreshInterval: time.Hour, RequestOnUnknownKID: true}
			if tc.limited {
				sourceOptions.SetRefreshRateLimit(1, time.Hour)
			}
			lifetime, stop := context.WithCancel(context.Background())
			defer stop()
			logger := &diagnosticLogger{warnings: make(chan struct{}, 8)}
			method, err := authx.NewManagedJWTMethod(lifetime, authx.ManagedJWTMethodOptions{
				Endpoint:          "https://issuer.example/jwks",
				JWKSOptions:       sourceOptions,
				FallbackPublicKey: key.Public(),
				Log:               logger,
			})
			if err != nil {
				t.Fatal(err)
			}
			entity, err := authx.NewErrorAdapter(method).Authenticate(context.Background(), validTokenUnknownKID)
			if tc.allow {
				if err != nil || entity == nil {
					t.Fatalf("confirmed absence fallback rejected: %v", err)
				}
			} else {
				if err == nil || entity != nil {
					t.Fatalf("failed lookup bypassed through static key: entity=%v err=%v", entity, err)
				}
				if tc.failure != nil && !errors.Is(err, tc.failure) {
					t.Fatalf("dependency cause lost: %v", err)
				}
				if tc.limited && !errors.Is(err, authentication.ErrKeyNotFound) {
					t.Fatalf("legacy rate-limit category lost: %v", err)
				}
			}
			logger.mu.Lock()
			logs := strings.Join(logger.entries, "\n")
			logger.mu.Unlock()
			if strings.Contains(logs, "dependency-secret-marker") || strings.Contains(logs, "injected-log-marker") || strings.Contains(logs, validTokenUnknownKID) {
				t.Fatalf("fallback diagnostic exposed sensitive input: %s", logs)
			}
			if !tc.allow && !strings.Contains(logs, "rejected") {
				t.Fatalf("rejected fallback not diagnosed: %s", logs)
			}
		})
	}
}

func TestManagedJWTMethodDoesNotFallbackAfterCancellation(t *testing.T) {
	key, err := parseECDSAPublicKeyFromPrivateKey(ecdsaPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, cancelLifetime := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "lifetime"}[cancelLifetime], func(t *testing.T) {
			var requests atomic.Int32
			arrived := make(chan struct{})
			client := managedHTTPClient(func(req *http.Request) (*http.Response, error) {
				if requests.Add(1) == 1 {
					return managedJWKSResponse(req), nil
				}
				close(arrived)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})
			lifetime, stop := context.WithCancel(context.Background())
			defer stop()
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			method, err := authx.NewManagedJWTMethod(lifetime, authx.ManagedJWTMethodOptions{
				Endpoint:          "https://issuer.example/jwks",
				JWKSOptions:       authentication.JWKSOptions{Client: client, RefreshInterval: time.Hour, RequestOnUnknownKID: true},
				FallbackPublicKey: key.Public(),
			})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := authx.NewErrorAdapter(method).Authenticate(requestContext, validTokenUnknownKID)
				done <- err
			}()
			select {
			case <-arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("demand refresh did not start")
			}
			if cancelLifetime {
				stop()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled lookup bypassed through fallback: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled authentication did not finish")
			}
		})
	}
}

func TestManagedJWTMethodCacheAgePreventsStaticFallback(t *testing.T) {
	key, err := parseECDSAPublicKeyFromPrivateKey(ecdsaPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, maxAge := range []time.Duration{0, time.Nanosecond} {
		t.Run(maxAge.String(), func(t *testing.T) {
			lifetime, stop := context.WithCancel(context.Background())
			defer stop()
			logger := &diagnosticLogger{warnings: make(chan struct{}, 8)}
			method, err := authx.NewManagedJWTMethod(lifetime, authx.ManagedJWTMethodOptions{
				Endpoint: "https://issuer.example/jwks",
				JWKSOptions: authentication.JWKSOptions{
					Client:          managedHTTPClient(func(req *http.Request) (*http.Response, error) { return managedJWKSResponse(req), nil }),
					RefreshInterval: time.Hour,
					MaxCacheAge:     maxAge,
				},
				FallbackPublicKey: key.Public(),
				Log:               logger,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{validToken, validTokenUnknownKID} {
				entity, err := authx.NewErrorAdapter(method).Authenticate(context.Background(), token)
				if maxAge == 0 {
					if err != nil || entity == nil {
						t.Fatalf("unlimited age rejected usable key: %v", err)
					}
				} else if !errors.Is(err, authentication.ErrKeySetExpired) || entity != nil {
					t.Fatalf("expired snapshot bypassed through cache or static fallback: entity=%v err=%v", entity, err)
				}
			}
			if maxAge > 0 {
				logger.mu.Lock()
				logs := strings.Join(logger.entries, "\n")
				logger.mu.Unlock()
				if !strings.Contains(logs, "key_set_expired") || !strings.Contains(logs, "rejected") {
					t.Fatalf("expiration diagnostic missing: %s", logs)
				}
			}
		})
	}
}

type managedHTTPClient func(*http.Request) (*http.Response, error)

func (f managedHTTPClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

func managedJWKSResponse(req *http.Request) *http.Response {
	response := httptest.NewRecorder()
	jwksHandler(response, req)
	return response.Result()
}
