package authentication

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestJWKSDiagnosticIdentifiesField(t *testing.T) {
	for _, tc := range []struct{ name, body, field string }{
		{"envelope", `{"keys":null}`, "keys"},
		{"kid", `{"keys":[{"use":"enc"},{"kid":7}]}`, "keys[1].kid"},
		{"use", `{"keys":[{"use":7}]}`, "keys[0].use"},
		{"algorithm", `{"keys":[{"alg":7}]}`, "keys[0].alg"},
		{"operations", `{"keys":[{"key_ops":7}]}`, "keys[0].key_ops"},
		{"operation element", `{"keys":[{"key_ops":["verify",7]}]}`, "keys[0].key_ops[1]"},
		{"private parameter", `{"keys":[{"d":"secret"}]}`, "keys[0].d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diagnostic *JWKSError
			source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
				VerificationPolicy: &JWKSVerificationPolicy{AllowedAlgorithms: []string{"ES256"}},
				Client:             concurrencyClient(func(*http.Request) (*http.Response, error) { return responseWithBody(tc.body), nil }),
				DiagnosticFunc:     func(event *JWKSError) { diagnostic = event },
			})
			if err != nil {
				t.Fatal(err)
			}
			err = source.Start(context.Background())
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			if err := source.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			var detail interface{ Field() string }
			if !errors.As(err, &detail) || detail.Field() != tc.field {
				t.Fatalf("missing diagnostic field %q: %v", tc.field, err)
			}
			if diagnostic == nil || !strings.Contains(diagnostic.Error(), "field="+tc.field) {
				t.Fatalf("callback lost field context: %v", diagnostic)
			}
			assertSafeJWKSError(t, diagnostic)
		})
	}
}

func TestJWKSConfigurationDiagnosticIdentifiesField(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, field string
		options               *JWKSOptions
	}{
		{name: "endpoint", endpoint: "https://example.test/%zz", field: "endpoint"},
		{name: "interval", endpoint: "https://example.test/keys", options: &JWKSOptions{RefreshInterval: -1}, field: "RefreshInterval"},
		{name: "algorithm", endpoint: "https://example.test/keys", options: &JWKSOptions{VerificationPolicy: &JWKSVerificationPolicy{AllowedAlgorithms: []string{"ES256", "unsupported"}}}, field: "VerificationPolicy.AllowedAlgorithms[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var options []*JWKSOptions
			if tc.options != nil {
				options = append(options, tc.options)
			}
			_, err := NewManagedKeySourceJWKS(tc.endpoint, options...)
			var diagnostic *JWKSError
			if !errors.As(err, &diagnostic) || diagnostic.Field() != tc.field {
				t.Fatalf("missing configuration field %q: %v", tc.field, err)
			}
		})
	}
}
