package authentication

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedJWKSExplicitStartAndTerminalStop(t *testing.T) {
	var calls atomic.Int32
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { calls.Add(1); return concurrencyResponse(), nil })})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("constructor performed I/O")
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceNotStarted) {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := source.Start(lifetime); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	ignored, cancelIgnored := context.WithCancel(context.Background())
	if err := source.Start(ignored); err != nil {
		t.Fatal(err)
	}
	cancelIgnored()
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatalf("second Start replaced lifetime: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("repeated Start reloaded")
	}
	source.Stop()
	source.Stop()
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
	if err := source.Start(context.Background()); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
}

func TestManagedJWKSInitialFailureCanRetry(t *testing.T) {
	dependency := errors.New("dependency failure")
	var calls atomic.Int32
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, dependency
		}
		return concurrencyResponse(), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	if err := source.Start(context.Background()); !errors.Is(err, dependency) {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceNotStarted) {
		t.Fatal(err)
	}
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedJWKSConcurrentStartAndCancellation(t *testing.T) {
	entered := make(chan struct{})
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- source.Start(ctx) }()
	awaitRefreshSignal(t, entered)
	if err := source.Start(context.Background()); !errors.Is(err, ErrKeySourceStarting) {
		t.Fatal(err)
	}
	cancel()
	if err := awaitRefreshResult(t, started); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestManagedJWKSLifetimeCancellationCancelsDemandRefresh(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) > 1 {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		}
		return concurrencyResponse(), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "missing"); result <- err }()
	awaitRefreshSignal(t, entered)
	cancel()
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lifetime allowed cached read: %v", err)
	}
	awaitRefreshSignal(t, canceled)
	if err := awaitRefreshResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestManagedJWKSConfigurationValidation(t *testing.T) {
	cases := []JWKSOptions{{RefreshInterval: -1}, {MaxResponseBytes: -1}, {limit: -1, duration: time.Second}, {limit: 1}, {duration: time.Second}}
	for _, options := range cases {
		if _, err := NewManagedKeySourceJWKS("https://example.test/keys", &options); err == nil {
			t.Fatalf("accepted invalid options %+v", options)
		}
	}
	for _, endpoint := range []string{"", ":bad", "ftp://example.test/keys", "https:///keys"} {
		if _, err := NewManagedKeySourceJWKS(endpoint); err == nil {
			t.Fatalf("accepted invalid endpoint %q", endpoint)
		}
	}
}

func TestLegacyJWKSNegativeIntervalUsesDefault(t *testing.T) {
	warning := make(chan string, 1)
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: -1, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil }), WarnFunc: func(message string) { warning <- message }})
	defer source.Stop()
	if source.refreshInterval != time.Minute {
		t.Fatalf("interval %s", source.refreshInterval)
	}
	select {
	case message := <-warning:
		if message != "operation=jwks_config stage=validate reason=negative_refresh_interval outcome=defaulted" {
			t.Fatal(message)
		}
	default:
		t.Fatal("missing warning")
	}
}

func TestManagedJWKSStopBeforeStartIsTerminal(t *testing.T) {
	source, err := NewManagedKeySourceJWKS("https://example.test/keys")
	if err != nil {
		t.Fatal(err)
	}
	source.Stop()
	if err := source.Start(context.Background()); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); !errors.Is(err, ErrKeySourceStopped) {
		t.Fatal(err)
	}
}

func TestLegacyJWKSStartKeepsEagerLifetime(t *testing.T) {
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { calls.Add(1); return concurrencyResponse(), nil })})
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	source.Stop()
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("Start reloaded legacy source")
	}
}

func ExampleNewManagedKeySourceJWKS() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := NewManagedKeySourceJWKS("https://example.com/.well-known/jwks.json", &JWKSOptions{Client: &http.Client{Timeout: 5 * time.Second}, RefreshInterval: time.Minute})
	if err != nil {
		return
	}
	defer source.Stop()
	if err := source.Start(ctx); err != nil {
		return
	}
	// Pass source to NewViaJWT. Cancel ctx or call Stop during shutdown.
}

func TestManagedJWKSDemandFailureRetainsLifetimeDeadline(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) > 1 {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return concurrencyResponse(), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	lifetime := &controlledDeadlineContext{done: make(chan struct{})}
	if err := source.Start(lifetime); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "missing"); result <- err }()
	awaitRefreshSignal(t, entered)
	close(lifetime.done)
	err = awaitRefreshResult(t, result)
	if !errors.Is(err, ErrKeySourceStopped) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, context.Canceled) {
		t.Fatalf("lost lifecycle or request cause: %v", err)
	}
}

type controlledDeadlineContext struct{ done chan struct{} }

func (c *controlledDeadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *controlledDeadlineContext) Done() <-chan struct{}       { return c.done }
func (c *controlledDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
func (c *controlledDeadlineContext) Value(any) any { return nil }
