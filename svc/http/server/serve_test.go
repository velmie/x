package server_test

import (
	"bufio"
	"context"
	"crypto/tls"
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

func TestServeClosesNewConnectionsPromptly(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		name := "TCP"
		if useTLS {
			name = "TLS handshake"
		}
		t.Run(
			name,
			func(t *testing.T) {
				listener := newTestListener(t, nil)
				var ln net.Listener = listener
				reading := make(chan struct{})
				if useTLS {
					ln = tls.NewListener(
						&readNotifyListener{
							Listener: listener,
							reading:  reading,
						},
						&tls.Config{
							MinVersion: tls.VersionTLS12,
						},
					)
				}
				states := make(chan http.ConnState, 4)
				var calls atomic.Int32
				srv := &http.Server{
					Handler: http.HandlerFunc(
						func(http.ResponseWriter, *http.Request) {
							calls.Add(1)
						},
					),
					ConnState: func(_ net.Conn, state http.ConnState) {
						states <- state
					},
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := startServe(ctx, srv, ln, 5*time.Second)
				conn, err := net.Dial("tcp", listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if state := receive(t, states); state != http.StateNew {
					t.Fatalf("first state = %v, want new", state)
				}
				if useTLS {
					// Wait until the TLS handshake is reading from the socket.
					receive(t, reading)
				}
				started := time.Now()
				cancel()
				if err := receive(t, done); err != nil {
					t.Fatalf("silent connection prevented clean cancellation: %v", err)
				}
				if elapsed := time.Since(started); elapsed >= time.Second {
					t.Fatalf("silent connection held shutdown for %v", elapsed)
				}
				if state := receive(t, states); state != http.StateClosed {
					t.Fatalf("final state = %v, want closed", state)
				}
				if calls.Load() != 0 {
					t.Fatal("silent connection entered the handler")
				}
			},
		)
	}
}

func TestServePreservesConnStateAndClosesIdleConnections(t *testing.T) {
	listener := newTestListener(t, nil)
	states := make(chan http.ConnState, 4)
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		),
		ConnState: func(_ net.Conn, state http.ConnState) {
			states <- state
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startServe(ctx, srv, listener, 5*time.Second)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	for _, want := range []http.ConnState{http.StateNew, http.StateActive, http.StateIdle} {
		if state := receive(t, states); state != want {
			t.Fatalf("state = %v, want %v", state, want)
		}
	}
	started := time.Now()
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("idle connection held shutdown for %v", elapsed)
	}
	if state := receive(t, states); state != http.StateClosed {
		t.Fatalf("final state = %v, want closed", state)
	}
}

func TestServeKeepAliveRequestWhileNewConnectionCloseBlocks(t *testing.T) {
	closing := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	listener := &closeControlledListener{
		Listener: newTestListener(t, nil),
		closing:  closing,
		release:  release,
	}
	newConnection := make(chan struct{}, 2)
	idle := make(chan struct{}, 2)
	var calls atomic.Int32
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, "served by application")
			},
		),
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				newConnection <- struct{}{}
			case http.StateIdle:
				idle <- struct{}{}
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startServe(ctx, srv, listener, 5*time.Second)
	silent, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	receive(t, newConnection)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	request := func() {
		t.Helper()
		if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		res, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK || string(body) != "served by application" {
			t.Fatalf("response = %d %q, want application response", res.StatusCode, body)
		}
	}
	request()
	receive(t, idle)
	cancel()
	receive(t, closing)
	// Shutdown has not started: closing the silent connection is still blocked.
	request()
	if calls.Load() != 2 {
		t.Fatalf("handler calls = %d, want 2", calls.Load())
	}
	unblock()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestServeIgnoresNewConnectionCloseError(t *testing.T) {
	release := make(chan struct{})
	close(release)
	listener := &closeControlledListener{
		Listener: newTestListener(t, nil),
		closing:  make(chan struct{}),
		release:  release,
		closeErr: errors.New("connection close failed"),
	}
	accepted := make(chan struct{})
	srv := &http.Server{
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				close(accepted)
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startServe(ctx, srv, listener, 5*time.Second)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	receive(t, accepted)
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatalf("unused connection close error prevented clean cancellation: %v", err)
	}
}

func TestServeFirstRequestDuringCancellation(t *testing.T) {
	listener := newTestListener(t, nil)
	active := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) {
				calls.Add(1)
			},
		),
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateActive {
				close(active)
				<-release
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startServe(ctx, srv, listener, 5*time.Second)
	clientResult := startRequest(listener.Addr())
	receive(t, active)
	cancel()
	receive(t, listener.closed)
	close(release)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	_ = receive(t, clientResult)
	if calls.Load() != 0 {
		t.Fatal("net/http dispatched the paused first request after shutdown started")
	}
}

func TestServeClosesConnectionAcceptedDuringCancellation(t *testing.T) {
	listener := newTestListener(t, nil)
	accepted := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			close(accepted)
			<-release
			return ctx
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startServe(ctx, srv, listener, 5*time.Second)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	receive(t, accepted)
	cancel()
	receive(t, listener.closed)
	started := time.Now()
	close(release)
	if err := receive(t, done); err != nil {
		t.Fatalf("late accepted connection prevented clean cancellation: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("late accepted connection held shutdown for %v", elapsed)
	}
}

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

// readNotifyListener observes the first socket read beneath a TLS listener.
type readNotifyListener struct {
	net.Listener
	reading chan struct{}
}

func (l *readNotifyListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &readNotifyConn{
		Conn:    conn,
		reading: l.reading,
	}, nil
}

type readNotifyConn struct {
	net.Conn
	reading chan struct{}
	once    sync.Once
}

func (c *readNotifyConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.reading) })
	return c.Conn.Read(p)
}

// closeControlledListener wraps only the first accepted connection.
type closeControlledListener struct {
	net.Listener
	closing  chan struct{}
	release  <-chan struct{}
	closeErr error
	wrapped  bool
}

func (l *closeControlledListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil || l.wrapped {
		return conn, err
	}
	l.wrapped = true
	return &closeControlledConn{
		Conn:     conn,
		closing:  l.closing,
		release:  l.release,
		closeErr: l.closeErr,
	}, nil
}

type closeControlledConn struct {
	net.Conn
	closing  chan struct{}
	release  <-chan struct{}
	closeErr error
	once     sync.Once
}

func (c *closeControlledConn) Close() error {
	c.once.Do(
		func() {
			close(c.closing)
			<-c.release
			c.closeErr = errors.Join(c.Conn.Close(), c.closeErr)
		},
	)
	return c.closeErr
}
