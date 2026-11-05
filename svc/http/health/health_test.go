package health_test

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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/http/health"
)

func TestCheckOrderedAndEmpty(t *testing.T) {
	var calls []string
	failure := errors.New("database offline")
	report := health.Check(
		context.Background(),
		health.Probe{
			Name: "database",
			Check: func(context.Context) error {
				calls = append(calls, "database")
				return failure
			},
		},
		health.Probe{
			Name: "broker",
			Check: func(context.Context) error {
				calls = append(calls, "broker")
				return nil
			},
		},
	)
	if report.Ready || len(report.Components) != 2 || !reflect.DeepEqual(calls, []string{"database", "broker"}) {
		t.Fatalf("report = %#v, calls = %#v", report, calls)
	}
	if report.Components[0].Name != "database" || report.Components[0].Reason != health.ReasonUnavailable ||
		report.Components[0].Cause != failure || report.Components[1].Name != "broker" || !report.Components[1].Ready {
		t.Fatalf("components = %#v", report.Components)
	}
	empty := health.Check(context.Background())
	body, err := json.Marshal(empty)
	if err != nil || !empty.Ready || string(body) != `{"ready":true,"components":[]}` {
		t.Fatalf("empty report = %s, error = %v", body, err)
	}
}

func TestCheckPerProbeTimeoutAndContinues(t *testing.T) {
	failure := errors.New("probe cleanup failed")
	later := false
	report := health.Check(
		context.Background(),
		health.Probe{
			Name:    "slow",
			Timeout: 20 * time.Millisecond,
			Check: func(ctx context.Context) error {
				<-ctx.Done()
				return failure
			},
		},
		health.Probe{
			Name: "later",
			Check: func(context.Context) error {
				later = true
				return nil
			},
		},
	)
	component := report.Components[0]
	if report.Ready || component.Ready || component.Reason != health.ReasonDeadlineExceeded ||
		!errors.Is(component.Cause, failure) || !errors.Is(component.Cause, context.DeadlineExceeded) || !later {
		t.Fatalf("component = %#v, later = %v", component, later)
	}
}

func TestCheckWaitsForCleanupAndSkipsAfterParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := make(chan struct{})
	cleanup := make(chan struct{})
	release := make(chan struct{})
	result := make(chan health.Report, 1)
	var later bool
	go func() {
		result <- health.Check(
			ctx,
			health.Probe{
				Name: "slow",
				Check: func(ctx context.Context) error {
					close(started)
					<-ctx.Done()
					close(cleanup)
					<-release
					return nil
				},
			},
			health.Probe{
				Name: "skipped",
				Check: func(context.Context) error {
					later = true
					return nil
				},
			},
		)
	}()
	receive(t, started)
	receive(t, cleanup)
	select {
	case report := <-result:
		t.Fatalf("Check returned while probe cleanup was running: %#v", report)
	default:
	}
	close(release)
	report := receive(t, result)
	if later || report.Ready || len(report.Components) != 2 {
		t.Fatalf("later = %v, report = %#v", later, report)
	}
	for _, component := range report.Components {
		if component.Ready || component.Reason != health.ReasonDeadlineExceeded ||
			!errors.Is(component.Cause, context.DeadlineExceeded) {
			t.Fatalf("component = %#v", component)
		}
	}
}

func TestCheckReasonsAndJoinedCauses(t *testing.T) {
	failure := errors.New("key set unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	report := health.Check(
		ctx,
		health.Probe{
			Name: "jwks",
			Check: func(context.Context) error {
				cancel()
				return errors.Join(failure, context.DeadlineExceeded)
			},
			Reason: func(error) string { return "jwks_stale" },
		},
		health.Probe{
			Name:   "skipped",
			Check:  func(context.Context) error { t.Fatal("probe must not start"); return nil },
			Reason: func(error) string { return "wrong" },
		},
	)
	first, skipped := report.Components[0], report.Components[1]
	if first.Reason != health.ReasonDeadlineExceeded || !errors.Is(first.Cause, failure) ||
		!errors.Is(first.Cause, context.DeadlineExceeded) || !errors.Is(first.Cause, context.Canceled) {
		t.Fatalf("first = %#v", first)
	}
	if skipped.Reason != health.ReasonCanceled || !errors.Is(skipped.Cause, context.Canceled) {
		t.Fatalf("skipped = %#v", skipped)
	}
	custom := health.Check(
		context.Background(),
		health.Probe{
			Name:  "jwks",
			Check: func(context.Context) error { return failure },
			Reason: func(err error) string {
				if errors.Is(err, failure) {
					return "jwks_stale"
				}
				return ""
			},
		},
	)
	if custom.Components[0].Reason != "jwks_stale" || custom.Components[0].Cause != failure {
		t.Fatalf("custom = %#v", custom)
	}
}

func TestCheckDefaultBudgetsAndTypedReason(t *testing.T) {
	failure := &probeFailure{
		kind:   "key set unavailable",
		secret: "private-token",
	}
	before := time.Now()
	var deadline time.Time
	report := health.Check(
		context.Background(),
		health.Probe{
			Name: "key_set",
			Check: func(ctx context.Context) error {
				deadline, _ = ctx.Deadline()
				return fmt.Errorf("check key set: %w", failure)
			},
			Reason: func(err error) string {
				var typed *probeFailure
				if errors.As(err, &typed) {
					return "key_set_unavailable"
				}
				return ""
			},
		},
	)
	after := time.Now()
	var typed *probeFailure
	if deadline.Before(before.Add(health.DefaultProbeTimeout)) || deadline.After(after.Add(health.DefaultProbeTimeout)) ||
		report.Components[0].Reason != "key_set_unavailable" || !errors.As(report.Components[0].Cause, &typed) || typed != failure {
		t.Fatalf("deadline = %v, component = %#v", deadline, report.Components[0])
	}
	before = time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := health.NewHandler(
		logger,
		health.HandlerOptions{},
		health.Probe{
			Name: "database",
			Check: func(ctx context.Context) error {
				deadline, _ = ctx.Deadline()
				return nil
			},
		},
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodHead, "/", nil))
	after = time.Now()
	if deadline.Before(before.Add(health.DefaultHandlerTimeout)) || deadline.After(after.Add(health.DefaultHandlerTimeout)) {
		t.Fatalf("handler default deadline = %v", deadline)
	}
}

func TestHandlerRedactsAtConfiguredLogger(t *testing.T) {
	failure := &probeFailure{
		kind:   "connection refused",
		secret: "private-token",
	}
	var output bytes.Buffer
	options := &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "error" {
				var typed *probeFailure
				if err, ok := attr.Value.Any().(error); ok && errors.As(err, &typed) {
					return slog.String("error", typed.kind)
				}
			}
			return attr
		},
	}
	logger := slog.New(slog.NewJSONHandler(&output, options))
	handler := health.NewHandler(
		logger,
		health.HandlerOptions{},
		health.Probe{
			Name:  "database",
			Check: func(context.Context) error { return failure },
		},
	)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(output.String(), failure.secret) || strings.Contains(writer.Body.String(), failure.secret) ||
		!strings.Contains(output.String(), failure.kind) || !strings.Contains(output.String(), "database") {
		t.Fatalf("log = %q, body = %q", output.String(), writer.Body.String())
	}
}

func TestHandlerSnapshotsProbes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	probes := []health.Probe{
		{
			Name:  "database",
			Check: func(context.Context) error { return nil },
		},
	}
	handler := health.NewHandler(logger, health.HandlerOptions{}, probes...)
	probes[0].Name = "changed"
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(writer.Body.String(), `"name":"database"`) {
		t.Fatalf("body = %q", writer.Body.String())
	}
}

func TestCheckPreservesCancellationCause(t *testing.T) {
	cancelCause := errors.New("dependency shutdown initiated")
	probeCause := errors.New("probe cleanup failed")
	ctx, cancel := context.WithCancelCause(context.Background())
	report := health.Check(
		ctx,
		health.Probe{
			Name: "active",
			Check: func(context.Context) error {
				cancel(cancelCause)
				return probeCause
			},
		},
		health.Probe{
			Name: "skipped",
			Check: func(context.Context) error {
				t.Fatal("probe started after cancellation")
				return nil
			},
		},
	)
	for _, component := range report.Components {
		if component.Reason != health.ReasonCanceled || !errors.Is(component.Cause, context.Canceled) ||
			!errors.Is(component.Cause, cancelCause) {
			t.Fatalf("component lost cancellation cause: %#v", component)
		}
	}
	if !errors.Is(report.Components[0].Cause, probeCause) {
		t.Fatalf("component lost probe cause: %#v", report.Components[0])
	}
}

func TestCheckLateSuccessIsUnavailable(t *testing.T) {
	report := health.Check(
		context.Background(),
		health.Probe{
			Name:    "late",
			Timeout: 10 * time.Millisecond,
			Check: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
		},
	)
	if report.Ready || report.Components[0].Reason != health.ReasonDeadlineExceeded ||
		!errors.Is(report.Components[0].Cause, context.DeadlineExceeded) {
		t.Fatalf("report = %#v", report)
	}
}

func TestHandlerWireContractAndDiagnostics(t *testing.T) {
	failure := errors.New("private dependency cause")
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(
			method,
			func(t *testing.T) {
				capture := &recordHandler{}
				calls := 0
				handler := health.NewHandler(
					slog.New(capture),
					health.HandlerOptions{},
					health.Probe{
						Name: "database",
						Check: func(context.Context) error {
							calls++
							return failure
						},
					},
				)
				writer := httptest.NewRecorder()
				handler.ServeHTTP(writer, httptest.NewRequest(method, "/readyz?secret=private", nil))
				if writer.Header().Get("Content-Type") != "application/json" || writer.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("headers = %#v", writer.Header())
				}
				if method == http.MethodPost {
					if writer.Code != 405 || writer.Header().Get("Allow") != "GET, HEAD" || calls != 0 ||
						!strings.Contains(writer.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) || len(capture.records) != 0 {
						t.Fatalf("method response = %d %q, calls %d, records %#v", writer.Code, writer.Body.String(), calls, capture.records)
					}
					return
				}
				if writer.Code != 503 || calls != 1 || strings.Contains(writer.Body.String(), failure.Error()) {
					t.Fatalf("response = %d %q, calls %d", writer.Code, writer.Body.String(), calls)
				}
				if method == http.MethodHead && writer.Body.Len() != 0 {
					t.Fatalf("HEAD body = %q", writer.Body.String())
				}
				if method == http.MethodGet && writer.Body.String() !=
					"{\"data\":{\"ready\":false,\"components\":[{\"name\":\"database\",\"ready\":false,\"reason\":\"dependency_unavailable\"}]}}\n" {
					t.Fatalf("body = %s", writer.Body.String())
				}
				attrs := capture.only(t, slog.LevelWarn)
				if attrs["operation"].String() != "readiness" || attrs["stage"].String() != "probe" ||
					attrs["component"].String() != "database" || attrs["reason"].String() != health.ReasonUnavailable ||
					attrs["outcome"].String() != "unavailable" || attrs["error"].Any() != failure {
					t.Fatalf("log = %#v", attrs)
				}
			},
		)
	}
}

func TestHandlerSuccessEmptyAndTotalBudget(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := httptest.NewRecorder()
	health.NewHandler(logger, health.HandlerOptions{}).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if writer.Code != 200 || writer.Body.String() != "{\"data\":{\"ready\":true,\"components\":[]}}\n" {
		t.Fatalf("empty response = %d %q", writer.Code, writer.Body.String())
	}
	started := make(chan struct{})
	cleanup := make(chan struct{})
	release := make(chan struct{})
	called := false
	handler := health.NewHandler(
		logger,
		health.HandlerOptions{
			Timeout: 20 * time.Millisecond,
		},
		health.Probe{
			Name: "slow",
			Check: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				close(cleanup)
				<-release
				return ctx.Err()
			},
		},
		health.Probe{
			Name:  "later",
			Check: func(context.Context) error { called = true; return nil },
		},
	)
	done := make(chan struct{})
	writer = httptest.NewRecorder()
	go func() {
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	receive(t, started)
	receive(t, cleanup)
	select {
	case <-done:
		t.Fatal("handler returned while probe cleanup was running")
	default:
	}
	close(release)
	receive(t, done)
	if writer.Code != 503 || called || strings.Count(writer.Body.String(), "deadline_exceeded") != 2 {
		t.Fatalf("status %d, called %v, body %s", writer.Code, called, writer.Body.String())
	}
}

func TestHandlerResponseWriteFailure(t *testing.T) {
	failure := errors.New("delivery failed")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		capture := &recordHandler{}
		writer := &errorWriter{
			failure: failure,
		}
		health.NewHandler(slog.New(capture), health.HandlerOptions{}).ServeHTTP(writer, httptest.NewRequest(method, "/", nil))
		attrs := capture.only(t, slog.LevelError)
		if attrs["operation"].String() != "readiness" || attrs["reason"].String() != "response_write_failed" ||
			attrs["error"].Any() != failure {
			t.Fatalf("method %s, diagnostics %#v", method, attrs)
		}
	}
}

func TestHandlerDoesNotCatchProbePanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := health.NewHandler(
		logger,
		health.HandlerOptions{},
		health.Probe{
			Name:  "dependency",
			Check: func(context.Context) error { panic("probe panic") },
		},
	)
	defer func() {
		if recovered := recover(); recovered != "probe panic" {
			t.Fatalf("panic = %v", recovered)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a test event")
		var zero T
		return zero
	}
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

type probeFailure struct {
	kind   string
	secret string
}

func (e *probeFailure) Error() string { return e.kind + ": " + e.secret }

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
