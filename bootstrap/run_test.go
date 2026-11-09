package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRunFailureStopsPeersAndJoins(t *testing.T) {
	failure := errors.New("failed")
	cleanup := errors.New("cleanup failed")
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	cleanupStarted := make(chan struct{})
	finishCleanup := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			Component{Name: "first", Run: func(context.Context) error { started <- struct{}{}; <-release; return failure }},
			Component{Name: "second", Run: func(ctx context.Context) error {
				started <- struct{}{}
				<-ctx.Done()
				close(cleanupStarted)
				<-finishCleanup
				return errors.Join(ctx.Err(), cleanup)
			}},
		)
	}()
	<-started
	<-started
	close(release)
	<-cleanupStarted
	select {
	case err := <-done:
		t.Fatalf("returned before cleanup: %v", err)
	default:
	}
	close(finishCleanup)
	err := <-done
	if !errors.Is(err, failure) || !errors.Is(err, cleanup) || !errors.Is(err, context.Canceled) {
		t.Fatalf("lost causes: %v", err)
	}
	if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") || strings.Index(err.Error(), "first") > strings.Index(err.Error(), "second") {
		t.Fatalf("missing names or declaration order: %v", err)
	}
}

func TestRunUnexpectedReturns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result error
	}{
		{name: "nil"},
		{name: "canceled", result: context.Canceled},
		{name: "failure", result: errors.New("failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Run(context.Background(), Component{Name: "relay", Run: func(context.Context) error { return tc.result }})
			if err == nil || !strings.Contains(err.Error(), "relay") || (tc.result != nil && !errors.Is(err, tc.result)) {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestRunCanceledParentStartsAllAndPreservesShutdownFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cleanup := errors.New("cleanup")
	started := make(chan string, 3)
	err := Run(ctx,
		Component{Name: "normal", Run: func(ctx context.Context) error { started <- "normal"; return ctx.Err() }},
		Component{Name: "wrapped", Run: func(ctx context.Context) error { started <- "wrapped"; return fmt.Errorf("shutdown: %w", ctx.Err()) }},
		Component{Name: "joined", Run: func(ctx context.Context) error { started <- "joined"; return errors.Join(ctx.Err(), cleanup) }},
	)
	if len(started) != 3 || !errors.Is(err, cleanup) || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "normal") || !strings.Contains(err.Error(), "wrapped") || !strings.Contains(err.Error(), "joined") {
		t.Fatalf("unexpected result, started %d: %v", len(started), err)
	}
	if err := Run(ctx); err != nil {
		t.Fatalf("empty: %v", err)
	}
}

func TestRunSimultaneousFailures(t *testing.T) {
	a, b := errors.New("a cause"), errors.New("b cause")
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	component := func(err error) func(context.Context) error {
		return func(context.Context) error { ready <- struct{}{}; <-release; return err }
	}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Component{Name: "a", Run: component(a)}, Component{Name: "b", Run: component(b)})
	}()
	<-ready
	<-ready
	close(release)
	err := <-done
	if !errors.Is(err, a) || !errors.Is(err, b) {
		t.Fatalf("lost simultaneous failure: %v", err)
	}
}
