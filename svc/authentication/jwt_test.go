package authentication_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/svc/authentication"
)

func TestJWTMethodOptionsValidation(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	tests := []struct {
		name    string
		options []authentication.JWTMethodOption
		wantErr bool
	}{
		{
			name:    "Default options. JWT public key is required.",
			options: []authentication.JWTMethodOption{},
			wantErr: true,
		},
		{
			name: "JWKS enabled.",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
			},
			wantErr: false,
		},
		{
			name: "JWKS disabled with JWT public key",
			options: []authentication.JWTMethodOption{
				authentication.WithJWTPublicKey(key.Public()),
			},
			wantErr: false,
		},

		{
			name: "JWKS with invalid rate limit",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimit(-1), // Неверное значение
			},
			wantErr: true,
		},
		{
			name: "JWKS with valid rate limit 0 - disabled",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimit(0),
			},
			wantErr: false,
		},
		{
			name: "JWKS with valid rate limit",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimit(5),
			},
			wantErr: false,
		},
		{
			name: "JWKS with invalid duration",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimitDuration(-1), // Неверное значение
			},
			wantErr: true,
		},
		{
			name: "JWKS with valid duration 0 - disabled",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimitDuration(0),
			},
			wantErr: false,
		},
		{
			name: "JWKS with valid duration",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSRequestRateLimitDuration(1 * time.Minute),
			},
			wantErr: false,
		},
		{
			name: "JWKS with invalid retries count",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSMaxRetries(0), // Неверное значение
			},
			wantErr: true,
		},
		{
			name: "JWKS with valid retries count",
			options: []authentication.JWTMethodOption{
				authentication.WithJWKSSource(&url.URL{Path: "/test"}),
				authentication.WithJWKSMaxRetries(5),
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := append(tc.options, authentication.WithLogger(&mockLogger{}))
			_, err := authentication.NewJWTMethod(opts...)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewJWTMethod() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

const (
	ecdsaPrivateKey = `
-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIHaLl+VdOw7OPuh0TPe3199Q8kDEJzMKhi3TeURFZAi+oAoGCCqGSM49
AwEHoUQDQgAEkFguL0cYfHECpRJV4YDnon60cBOyU+7jM6U7wYhFZDvv2YYB5lFe
Gd9oeBlhKIVCr6iCqB08/I1K+a4M4MvHBQ==
-----END EC PRIVATE KEY-----`
	validToken = "eyJhbGciOiJFUzI1NiIsImtpZCI6InRlc3QiLCJ0eXAiOiJKV1QifQ.eyJpc3MiOiJ2ZWxtaWUveC9zdmMvYXV0aGVudG" +
		"ljYXRpb24iLCJuZXN0ZWQiOnsia2V5IjoidmFsdWUifSwic29tZSI6ImNsYWltIn0.TELCzb7sTfwVaFv_i-lkXG9Np035X2y-eAHxF" +
		"oC3UjbuP2nP2-_xqOX4V0DomtafnksBRuuluxIYdL2JAx-6wg"
	validTokenUnknownKID = "eyJhbGciOiJFUzI1NiIsImtpZCI6InVua25vd24iLCJ0eXAiOiJKV1QifQ.eyJpc3MiOiJ2ZWxtaWUveC9zd" +
		"mMvYXV0aGVudGljYXRpb24iLCJuZXN0ZWQiOnsia2V5IjoidmFsdWUifSwic29tZSI6ImNsYWltIn0.M2shjyxW7jm7XY0BzE6A3mm4" +
		"bxMttXWEx3LNlY-2eU3AwUBOQ1bb1528PY0XOj__wEW186mnAEKP7fT9EppRwA"
	/*
		{
		  "iss": "velmie/x/svc/authentication",
		  "nested": {
			"key": "value"
		  },
		  "some": "claim"
		}
	*/
)

func TestSources(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(jwksHandler))
	defer ts.Close()

	endpoint, _ := url.Parse(ts.URL)

	m := createJWTMethod(t, true, authentication.WithJWKSSource(endpoint))

	t.Run("JWKS valid token", func(t *testing.T) {
		entity, err := m.Authenticate(context.Background(), validToken)
		if err != nil {
			t.Fatalf("m.Authenticate(...) unexpected error: %s", err)
		}
		if len(entity) != 3 || entity["iss"] != "velmie/x/svc/authentication" {
			t.Errorf("m.Authenticate(...), got invalid entity values: %v", entity)
		}
	})

	t.Run("JWKS unknown key id", func(t *testing.T) {
		_, err := m.Authenticate(context.Background(), validTokenUnknownKID)
		if err == nil {
			t.Fatalf("m.Authenticate(...) error is expected, got nil")
		}
		if !strings.Contains(err.Error(), "key is not found") {
			t.Errorf("m.Authenticate(...) unexpected error: %s", err)
		}
	})

	pk, _ := parseECDSAPublicKeyFromPrivateKey(ecdsaPrivateKey)

	logger := &mockLogger{}
	withFallback := createJWTMethod(
		t,
		true,
		authentication.WithJWKSSource(endpoint),
		authentication.WithJWTPublicKey(&pk.PublicKey),
		authentication.WithLogger(logger),
	)

	t.Run("unknown key id should fallback to the given key", func(t *testing.T) {
		_, err := withFallback.Authenticate(context.Background(), validTokenUnknownKID)
		if err != nil {
			t.Fatalf("m.Authenticate(...) unexpected error: %s", err)
		}
		if len(logger.WarningMsgs) == 0 {
			t.Errorf("expected warning message when falling back to the given key source")
		}
	})
}

func createJWTMethod(t *testing.T, withJWKS bool, options ...authentication.JWTMethodOption) authentication.Method {
	sourceReady := make(chan struct{})
	if withJWKS {
		options = append(options, authentication.WithJWKSSourceReadySignal(sourceReady))
	}
	m, err := authentication.NewJWTMethod(options...)
	if err != nil {
		t.Fatalf("authentication.NewJWTMethod(...) unexpected error: %s", err)
	}
	if withJWKS {
		select {
		case <-sourceReady:
		case <-time.After(3 * time.Second):
			t.Fatalf("JWKS source timeout error")
		}
	}
	return m
}

func jwksHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := []byte(`
{
  "keys": [
    {
      "use": "test",
      "kty": "EC",
      "kid": "test",
      "crv": "P-256",
      "alg": "ES256",
      "x": "kFguL0cYfHECpRJV4YDnon60cBOyU-7jM6U7wYhFZDs",
      "y": "79mGAeZRXhnfaHgZYSiFQq-ogqgdPPyNSvmuDODLxwU"
    }
  ]
}`)
	w.Write(response)
}

func parseECDSAPublicKeyFromPrivateKey(key string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(key))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the key")
	}
	priv, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ECDSA private key: %v", err)
	}
	return priv, nil
}

type mockLogger struct {
	InfoMsgs    []string
	WarningMsgs []string
	ErrorMsgs   []string
	DebugMsgs   []string
}

func (ml *mockLogger) Info(v ...any) {
	ml.InfoMsgs = append(ml.InfoMsgs, fmt.Sprint(v...))
}

func (ml *mockLogger) Warning(v ...any) {
	ml.WarningMsgs = append(ml.WarningMsgs, fmt.Sprint(v...))
}

func (ml *mockLogger) Error(v ...any) {
	ml.ErrorMsgs = append(ml.ErrorMsgs, fmt.Sprint(v...))
}

func (ml *mockLogger) Debug(v ...any) {
	ml.DebugMsgs = append(ml.DebugMsgs, fmt.Sprint(v...))
}
