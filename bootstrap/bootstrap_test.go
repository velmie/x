package bootstrap_test

import (
	"context"
	"errors"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/velmie/x/bootstrap"
)

const resultWaitTimeout = 3 * time.Second

var ErrFailedStartup = errors.New("startup failed")

func NewHTTPService(addr string) *bootstrap.ServerWrapper {
	mux := http.NewServeMux()
	mux.Handle("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	s := &http.Server{
		Handler: mux,
		Addr:    addr,
	}
	return bootstrap.NewServerWrapper(s)
}

func TestOrchestrator_NoServices(t *testing.T) {
	orc := bootstrap.NewOrchestrator()
	if err := orc.Serve(); err == nil || !errors.Is(err, bootstrap.ErrNoRegisteredServices) {
		t.Fatalf("expected errors to be returned, but got")
	}
}

func TestOrchestrator_ServeHTTP(t *testing.T) {
	svc := NewHTTPService(":8080")

	orc := bootstrap.NewOrchestrator(
		bootstrap.WithStopSignals(syscall.SIGINT),
		bootstrap.WithLogger(bootstrap.NewNoopLogger()),
		bootstrap.WithShutdownTimeout(time.Second),
	)
	orc.Register(svc)

	finishCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	defer close(errCh) // close so goroutine is stopped

	go func() {
		if err := orc.Serve(); err != nil {
			errCh <- err
		}
		finishCh <- struct{}{}
	}()

	const url = "http://localhost:8080"
	if err := sendReqToHttpEndpoint(url); err != nil {
		t.Fatalf("http service is already started but request wasn't successful: %v", err)
	}

	if err := sendSignal(syscall.SIGINT); err != nil {
		t.Fatalf("failed to send interrupt signal to current process: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), resultWaitTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("failed to shutdown orchestrator within timeout")
	case err := <-errCh:
		t.Fatalf("unexpected error is raised from serve function: %v", err)
	case <-finishCh:
	}

	// check again and error must be 404
	if err := sendReqToHttpEndpoint(url); err == nil {
		t.Fatalf("http service is down already, so no response must be retrieved: %v", err)
	}
}

func TestOrchestrator_ServeStartupError(t *testing.T) {
	startFn := func() error {
		return ErrFailedStartup
	}

	stopFn := func(ctx context.Context) error {
		return nil
	}

	orc := bootstrap.NewOrchestrator(bootstrap.WithLogger(bootstrap.NewNoopLogger()))
	orc.Register(bootstrap.ServiceFunc(startFn, stopFn))

	errCh := make(chan error)
	go func() {
		if err := orc.Serve(); err != nil {
			errCh <- err
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), resultWaitTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("waiting for error for too long")
	case err := <-errCh:
		if !errors.Is(err, ErrFailedStartup) {
			t.Fatalf("expected to get error '%v' but got '%v'", ErrFailedStartup, err)
		}
	}
}

func TestOrchestrator_ServeMultipleStartupErrors(t *testing.T) {
	failedStartFn := func() error {
		return ErrFailedStartup
	}

	failedStopFn := func(ctx context.Context) error {
		return nil
	}

	stopCh := make(chan struct{}, 1)
	defer close(stopCh)
	successStartFn := func() error {
		<-stopCh
		// we raise error, but it must not be matter since shutdown is triggered already
		return errors.New("connection is closed")
	}

	successStopFn := func(ctx context.Context) error {
		return errors.New("error on stop function")
	}

	orc := bootstrap.NewOrchestrator(bootstrap.WithLogger(bootstrap.NewNoopLogger()))
	orc.Register(bootstrap.ServiceFunc(failedStartFn, failedStopFn))
	orc.Register(bootstrap.ServiceFunc(successStartFn, successStopFn))
	orc.Register(bootstrap.ServiceFunc(failedStartFn, failedStopFn))

	errCh := make(chan error)
	go func() {
		if err := orc.Serve(); err != nil {
			errCh <- err
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), resultWaitTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("waiting for error for too long")
	case err := <-errCh:
		if !errors.Is(err, ErrFailedStartup) {
			t.Fatalf("expected to get error '%v' but got '%v'", ErrFailedStartup, err)
		}
	}
}

func TestOrchestrator_Stop(t *testing.T) {
	svc := NewHTTPService(":8080")

	orc := bootstrap.NewOrchestrator(
		bootstrap.WithStopSignals(syscall.SIGINT),
		bootstrap.WithLogger(bootstrap.NewNoopLogger()),
		bootstrap.WithShutdownTimeout(time.Second),
	)
	orc.Register(svc)

	finishCh := make(chan struct{}, 1)
	errCh := make(chan error)

	go func() {
		if err := orc.Serve(); err != nil {
			errCh <- err
		}
		finishCh <- struct{}{}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), resultWaitTimeout)
	defer cancel()

	orc.Stop()

	select {
	case <-ctx.Done():
		t.Fatal("failed to shutdown orchestrator within timeout")
	case err := <-errCh:
		t.Fatalf("unexpected error is raised from serve function: %v", err)
	case <-finishCh:
	}
}

// sendReqToHttpEndpoint tries to send HTTP request with some backoff, since server might try to start for too long
func sendReqToHttpEndpoint(url string) (err error) {
	for i := 0; i < 5; i++ {
		_, err = http.DefaultClient.Get(url)
		if err == nil {
			return nil
		}
		<-time.After(200 * time.Millisecond)
	}
	return err
}

func sendSignal(sig syscall.Signal) error {
	return syscall.Kill(syscall.Getpid(), sig)
}
