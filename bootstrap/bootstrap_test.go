package bootstrap_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/velmie/x/bootstrap"
)

const resultWaitTimeout = 3 * time.Second

func TestOrchestrator_NoServices(t *testing.T) {
	if err := bootstrap.NewOrchestrator().Serve(); !errors.Is(err, bootstrap.ErrNoRegisteredServices) {
		t.Fatalf("expected ErrNoRegisteredServices, got %v", err)
	}
}

func TestOrchestrator_StopBeforeServe(t *testing.T) {
	orc := bootstrap.NewOrchestrator()
	started := make(chan struct{})
	stops := 0
	orc.Register(bootstrap.ServiceFunc(func() error {
		close(started)
		return nil
	}, func(context.Context) error {
		<-started
		stops++
		return nil
	}))
	stopConcurrently(t, orc)
	if err := waitResult(t, serve(orc)); err != nil {
		t.Fatal(err)
	}
	if stops != 1 {
		t.Fatalf("Stop called %d times, want 1", stops)
	}
}

func TestOrchestrator_ConcurrentStopAndJoin(t *testing.T) {
	orc := bootstrap.NewOrchestrator()
	enteredStop := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		requested, done := make(chan struct{}), make(chan struct{})
		orc.Register(bootstrap.ServiceFunc(func() error {
			defer close(done)
			<-requested
			return errors.New("service finished after shutdown request")
		}, func(context.Context) error {
			close(requested)
			<-done // Service.Stop owns the join of its work.
			enteredStop <- struct{}{}
			<-release
			return nil
		}))
	}
	served := serve(orc)
	stopConcurrently(t, orc)
	waitSignal(t, enteredStop)
	waitSignal(t, enteredStop) // Both Stop methods run before either is released.
	select {
	case err := <-served:
		t.Fatalf("Serve returned before cleanup: %v", err)
	default:
	}
	stopConcurrently(t, orc) // Requests during slow cleanup must also return.
	releaseOnce.Do(func() { close(release) })
	if err := waitResult(t, served); err != nil {
		t.Fatal(err)
	}
	stopConcurrently(t, orc) // No receiver remains after Serve.
}

func TestOrchestrator_StartupAndShutdownErrors(t *testing.T) {
	for _, startup := range []bool{false, true} {
		startup := startup
		t.Run(fmt.Sprintf("startup_error=%t", startup), func(t *testing.T) {
			logger := &recordingLogger{redact: "fixture-secret"}
			orc := bootstrap.NewOrchestrator(bootstrap.WithLogger(logger))
			startCause := errors.New("startup dependency refused credential=fixture-secret")
			stopCauses := []*shutdownError{{"first cleanup failed credential=fixture-secret"}, {"second cleanup failed credential=fixture-secret"}}
			ready := make(chan struct{}, len(stopCauses))
			for i, cause := range stopCauses {
				i, cause := i, cause
				orc.Register(bootstrap.ServiceFunc(func() error {
					ready <- struct{}{}
					if startup && i == 1 {
						return startCause
					}
					return nil
				}, func(context.Context) error { return cause }))
			}
			served := serve(orc)
			for range stopCauses {
				waitSignal(t, ready)
			}
			if !startup {
				orc.Stop()
			}
			err := waitResult(t, served)
			if startup && !errors.Is(err, startCause) {
				t.Fatalf("startup cause lost: %v", err)
			}
			for _, cause := range stopCauses {
				if !errors.Is(err, cause) {
					t.Fatalf("shutdown cause lost: %v", err)
				}
			}
			var typed *shutdownError
			if !errors.As(err, &typed) || typed != stopCauses[0] {
				t.Fatalf("typed shutdown cause lost or reordered: %v", err)
			}
			logs := logger.String()
			if strings.Contains(logs, "fixture-secret") {
				t.Fatalf("raw cause leaked to logs: %s", logs)
			}
			if strings.Count(logs, "service_stop_failed") != 2 || (startup && strings.Count(logs, "service_start_failed") != 1) {
				t.Fatalf("missing or duplicate failure diagnostics: %s", logs)
			}
			for _, record := range logger.records {
				cause, ok := record["error"].(error)
				if !ok {
					t.Fatalf("diagnostic lost error object: %v", record)
				}
				index, ok := record["service_index"].(int)
				if !ok || index < 0 || index >= len(stopCauses) {
					t.Fatalf("diagnostic lost service: %v", record)
				}
				if record["stage"] == "startup" {
					if index != 1 || cause != startCause {
						t.Fatalf("startup source or cause changed: %v", record)
					}
				} else if cause != stopCauses[index] {
					t.Fatalf("shutdown source or cause changed: %v", record)
				}
			}
			for _, message := range []string{"first cleanup failed", "second cleanup failed"} {
				if !strings.Contains(logs, message) {
					t.Fatalf("redaction erased useful cause %q: %s", message, logs)
				}
			}
			if startup && !strings.Contains(logs, "startup dependency refused") {
				t.Fatalf("startup cause absent from serialized log: %s", logs)
			}
		})
	}
}

func TestOrchestrator_StartFailureDuringShutdownIsDiagnosed(t *testing.T) {
	logger := &recordingLogger{written: make(chan struct{}, 2)}
	orc := bootstrap.NewOrchestrator(bootstrap.WithLogger(logger))
	first, late := errors.New("listener failed"), errors.New("worker drain failed")
	release := make(chan struct{})
	orc.Register(bootstrap.ServiceFunc(func() error { return first }, func(context.Context) error { return nil }))
	orc.Register(bootstrap.ServiceFunc(func() error { <-release; return late }, func(context.Context) error { close(release); return nil }))
	if err := waitResult(t, serve(orc)); err != first {
		t.Fatalf("first startup error changed: %v", err)
	}
	waitSignal(t, logger.written)
	waitSignal(t, logger.written)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.records) != 2 {
		t.Fatalf("expected both startup failures exactly once: %v", logger.records)
	}
	if record := logger.records[1]; record["error"] != late || record["service_index"] != 1 || record["outcome"] != "shutdown_in_progress" {
		t.Fatalf("late failure lost its cause or service: %v", record)
	}
}

func TestOrchestrator_HTTPShutdownDiagnostic(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &listeningServer{Server: &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}, listener: listener}
	defer server.Close()
	logger := &recordingLogger{written: make(chan struct{}, 1)}
	orc := bootstrap.NewOrchestrator(bootstrap.WithLogger(logger))
	orc.Register(bootstrap.NewServerWrapper(server))
	served := serve(orc)
	defer orc.Stop()
	client := &http.Client{Timeout: resultWaitTimeout}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	orc.Stop()
	if err := waitResult(t, served); err != nil {
		t.Fatalf("normal HTTP shutdown failed: %v", err)
	}
	waitSignal(t, logger.written)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.records) != 0 || len(logger.infos) != 1 {
		t.Fatalf("normal HTTP close must be informational: errors=%v info=%v", logger.records, logger.infos)
	}
	if record := logger.infos[0]; record["error"] != http.ErrServerClosed || record["service_index"] != 0 || record["reason"] != "service_stopped" {
		t.Fatalf("HTTP close lost its cause, source or outcome: %v", record)
	}
}

func TestOrchestrator_StartupErrorIdentity(t *testing.T) {
	cause := errors.New("startup failed")
	orc := bootstrap.NewOrchestrator()
	orc.Register(bootstrap.ServiceFunc(func() error { return cause }, func(context.Context) error { return nil }))
	if err := waitResult(t, serve(orc)); err != cause {
		t.Fatalf("sole startup error changed: %v", err)
	}
}

func TestOrchestrator_ShutdownTimeout(t *testing.T) {
	orc := bootstrap.NewOrchestrator(bootstrap.WithShutdownTimeout(10 * time.Millisecond))
	contexts := make(chan context.Context, 2)
	for i := 0; i < 2; i++ {
		orc.Register(bootstrap.ServiceFunc(func() error { return nil }, func(ctx context.Context) error {
			contexts <- ctx
			<-ctx.Done()
			return ctx.Err()
		}))
	}
	orc.Stop()
	err := waitResult(t, serve(orc))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline cause lost: %v", err)
	}
	first, second := <-contexts, <-contexts
	if first == second {
		t.Fatal("services must have independent shutdown contexts")
	}
	for _, ctx := range []context.Context{first, second} {
		if _, ok := ctx.Deadline(); !ok || ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("service timeout missing: %v", ctx.Err())
		}
	}
}

func TestOrchestrator_FailedStopDoesNotJoinArbitraryStart(t *testing.T) {
	release, done := make(chan struct{}), make(chan struct{})
	defer func() { close(release); waitSignal(t, done) }()
	cause := errors.New("service could not stop its work")
	orc := bootstrap.NewOrchestrator()
	orc.Register(bootstrap.ServiceFunc(func() error {
		defer close(done)
		<-release
		return nil
	}, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); ok {
			return errors.New("unexpected default deadline")
		}
		return cause
	}))
	orc.Stop()
	if err := waitResult(t, serve(orc)); !errors.Is(err, cause) {
		t.Fatalf("expected failed Stop without waiting for Start, got %v", err)
	}
}

func TestOrchestrator_SignalHTTP(t *testing.T) {
	mode := os.Getenv("BOOTSTRAP_SIGNAL_TEST")
	if mode == "" {
		for _, mode := range []string{"default", "custom"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOrchestrator_SignalHTTP$")
				cmd.Env = append(os.Environ(), "BOOTSTRAP_SIGNAL_TEST="+mode)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("signal subprocess: %v\n%s", err, output)
				}
			})
		}
		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	srv := &listeningServer{Server: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}, listener: listener}
	defer srv.Close()
	orc := bootstrap.NewOrchestrator(bootstrap.WithShutdownTimeout(time.Second))
	sig := syscall.SIGTERM
	if mode == "custom" {
		sig = syscall.SIGUSR1
		orc = bootstrap.NewOrchestrator(bootstrap.WithStopSignals(sig), bootstrap.WithShutdownTimeout(time.Second))
	}
	orc.Register(bootstrap.NewServerWrapper(srv))
	served := serve(orc)
	client := &http.Client{Timeout: resultWaitTimeout}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTTP status %d", response.StatusCode)
	}
	if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
		t.Fatal(err)
	}
	if err := waitResult(t, served); err != nil {
		t.Fatal(err)
	}
	if response, err := client.Get("http://" + listener.Addr().String()); err == nil {
		response.Body.Close()
		t.Fatal("HTTP listener remains open after shutdown")
	}
}

func serve(orc *bootstrap.Orchestrator) <-chan error {
	result := make(chan error, 1)
	go func() { result <- orc.Serve() }()
	return result
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(resultWaitTimeout):
		t.Fatal("timed out waiting for Serve")
		return nil
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(resultWaitTimeout):
		t.Fatal("timed out waiting for service")
	}
}

func stopConcurrently(t *testing.T, orc *bootstrap.Orchestrator) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); orc.Stop(); orc.Stop() }()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	waitSignal(t, done)
}

type shutdownError struct{ message string }

func (e *shutdownError) Error() string { return e.message }

type listeningServer struct {
	*http.Server
	listener net.Listener
}

func (s *listeningServer) ListenAndServe() error { return s.Serve(s.listener) }

type recordingLogger struct {
	mu      sync.Mutex
	logs    strings.Builder
	records []map[string]any
	infos   []map[string]any
	redact  string
	written chan struct{}
}

func (l *recordingLogger) Info(msg string, args ...any) {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "error" {
			l.record(false, msg, args...)
			return
		}
	}
}

func (l *recordingLogger) Error(msg string, args ...any) { l.record(true, msg, args...) }

func (l *recordingLogger) record(failure bool, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fields := make(map[string]any)
	for i := 0; i+1 < len(args); i += 2 {
		fields[args[i].(string)] = args[i+1]
	}
	if failure {
		l.records = append(l.records, fields)
	} else {
		l.infos = append(l.infos, fields)
	}
	text := fmt.Sprint(msg, args)
	if l.redact != "" {
		text = strings.ReplaceAll(text, l.redact, "[redacted]")
	}
	fmt.Fprintln(&l.logs, strconv.Quote(text))
	if l.written != nil {
		l.written <- struct{}{}
	}
}
func (l *recordingLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.logs.String()
}
