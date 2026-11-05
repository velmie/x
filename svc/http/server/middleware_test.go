package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/velmie/x/svc/http/server"
)

func TestRecoverBeforeCommitAndHEAD(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(
			method,
			func(t *testing.T) {
				capture := &recordHandler{}
				payload := errors.New("private panic detail")
				handler := server.Recover(
					slog.New(capture),
					http.HandlerFunc(
						func(w http.ResponseWriter, _ *http.Request) {
							w.Header().Set("X-Untrusted", "discard")
							panic(payload)
						},
					),
					server.RecoveryOptions{},
				)
				writer := httptest.NewRecorder()
				req := httptest.NewRequest(method, "/?secret=private", nil)
				handler.ServeHTTP(writer, req)
				if writer.Code != 500 || writer.Header().Get("X-Untrusted") != "" {
					t.Fatalf("status = %d, headers = %#v", writer.Code, writer.Header())
				}
				if writer.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("content type = %q", writer.Header().Get("Content-Type"))
				}
				if method == http.MethodHead {
					if writer.Body.Len() != 0 {
						t.Fatalf("HEAD body = %q", writer.Body.String())
					}
				} else if body := writer.Body.String(); !strings.Contains(body, `"code":"INTERNAL_SERVER_ERROR"`) ||
					!strings.Contains(body, `"title":"Could not complete request."`) ||
					!strings.Contains(body, `"target":"common"`) || strings.Contains(body, payload.Error()) {
					t.Fatalf("public body = %q", body)
				}
				attrs := capture.only(t, slog.LevelError)
				if attrs["panic"].Any() != payload || attrs["reason"].String() != "handler_panic" ||
					attrs["operation"].String() != "handle_http_request" || attrs["outcome"].String() != "failed" {
					t.Fatalf("panic record = %#v", attrs)
				}
				if stack := attrs["stack"].String(); stack == "" || len(stack) > 64*1024 {
					t.Fatalf("stack length = %d", len(stack))
				}
			},
		)
	}
}

func TestRecoverCustomResponder(t *testing.T) {
	writeErr := errors.New("custom response write failed")
	for _, tc := range []struct {
		name        string
		handled     bool
		err         error
		wantStatus  int
		wantBody    string
		wantFailure error
	}{
		{
			name:       "handled",
			handled:    true,
			wantStatus: http.StatusTeapot,
			wantBody:   "custom",
		},
		{
			name:       "fallback",
			wantStatus: http.StatusInternalServerError,
			wantBody:   "INTERNAL_SERVER_ERROR",
		},
		{
			name:        "failed",
			handled:     true,
			err:         writeErr,
			wantStatus:  http.StatusOK,
			wantFailure: writeErr,
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				capture := &recordHandler{}
				opts := server.RecoveryOptions{
					Responder: func(w http.ResponseWriter, _ *http.Request) (bool, error) {
						if tc.handled && tc.err == nil {
							w.WriteHeader(http.StatusTeapot)
							_, _ = w.Write([]byte("custom"))
						}
						return tc.handled, tc.err
					},
				}
				handler := server.Recover(
					slog.New(capture),
					http.HandlerFunc(
						func(http.ResponseWriter, *http.Request) {
							panic("failure")
						},
					),
					opts,
				)
				writer := httptest.NewRecorder()
				handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
				if writer.Code != tc.wantStatus || !strings.Contains(writer.Body.String(), tc.wantBody) {
					t.Fatalf("response = %d %q", writer.Code, writer.Body.String())
				}
				attrs := capture.only(t, slog.LevelError)
				if tc.wantFailure != nil && attrs["response_error"].Any() != tc.wantFailure {
					t.Fatalf("response error = %v", attrs["response_error"])
				}
				if tc.name == "failed" && writer.Body.Len() != 0 {
					t.Fatalf("failed custom response was replaced: %q", writer.Body.String())
				}
			},
		)
	}
}

func TestRecoverLogsDefaultWriteFailure(t *testing.T) {
	failure := errors.New("response failed")
	capture := &recordHandler{}
	writer := &errorWriter{
		failure: failure,
	}
	handler := server.Recover(
		slog.New(capture),
		http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) {
				panic("failure")
			},
		),
		server.RecoveryOptions{},
	)
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if writer.status != http.StatusInternalServerError || capture.only(t, slog.LevelError)["response_error"].Any() != failure {
		t.Fatal("failed response cause was not recorded")
	}
}

func TestRecoverPreservesAbortHandler(t *testing.T) {
	capture := &recordHandler{}
	handler := server.Recover(
		slog.New(capture),
		http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) {
				panic(http.ErrAbortHandler)
			},
		),
		server.RecoveryOptions{},
	)
	if got := invokePanic(handler, httptest.NewRecorder()); got != http.ErrAbortHandler {
		t.Fatalf("panic = %v", got)
	}
	if len(capture.records) != 0 {
		t.Fatalf("abort was logged: %#v", capture.records)
	}
}

func TestRecoverAfterCommitAborts(t *testing.T) {
	for _, flush := range []bool{false, true} {
		capture := &recordHandler{}
		handler := server.Recover(
			slog.New(capture),
			http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					if flush {
						if err := http.NewResponseController(w).Flush(); err != nil {
							panic(err)
						}
					} else {
						w.WriteHeader(http.StatusCreated)
					}
					panic("after commit")
				},
			),
			server.RecoveryOptions{},
		)
		writer := httptest.NewRecorder()
		if got := invokePanic(handler, writer); got != http.ErrAbortHandler {
			t.Fatalf("flush %v: panic = %v", flush, got)
		}
		attrs := capture.only(t, slog.LevelError)
		if attrs["outcome"].String() != "aborted" {
			t.Fatalf("flush %v: attrs = %#v", flush, attrs)
		}
	}
}

func TestRecoverInformationalAndInvalidStatusOnTransport(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		capture := &recordHandler{}
		handler := server.Recover(
			slog.New(capture),
			http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					if invalid {
						w.WriteHeader(99)
					} else {
						w.WriteHeader(http.StatusEarlyHints)
						panic("after informational status")
					}
				},
			),
			server.RecoveryOptions{},
		)
		url, stop := startHTTP(t, handler)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		gotInfo := 0
		trace := &httptrace.ClientTrace{
			Got1xxResponse: func(code int, _ textproto.MIMEHeader) error {
				gotInfo = code
				return nil
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		stop()
		if err != nil || res.StatusCode != 500 || !strings.Contains(string(body), "INTERNAL_SERVER_ERROR") {
			t.Fatalf("invalid %v: response = %d %q, error %v", invalid, res.StatusCode, body, err)
		}
		if !invalid && gotInfo != http.StatusEarlyHints {
			t.Fatalf("informational status = %d", gotInfo)
		}
		capture.only(t, slog.LevelError)
	}
}

func TestRecoverAbortAfterFlushOnTransport(t *testing.T) {
	capture := &recordHandler{}
	handler := server.Recover(
		slog.New(capture),
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("partial"))
				if err := http.NewResponseController(w).Flush(); err != nil {
					panic(err)
				}
				panic("private detail")
			},
		),
		server.RecoveryOptions{},
	)
	url, stop := startHTTP(t, handler)
	defer stop()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || string(body) != "partial" || readErr == nil || strings.Contains(string(body), "private") {
		t.Fatalf("status %d, body %q, error %v", res.StatusCode, body, readErr)
	}
	capture.only(t, slog.LevelError)
}

func TestAccessLogOutcomesAndAttributes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		method     string
		status     int
		panicValue any
		wantMethod string
		wantStatus int
		outcome    string
	}{
		{
			name:       "empty",
			method:     http.MethodGet,
			wantMethod: http.MethodGet,
			wantStatus: 200,
			outcome:    "completed",
		},
		{
			name:       "status",
			method:     http.MethodPost,
			status:     204,
			wantMethod: http.MethodPost,
			wantStatus: 204,
			outcome:    "completed",
		},
		{
			name:       "abort",
			method:     "sensitive-method",
			panicValue: http.ErrAbortHandler,
			wantMethod: "OTHER",
			outcome:    "aborted",
		},
		{
			name:       "committed abort",
			method:     http.MethodGet,
			status:     503,
			panicValue: "oops",
			wantMethod: http.MethodGet,
			wantStatus: 503,
			outcome:    "aborted",
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				capture := &recordHandler{}
				opts := server.AccessLogOptions{
					Attributes: func(*http.Request) []slog.Attr {
						return []slog.Attr{slog.String("client_source", "proxy")}
					},
				}
				handler := server.AccessLog(
					slog.New(capture),
					http.HandlerFunc(
						func(w http.ResponseWriter, _ *http.Request) {
							if tc.status != 0 {
								w.WriteHeader(tc.status)
							}
							if tc.panicValue != nil {
								panic(tc.panicValue)
							}
						},
					),
					opts,
				)
				req := httptest.NewRequest(tc.method, "/?secret=private", nil)
				req = req.WithContext(server.WithOperation(req.Context(), "list_items"))
				got := invokeRequest(handler, httptest.NewRecorder(), req)
				if got != tc.panicValue {
					t.Fatalf("panic = %v", got)
				}
				attrs := capture.only(t, slog.LevelInfo)
				if attrs["operation"].String() != "list_items" || attrs["component"].String() != "http_server" ||
					attrs["stage"].String() != "handle" || attrs["method"].String() != tc.wantMethod ||
					attrs["status"].Int64() != int64(tc.wantStatus) || attrs["outcome"].String() != tc.outcome ||
					attrs["client_source"].String() != "proxy" || attrs["duration_ms"].Kind() != slog.KindInt64 {
					t.Fatalf("attributes = %#v", attrs)
				}
			},
		)
	}
}

func TestAccessLogObservesRecoveredPanic(t *testing.T) {
	panicLog := &recordHandler{}
	accessLog := &recordHandler{}
	endpoint := http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("failure") },
	)
	handler := server.AccessLog(
		slog.New(accessLog),
		server.Recover(slog.New(panicLog), endpoint, server.RecoveryOptions{}),
		server.AccessLogOptions{},
	)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	attrs := accessLog.only(t, slog.LevelInfo)
	if writer.Code != 500 || attrs["status"].Int64() != 500 || attrs["outcome"].String() != "completed" {
		t.Fatalf("status %d, access record %#v", writer.Code, attrs)
	}
	panicLog.only(t, slog.LevelError)
}

func TestAccessLogSerializedJSONAndText(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		var output bytes.Buffer
		var logHandler slog.Handler
		if format == "json" {
			logHandler = slog.NewJSONHandler(&output, nil)
		} else {
			logHandler = slog.NewTextHandler(&output, nil)
		}
		handler := server.AccessLog(
			slog.New(logHandler),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			server.AccessLogOptions{},
		)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PRIVATE_METHOD", "/secret?token=private", nil))
		line := output.String()
		if strings.Contains(line, "PRIVATE_METHOD") || strings.Contains(line, "/secret") || strings.Contains(line, "token") ||
			!strings.Contains(line, "OTHER") || !strings.Contains(line, "completed") {
			t.Fatalf("%s output = %q", format, line)
		}
		if format == "json" {
			var data map[string]any
			if err := json.Unmarshal(output.Bytes(), &data); err != nil || data["operation"] != "handle_http_request" {
				t.Fatalf("JSON = %#v, error = %v", data, err)
			}
		}
	}
}

func TestOperationContext(t *testing.T) {
	ctx := context.Background()
	if got := server.Operation(ctx); got != "" {
		t.Fatalf("operation = %q", got)
	}
	ctx = server.WithOperation(ctx, "create_order")
	if got := server.Operation(ctx); got != "create_order" {
		t.Fatalf("operation = %q", got)
	}
}

func invokePanic(handler http.Handler, w http.ResponseWriter) any {
	return invokeRequest(handler, w, httptest.NewRequest(http.MethodGet, "/", nil))
}

func invokeRequest(handler http.Handler, w http.ResponseWriter, r *http.Request) (value any) {
	defer func() { value = recover() }()
	handler.ServeHTTP(w, r)
	return nil
}

func startHTTP(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler: handler,
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(
			func() {
				_ = srv.Close()
				<-done
			},
		)
	}
	t.Cleanup(stop)
	return fmt.Sprintf("http://%s/", listener.Addr()), stop
}

type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordHandler) only(t *testing.T, level slog.Level) map[string]slog.Value {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) != 1 || h.records[0].Level != level {
		t.Fatalf("records = %#v", h.records)
	}
	attrs := make(map[string]slog.Value)
	h.records[0].Attrs(
		func(attr slog.Attr) bool {
			attrs[attr.Key] = attr.Value
			return true
		},
	)
	return attrs
}

type errorWriter struct {
	header  http.Header
	status  int
	failure error
}

func (w *errorWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *errorWriter) WriteHeader(status int)    { w.status = status }
func (w *errorWriter) Write([]byte) (int, error) { return 0, w.failure }
