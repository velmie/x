package requestauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/http/requestauth"
	"github.com/velmie/x/svc/http/response"
	"github.com/velmie/x/svc/http/server"
	"github.com/velmie/x/svc/identity"
)

func TestBearerTokenFramingAndBounds(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		want   string
		err    error
	}{
		{
			name: "missing",
			err:  requestauth.ErrMissingToken,
		},
		{
			name:   "empty",
			header: http.Header{"Authorization": {""}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name:   "case insensitive scheme",
			header: http.Header{"Authorization": {"  bEaReR\ttoken  "}},
			want:   "token",
		},
		{
			name:   "duplicate values",
			header: http.Header{"Authorization": {"Bearer one", "Bearer two"}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name: "duplicate names with different case",
			header: http.Header{
				"Authorization": {"Bearer one"},
				"authorization": {"Bearer two"},
			},
			err: requestauth.ErrInvalidToken,
		},
		{
			name:   "combined credentials",
			header: http.Header{"Authorization": {"Bearer one,two"}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name:   "extra field",
			header: http.Header{"Authorization": {"Bearer one two"}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name:   "other scheme",
			header: http.Header{"Authorization": {"Basic token"}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name:   "maximum token and header",
			header: http.Header{"Authorization": {"Bearer " + strings.Repeat("a", requestauth.MaxBearerTokenBytes) + " "}},
			want:   strings.Repeat("a", requestauth.MaxBearerTokenBytes),
		},
		{
			name:   "token above limit",
			header: http.Header{"Authorization": {"Bearer " + strings.Repeat("a", requestauth.MaxBearerTokenBytes+1)}},
			err:    requestauth.ErrInvalidToken,
		},
		{
			name:   "header above limit",
			header: http.Header{"Authorization": {"Bearer a" + strings.Repeat(" ", requestauth.MaxAuthorizationBytes)}},
			err:    requestauth.ErrInvalidToken,
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				got, err := requestauth.BearerToken(tc.header)
				if got != tc.want || !errors.Is(err, tc.err) {
					t.Fatalf("token length=%d error=%v", len(got), err)
				}
				request := httptest.NewRequest(http.MethodGet, "/", nil)
				request.Header = tc.header
				extracted, extractErr := (requestauth.StrictBearerTokenExtractor{}).Extract(request)
				if extracted != got || !errors.Is(extractErr, tc.err) {
					t.Fatalf("extractor length=%d error=%v", len(extracted), extractErr)
				}
			},
		)
	}
}

func TestAuthenticatePreservesClassificationAndCause(t *testing.T) {
	cause := errors.New("verification dependency failed")
	verifier := testAuthenticator(
		func(context.Context, string) (identity.Principal, error) {
			return identity.Principal{}, fmt.Errorf("%w: %w", identity.ErrUnavailable, cause)
		},
	)
	principal, err := requestauth.Authenticate(context.Background(), nil, verifier)
	if principal.Valid() || !errors.Is(err, requestauth.ErrMissingToken) || !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("missing credentials: %+v %v", principal, err)
	}
	header := http.Header{"Authorization": {"Bearer token"}}
	principal, err = requestauth.Authenticate(context.Background(), header, verifier)
	if principal.Valid() || !errors.Is(err, identity.ErrUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("dependency cause: %+v %v", principal, err)
	}
}

func TestRequireResponseAndDiagnostics(t *testing.T) {
	dependency := errors.New("key provider timed out")
	unclassified := errors.New("unexpected verifier failure")
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctxDeadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	cases := []struct {
		name      string
		ctx       context.Context
		header    http.Header
		failure   error
		status    int
		code      string
		title     string
		challenge bool
		level     slog.Level
		reason    string
	}{
		{
			name:      "missing",
			status:    http.StatusUnauthorized,
			code:      response.ErrCodeUnauthorized,
			title:     "Valid bearer credentials are required.",
			challenge: true,
			level:     slog.LevelInfo,
			reason:    requestauth.ReasonMissingCredentials,
		},
		{
			name:      "malformed",
			header:    http.Header{"Authorization": {"Bearer bad,combined"}},
			status:    http.StatusUnauthorized,
			code:      response.ErrCodeUnauthorized,
			title:     "Valid bearer credentials are required.",
			challenge: true,
			level:     slog.LevelInfo,
			reason:    requestauth.ReasonMalformedCredentials,
		},
		{
			name:      "invalid",
			header:    http.Header{"Authorization": {"Bearer credential-canary"}},
			failure:   fmt.Errorf("%w: sub: expected nonblank text", identity.ErrInvalidCredentials),
			status:    http.StatusUnauthorized,
			code:      response.ErrCodeUnauthorized,
			title:     "Valid bearer credentials are required.",
			challenge: true,
			level:     slog.LevelInfo,
			reason:    requestauth.ReasonInvalidCredentials,
		},
		{
			name:    "dependency timeout with live request",
			header:  http.Header{"Authorization": {"Bearer credential-canary"}},
			failure: fmt.Errorf("%w: %w: %w", identity.ErrUnavailable, context.DeadlineExceeded, dependency),
			status:  http.StatusServiceUnavailable,
			code:    response.ErrCodeAuthUnavailable,
			title:   "Authentication is temporarily unavailable.",
			level:   slog.LevelError,
			reason:  requestauth.ReasonDependencyFailed,
		},
		{
			name:    "unexpected failure",
			header:  http.Header{"Authorization": {"Bearer credential-canary"}},
			failure: unclassified,
			status:  http.StatusInternalServerError,
			code:    response.ErrCodeInternalServerError,
			title:   "internal server error",
			level:   slog.LevelError,
			reason:  "internal_error",
		},
		{
			name:    "request canceled",
			ctx:     ctxCanceled,
			header:  http.Header{"Authorization": {"Bearer credential-canary"}},
			failure: fmt.Errorf("%w: %w", identity.ErrUnavailable, dependency),
			status:  499,
			code:    response.ErrCodeRequestCanceled,
			level:   slog.LevelInfo,
			reason:  "request_canceled",
		},
		{
			name:    "request deadline",
			ctx:     ctxDeadline,
			header:  http.Header{"Authorization": {"Bearer credential-canary"}},
			failure: context.DeadlineExceeded,
			status:  http.StatusGatewayTimeout,
			code:    response.ErrCodeDeadlineExceeded,
			level:   slog.LevelInfo,
			reason:  "deadline_exceeded",
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				var serialized bytes.Buffer
				records := &recordHandler{
					next: slog.NewJSONHandler(&serialized, nil),
				}
				logger := slog.New(records)
				verifier := testAuthenticator(
					func(context.Context, string) (identity.Principal, error) {
						return identity.Principal{}, tc.failure
					},
				)
				handler := requestauth.Require(
					verifier,
					logger,
					http.HandlerFunc(
						func(http.ResponseWriter, *http.Request) {
							t.Fatal("rejection reached handler")
						},
					),
				)
				request := httptest.NewRequest(http.MethodPost, "/", panicReader{})
				request.Header = tc.header
				ctx := tc.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				request = request.WithContext(server.WithOperation(ctx, "books.create"))
				writer := httptest.NewRecorder()
				writer.Header().Set("WWW-Authenticate", "Basic")
				handler.ServeHTTP(writer, request)
				if writer.Code != tc.status {
					t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
				}
				if got := writer.Header().Get("WWW-Authenticate"); (got == "Bearer") != tc.challenge || got != "" && got != "Bearer" {
					t.Fatalf("challenge=%q", got)
				}
				var envelope response.Errors
				if err := json.Unmarshal(writer.Body.Bytes(), &envelope); err != nil || len(envelope.Errors) != 1 {
					t.Fatalf("invalid error response: %s, %v", writer.Body.String(), err)
				}
				if envelope.Errors[0].Code != tc.code || tc.title != "" && envelope.Errors[0].Title != tc.title {
					t.Fatalf("body=%s", writer.Body.String())
				}
				record, attrs := records.only(t)
				if record.Level != tc.level || attrs["reason"].String() != tc.reason || attrs["operation"].String() != "books.create" ||
					attrs["stage"].String() != "authenticate" || attrs["outcome"].String() != "rejected" || attrs["component"].String() == "" {
					t.Fatalf("level=%v attrs=%v", record.Level, attrs)
				}
				if strings.Contains(serialized.String(), "credential-canary") || strings.Contains(writer.Body.String(), "credential-canary") {
					t.Fatal("credential appeared in diagnostics or response")
				}
				if tc.failure != nil && !errors.Is(attrs["error"].Any().(error), tc.failure) {
					t.Fatalf("diagnostic lost original cause: %v", attrs["error"])
				}
			},
		)
	}
}

func TestRequireSerializesUsefulSafeDiagnostics(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(
			format,
			func(t *testing.T) {
				var output bytes.Buffer
				var handler slog.Handler = slog.NewJSONHandler(&output, nil)
				if format == "text" {
					handler = slog.NewTextHandler(&output, nil)
				}
				verifier := testAuthenticator(
					func(context.Context, string) (identity.Principal, error) {
						return identity.Principal{}, fmt.Errorf("%w: permissions[3]: expected an unpadded string", identity.ErrInvalidCredentials)
					},
				)
				request := httptest.NewRequest(http.MethodGet, "/", nil)
				request.Header.Set("Authorization", "Bearer token-canary")
				handlerHTTP := requestauth.Require(verifier, slog.New(handler), http.NotFoundHandler())
				handlerHTTP.ServeHTTP(httptest.NewRecorder(), request)
				for _, expected := range []string{"operation", "authenticate", "permissions[3]", "expected an unpadded string"} {
					if !strings.Contains(output.String(), expected) {
						t.Fatalf("log lacks %q: %s", expected, output.String())
					}
				}
				if strings.Contains(output.String(), "token-canary") {
					t.Fatalf("log contains credential: %s", output.String())
				}
			},
		)
	}
}

func TestRequireHeadAndResponseWriteFailure(t *testing.T) {
	failure := errors.New("writer failed")
	records := &recordHandler{
		next: slog.NewTextHandler(io.Discard, nil),
	}
	verifier := testAuthenticator(
		func(context.Context, string) (identity.Principal, error) {
			return identity.Principal{}, identity.ErrInvalidCredentials
		},
	)
	handler := requestauth.Require(verifier, slog.New(records), http.NotFoundHandler())
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/", nil))
	if head.Code != http.StatusUnauthorized || head.Body.Len() != 0 || head.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("HEAD: status=%d body=%q headers=%v", head.Code, head.Body.String(), head.Header())
	}
	records.reset()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	writer := &failingResponseWriter{
		header: make(http.Header),
		err:    failure,
	}
	handler.ServeHTTP(writer, request)
	_, attrs := records.only(t)
	if !errors.Is(attrs["response_error"].Any().(error), failure) {
		t.Fatal("response failure was not retained in rejection record")
	}
}

func TestRequireAndPrincipalHandlerPassIdentity(t *testing.T) {
	want := identity.Principal{
		Issuer:      "issuer",
		Subject:     "subject",
		Permissions: []string{"books:read"},
	}
	verifier := testAuthenticator(
		func(_ context.Context, token string) (identity.Principal, error) {
			if token != "token" {
				t.Fatalf("wrong credential %q", token)
			}
			return want, nil
		},
	)
	handler := requestauth.Require(
		verifier,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		requestauth.PrincipalHandler(
			func(w http.ResponseWriter, r *http.Request, principal identity.Principal) {
				if principal.Subject != want.Subject || requestauth.Principal(r.Context()).Issuer != want.Issuer {
					t.Fatal("principal not propagated")
				}
				if !principal.Has("books:write") {
					w.WriteHeader(http.StatusForbidden)
				}
			},
		),
	)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != http.StatusForbidden {
		t.Fatalf("application authorization did not run: %d", writer.Code)
	}
}

func TestPrincipalPanicsWithoutGate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("missing gate did not panic")
		}
	}()
	requestauth.Principal(context.Background())
}

type testAuthenticator func(context.Context, string) (identity.Principal, error)

func (f testAuthenticator) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	return f(ctx, token)
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("request body was read before authentication") }

type failingResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) WriteHeader(int)           {}
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, w.err }

type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	next    slog.Handler
}

func (h *recordHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}
func (h *recordHandler) Handle(ctx context.Context, record slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, record.Clone())
	h.mu.Unlock()
	return h.next.Handle(ctx, record)
}
func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h.next.WithAttrs(attrs) }
func (h *recordHandler) WithGroup(name string) slog.Handler       { return h.next.WithGroup(name) }
func (h *recordHandler) reset() {
	h.mu.Lock()
	h.records = nil
	h.mu.Unlock()
}
func (h *recordHandler) only(t *testing.T) (slog.Record, map[string]slog.Value) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) != 1 {
		t.Fatalf("wanted one rejection log, got %d", len(h.records))
	}
	attrs := make(map[string]slog.Value)
	h.records[0].Attrs(
		func(attr slog.Attr) bool {
			attrs[attr.Key] = attr.Value
			return true
		},
	)
	return h.records[0], attrs
}
