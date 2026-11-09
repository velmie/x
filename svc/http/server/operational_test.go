package server_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/velmie/x/svc/http/server"
)

func TestOperationalRoutes(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	server.RegisterOperationalRoutes(mux, slog.New(slog.NewJSONHandler(io.Discard, nil)), server.OperationalRoutes{
		Readiness: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusOK) }),
		Build:     map[string]string{"version": "test"},
		Metrics:   http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; _, _ = w.Write([]byte("metrics")) }),
	})
	for _, path := range []string{"/readyz", "/build", "/metrics"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			t.Run(method+path, func(t *testing.T) {
				before := calls
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, httptest.NewRequest(method, path, http.NoBody))
				want := http.StatusOK
				if method == http.MethodPost {
					want = http.StatusMethodNotAllowed
				}
				if recorder.Code != want {
					t.Fatalf("status=%d want %d", recorder.Code, want)
				}
				if method == http.MethodPost && (recorder.Header().Get("Allow") != "GET, HEAD" || calls != before) {
					t.Fatal("method rejection ran handler or omitted Allow")
				}
				if path == "/build" && recorder.Header().Get("Cache-Control") != "no-store" && method != http.MethodPost {
					t.Fatal("missing no-store")
				}
				if method == http.MethodHead && path == "/build" && recorder.Body.Len() != 0 {
					t.Fatal("HEAD included build body")
				}
			})
		}
	}
	if calls != 4 {
		t.Fatalf("handler calls=%d", calls)
	}
}

func TestOperationalMetricsOptionalAndBuildEncodingFailure(t *testing.T) {
	var logs bytes.Buffer
	mux := http.NewServeMux()
	server.RegisterOperationalRoutes(mux, slog.New(slog.NewJSONHandler(&logs, nil)), server.OperationalRoutes{
		Readiness: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Build:     make(chan int),
	})
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if r.Code != http.StatusNotFound {
		t.Fatalf("metrics status %d", r.Code)
	}
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/build", http.NoBody))
	if !strings.Contains(logs.String(), "unsupported type") || !strings.Contains(logs.String(), "build") {
		t.Fatalf("missing encoding cause: %s", logs.String())
	}
	cause := errors.New("write failed")
	mux2 := http.NewServeMux()
	server.RegisterOperationalRoutes(mux2, slog.New(slog.NewJSONHandler(&logs, nil)), server.OperationalRoutes{
		Readiness: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Build:     "test",
	})
	mux2.ServeHTTP(&failingOperationalWriter{header: make(http.Header), cause: cause}, httptest.NewRequest(http.MethodGet, "/build", http.NoBody))
	if !strings.Contains(logs.String(), cause.Error()) {
		t.Fatalf("missing write cause: %s", logs.String())
	}
}

type failingOperationalWriter struct {
	header http.Header
	cause  error
}

func (w *failingOperationalWriter) Header() http.Header       { return w.header }
func (w *failingOperationalWriter) WriteHeader(int)           {}
func (w *failingOperationalWriter) Write([]byte) (int, error) { return 0, w.cause }
