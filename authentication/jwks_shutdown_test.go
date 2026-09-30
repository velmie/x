package authentication

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJWKSShutdownWaitsForRequestAndBodyCleanup(t *testing.T) {
	for _, stage := range []string{"request", "close"} {
		t.Run(stage, func(t *testing.T) {
			entered, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
				Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
					if stage == "request" {
						close(entered)
						<-r.Context().Done()
						<-release
						close(cleaned)
						return nil, r.Context().Err()
					}
					return &http.Response{StatusCode: 200, Body: shutdownBody{
						Reader: strings.NewReader(concurrencyJWKS),
						close:  func() error { close(entered); <-release; close(cleaned); return nil },
					}}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer source.Stop()
			started := make(chan error, 1)
			go func() { started <- source.Start(context.Background()) }()
			awaitRefreshSignal(t, entered)
			deadline := &controlledDeadlineContext{done: make(chan struct{})}
			close(deadline.done)
			if err := source.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Shutdown reported completion with %s cleanup still blocked: %v", stage, err)
			}
			if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceStopped) {
				t.Fatalf("timed out shutdown admitted a lookup: %v", err)
			}
			releaseOnce.Do(func() { close(release) })
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := source.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cleaned:
			default:
				t.Fatal("Shutdown returned before cleanup")
			}
			if err := awaitRefreshResult(t, started); !errors.Is(err, ErrKeySourceStopped) && !errors.Is(err, context.Canceled) {
				t.Fatalf("concurrent startup succeeded after shutdown: %v", err)
			}
		})
	}
}

func TestJWKSShutdownWaitsForWarningAfterFlightCompletion(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var source *KeySourceJWKS
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		RefreshInterval: time.Hour,
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: shutdownBody{Reader: strings.NewReader(concurrencyJWKS), close: func() error { return errors.New("close failure") }}}, nil
		}),
		WarnFunc: func(string) { source.Stop(); close(entered); <-release; close(finished) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	// The callback can stop the source before Start finishes, so either outcome is valid.
	if err := source.Start(context.Background()); err != nil && !errors.Is(err, ErrKeySourceStopped) && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	awaitRefreshSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := source.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("did not wait for callback: %v", err)
	}
	const callers = 8
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			results <- source.Shutdown(ctx)
		}()
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < callers; i++ {
		if err := awaitRefreshResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-finished:
	default:
		t.Fatal("Shutdown returned before callback")
	}
}

func TestJWKSShutdownIsTerminalForLegacySource(t *testing.T) {
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{
		RefreshInterval: time.Hour, RequestOnUnknownKID: true,
		Client: concurrencyClient(func(*http.Request) (*http.Response, error) { calls.Add(1); return concurrencyResponse(), nil }),
	})
	source.Stop()
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "missing"); err != ErrKeyNotFound {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := source.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kid := range []string{"known", "missing"} {
		if _, err := source.FetchPublicKey(context.Background(), kid); !errors.Is(err, ErrKeySourceStopped) {
			t.Fatal(err)
		}
	}
	if err := source.Start(context.Background()); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected load after shutdown: %d", calls.Load())
	}
}

func TestJWKSShutdownBeforeStart(t *testing.T) {
	source, err := NewManagedKeySourceJWKS("https://example.test/keys")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Start(context.Background()); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
}

func TestJWKSShutdownWaitsForRateLimitCallback(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	opts := &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true,
		Client:   concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil }),
		WarnFunc: func(string) { close(entered); <-release },
	}
	opts.SetRefreshRateLimit(1, time.Hour)
	source := NewKeySourceJWKS("https://example.test/keys", opts)
	defer source.Stop()
	result := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "missing"); result <- err }()
	awaitRefreshSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := source.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("did not wait for rate callback: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := source.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := awaitRefreshResult(t, result); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
}

func TestJWKSShutdownConcurrentWithStartupCompletion(t *testing.T) {
	for i := 0; i < 50; i++ {
		entered, release := make(chan struct{}), make(chan struct{})
		source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
			close(entered)
			<-release
			return concurrencyResponse(), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan error, 1)
		go func() { started <- source.Start(context.Background()) }()
		awaitRefreshSignal(t, entered)
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = source.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if err := awaitRefreshResult(t, started); err != nil && !errors.Is(err, ErrKeySourceStopped) && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceStopped) {
			t.Fatal(err)
		}
	}
}

type shutdownBody struct {
	io.Reader
	close func() error
}

func (b shutdownBody) Close() error { return b.close() }
