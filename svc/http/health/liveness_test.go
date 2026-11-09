package health_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/velmie/x/svc/http/health"
)

func TestLiveness(t *testing.T) {
	h := health.NewLivenessHandler(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(method, "/", http.NoBody))
		if method == http.MethodPost {
			if r.Code != http.StatusMethodNotAllowed || r.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("POST: %d %v", r.Code, r.Header())
			}
		} else if r.Code != http.StatusOK || (method == http.MethodGet && !strings.Contains(r.Body.String(), `"status":"ok"`)) || (method == http.MethodHead && r.Body.Len() != 0) {
			t.Fatalf("%s: %d %s", method, r.Code, r.Body.String())
		}
	}
}
