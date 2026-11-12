// Package server provides cooperative HTTP serving and middleware for finite
// handlers. It composes native net/http types and leaves routing, configuration,
// dependency ownership, and process supervision to the application.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Serve serves on ln, drains on cancellation, and joins admitted handlers.
// It takes exclusive ownership of an unused srv and ln. The caller configures
// srv, including any TLS listener, timeouts, BaseContext, ConnContext, ErrorLog,
// and ConnState, and must not concurrently call Serve, Shutdown, or Close on srv.
// Serve chains the caller's ConnState hook.
// A nil srv.Handler uses http.DefaultServeMux.
//
// On cancellation, an independent drain deadline starts.
// Connections that have not delivered a request, including pending TLS
// handshakes, are closed immediately. Active requests and idle keep-alive
// connections retain the normal http.Server.Shutdown behavior. A nonpositive drain
// gives no grace period. Clean cancellation returns nil. A drain or listener
// failure closes active connections. Serve joins its serving goroutine and every
// admitted handler, even after the drain deadline. Handlers must return after
// request cancellation and join their own work before returning. Late dispatch
// cannot enter the application after the admission gate closes.
//
// Serve does not join detached workers, hijacked connections, or shutdown hooks,
// close application dependencies, or log. The caller owns those resources and
// logs any returned error. Failures are phase-wrapped and preserve their causes.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, drain time.Duration) error {
	var admission sync.Mutex
	var handlers sync.WaitGroup
	closed := false
	stopping := false
	newConnections := make(map[net.Conn]struct{})
	connState := srv.ConnState
	srv.ConnState = func(conn net.Conn, state http.ConnState) {
		admission.Lock()
		closeNew := state == http.StateNew && stopping
		if state == http.StateNew && !stopping {
			newConnections[conn] = struct{}{}
		} else {
			delete(newConnections, conn)
		}
		admission.Unlock()
		if closeNew {
			_ = conn.Close()
		}
		if connState != nil {
			connState(conn, state)
		}
	}
	next := srv.Handler
	if next == nil {
		next = http.DefaultServeMux
	}
	srv.Handler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			admission.Lock()
			if closed {
				admission.Unlock()
				return
			}
			handlers.Add(1)
			admission.Unlock()
			defer handlers.Done()
			next.ServeHTTP(w, r)
		},
	)
	defer func() {
		admission.Lock()
		closed = true
		admission.Unlock()
		handlers.Wait()
	}()

	listener := &ownedListener{
		Listener: ln,
	}
	result := make(chan error, 1)
	go func() { result <- srv.Serve(listener) }()

	var serveErr error
	var drainErr error
	var closeErr error
	select {
	case serveErr = <-result:
		// Under exclusive ownership, an unsolicited serving exit is a listener
		// failure even if its error equals http.ErrServerClosed.
		closeErr = srv.Close()
	case <-ctx.Done():
		drainCtx, cancel := context.WithTimeout(context.Background(), drain)
		admission.Lock()
		stopping = true
		pending := newConnections
		newConnections = nil
		admission.Unlock()
		for conn := range pending {
			_ = conn.Close()
		}
		drainErr = srv.Shutdown(drainCtx)
		cancel()
		if drainErr != nil {
			closeErr = srv.Close()
		}
		serveErr = <-result
		if serveErr == http.ErrServerClosed {
			serveErr = nil
		}
	}
	// net/http can discard the listener's Close error, including when Shutdown
	// returns a deadline error. Its serving goroutine has now joined that Close.
	if listener.err != nil && !errors.Is(drainErr, listener.err) && !errors.Is(closeErr, listener.err) {
		closeErr = errors.Join(closeErr, listener.err)
	}
	return errors.Join(
		phaseError("serve", serveErr),
		phaseError("drain", drainErr),
		phaseError("close", closeErr),
	)
}

type ownedListener struct {
	net.Listener
	err error
}

func (l *ownedListener) Close() error {
	l.err = l.Listener.Close()
	return l.err
}

func phaseError(phase string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("HTTP %s: %w", phase, err)
}
