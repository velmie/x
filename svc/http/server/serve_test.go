package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velmie/x/svc/http/server"
)

func TestServeGracefullyDrainsActiveRequest(t *testing.T) {
	listener := newTestListener(t, nil)
	entered := make(chan *http.Request, 1)
	release := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				entered <- r
				<-release
				_, _ = w.Write([]byte("done"))
			},
		),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, srv, listener, 5*time.Second)
	clientResult := startRequest(listener.Addr())
	req := receive(t, entered)
	cancel()
	receive(t, listener.closed)
	select {
	case <-req.Context().Done():
		t.Fatal("serving context canceled the active request before draining")
	default:
	}
	select {
	case err := <-done:
		t.Fatalf("Serve returned before handler completed: %v", err)
	default:
	}
	close(release)
	if err := receive(t, clientResult); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, done); err != nil {
		t.Fatalf("clean cancellation: %v", err)
	}
	if listener.closes.Load() != 1 {
		t.Fatalf("listener close calls = %d", listener.closes.Load())
	}
}

func TestServeDeadlineCancelsAndJoinsCleanup(t *testing.T) {
	for _, drain := range []time.Duration{0, 30 * time.Millisecond} {
		t.Run(
			drain.String(),
			func(t *testing.T) {
				listener := newTestListener(t, nil)
				entered := make(chan struct{})
				canceled := make(chan struct{})
				release := make(chan struct{})
				srv := &http.Server{
					Handler: http.HandlerFunc(
						func(_ http.ResponseWriter, r *http.Request) {
							close(entered)
							<-r.Context().Done()
							close(canceled)
							<-release
						},
					),
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := startServe(ctx, srv, listener, drain)
				clientResult := startRequest(listener.Addr())
				receive(t, entered)
				cancel()
				receive(t, canceled)
				select {
				case err := <-done:
					t.Fatalf("Serve returned while cleanup was still running: %v", err)
				default:
				}
				close(release)
				if err := receive(t, done); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Serve error = %v, want deadline exceeded", err)
				}
				_ = receive(t, clientResult)
				if listener.closes.Load() != 1 {
					t.Fatalf("listener close calls = %d", listener.closes.Load())
				}
			},
		)
	}
}

func TestServeAcceptFailureClosesConnectionsAndRetainsCauses(t *testing.T) {
	acceptCause := errors.New("accept failure")
	acceptErr := errors.Join(http.ErrServerClosed, acceptCause)
	closeErr := errors.New("listener close failure")
	listener := &failingListener{
		testListener: newTestListener(t, closeErr),
		fail:         make(chan struct{}),
		acceptErr:    acceptErr,
	}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				close(canceled)
				<-release
			},
		),
	}
	done := startServe(context.Background(), srv, listener, time.Second)
	clientResult := startRequest(listener.Addr())
	receive(t, entered)
	close(listener.fail)
	receive(t, canceled)
	select {
	case err := <-done:
		t.Fatalf("Serve returned while handler was active: %v", err)
	default:
	}
	close(release)
	err := receive(t, done)
	if !errors.Is(err, acceptCause) || !errors.Is(err, closeErr) {
		t.Fatalf("Serve error lost a cause: %v", err)
	}
	_ = receive(t, clientResult)
	if listener.closes.Load() != 1 {
		t.Fatalf("listener close calls = %d", listener.closes.Load())
	}
}

func TestServeExactServerClosedAcceptFailureCancelsActiveRequest(t *testing.T) {
	listener := &failingListener{
		testListener: newTestListener(t, nil),
		fail:         make(chan struct{}),
		acceptErr:    http.ErrServerClosed,
	}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				close(canceled)
				<-release
			},
		),
	}
	done := startServe(context.Background(), srv, listener, time.Second)
	clientResult := startRequest(listener.Addr())
	receive(t, entered)
	close(listener.fail)
	select {
	case <-canceled:
	case <-time.After(500 * time.Millisecond):
		_ = srv.Close()
		close(release)
		<-done
		t.Fatal("listener failure did not cancel the active request")
	}
	select {
	case err := <-done:
		t.Fatalf("Serve returned before cleanup completed: %v", err)
	default:
	}
	close(release)
	if err := receive(t, done); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("listener failure was lost: %v", err)
	}
	_ = receive(t, clientResult)
}

func TestServeCancellationBeforeStartup(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("close failed")} {
		listener := newTestListener(t, closeErr)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := receive(t, startServe(ctx, &http.Server{}, listener, time.Second))
		if closeErr == nil && err != nil || closeErr != nil && !errors.Is(err, closeErr) {
			t.Fatalf("Serve error = %v, close error = %v", err, closeErr)
		}
		if listener.closes.Load() != 1 {
			t.Fatalf("listener close calls = %d", listener.closes.Load())
		}
	}
}

func TestServeDrainAndListenerCloseFailuresAreJoined(t *testing.T) {
	closeErr := errors.New("listener close failed")
	listener := newTestListener(t, closeErr)
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				<-release
			},
		),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, srv, listener, 0)
	clientResult := startRequest(listener.Addr())
	receive(t, entered)
	cancel()
	receive(t, listener.closed)
	close(release)
	err := receive(t, done)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, closeErr) {
		t.Fatalf("Serve error lost a cause: %v", err)
	}
	_ = receive(t, clientResult)
}

func TestServeClosesAdmissionBeforeReturning(t *testing.T) {
	listener := newTestListener(t, nil)
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				select {
				case entered <- struct{}{}:
				default:
				}
				w.WriteHeader(http.StatusNoContent)
			},
		),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startServe(ctx, srv, listener, time.Second)
	if err := receive(t, startRequest(listener.Addr())); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	wrapped := srv.Handler
	var callers sync.WaitGroup
	for i := 0; i < 20; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for j := 0; j < 30; j++ {
				wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			}
		}()
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	callers.Wait()
	before := calls.Load()
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if calls.Load() != before {
		t.Fatal("late dispatch entered application code")
	}
}

func startServe(ctx context.Context, srv *http.Server, listener net.Listener, drain time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, srv, listener, drain) }()
	return done
}

func startRequest(addr net.Addr) <-chan error {
	result := make(chan error, 1)
	go func() {
		client := &http.Client{
			Timeout: 5 * time.Second,
		}
		res, err := client.Get("http://" + addr.String())
		if err == nil {
			_, err = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
		result <- err
	}()
	return result
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

type testListener struct {
	net.Listener
	closed   chan struct{}
	closeErr error
	once     sync.Once
	closes   atomic.Int32
}

func newTestListener(t *testing.T, closeErr error) *testListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return &testListener{
		Listener: listener,
		closed:   make(chan struct{}),
		closeErr: closeErr,
	}
}

func (l *testListener) Close() error {
	l.closes.Add(1)
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return errors.Join(err, l.closeErr)
}

type failingListener struct {
	*testListener
	accepts   atomic.Int32
	fail      chan struct{}
	acceptErr error
}

func (l *failingListener) Accept() (net.Conn, error) {
	if l.accepts.Add(1) == 1 {
		return l.Listener.Accept()
	}
	select {
	case <-l.fail:
		return nil, l.acceptErr
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
