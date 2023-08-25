package auth_test

import (
	"errors"
	"net/http"
	"testing"

	. "github.com/velmie/x/svc/http/handler/auth"
)

func TestBearerTokenExtractor(t *testing.T) {
	extractor := NewBearerTokenExtractor()

	t.Run("missing Authorization header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		_, err := extractor.Extract(req)
		if !errors.Is(err, ErrMissingToken) {
			t.Fatalf("expected missing token error, got: %v", err)
		}
	})

	t.Run("Authorization header too short", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bear")
		_, err := extractor.Extract(req)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("expected invalid token error, got: %v", err)
		}
	})

	t.Run("Authorization header wrong schema", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "WrongSchema tokenValue")
		_, err := extractor.Extract(req)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("expected invalid token error, got: %v", err)
		}
	})

	t.Run("correct Authorization header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer tokenValue")
		token, err := extractor.Extract(req)
		if err != nil {
			t.Fatalf("did not expect an error, got: %v", err)
		}
		if token != "tokenValue" {
			t.Fatalf("expected tokenValue, got: %v", token)
		}
	})
}
