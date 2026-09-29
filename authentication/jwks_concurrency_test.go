package authentication

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const concurrencyJWKS = `{"keys":[{"kty":"oct","k":"c2VjcmV0","kid":"known"}]}`

func TestJWKSRefreshDoesNotBlockCachedReadOrCanceledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) > 1 {
			close(started)
			<-release
		}
		return concurrencyResponse(), nil
	})})
	defer source.Stop()
	defer close(release)
	refresh := make(chan error, 1)
	go func() { refresh <- source.requestKeys(context.Background()) }()
	awaitRefreshSignal(t, started)
	hit := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "known"); hit <- err }()
	if err := awaitRefreshResult(t, hit); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- source.requestKeys(ctx) }()
	cancel()
	if err := awaitRefreshResult(t, waiting); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestJWKSStopCancelsRefreshAndPreservesLegacyAccess(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Millisecond, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 2 {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		}
		return concurrencyResponse(), nil
	})})
	defer source.Stop()
	awaitRefreshSignal(t, started)
	source.refreshMu.Lock()
	flightDone := source.flight.done
	source.refreshMu.Unlock()
	stopped := make(chan error, 1)
	go func() { source.Stop(); source.Stop(); stopped <- nil }()
	awaitRefreshResult(t, stopped)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel HTTP")
	}
	select {
	case <-flightDone:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled refresh did not complete")
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("post-Stop miss did not refresh: %d", calls.Load())
	}
}

func TestJWKSConcurrentMissSharesRefreshDespiteCanceledInitiator(t *testing.T) {
	started, release, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return concurrencyResponse(), nil
		}
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			close(canceled)
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(concurrencyJWKS, "known", "rotated")))}, nil
	})})
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initiator := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(ctx, "rotated"); initiator <- err }()
	awaitRefreshSignal(t, started)
	const waiters = 16
	results := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() { _, err := source.FetchPublicKey(context.Background(), "rotated"); results <- err }()
	}
	awaitRefreshWaiters(t, source, waiters+1)
	cancel()
	if err := awaitRefreshResult(t, initiator); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-canceled:
		t.Fatal("one canceled waiter aborted shared HTTP")
	default:
	}
	close(release)
	for i := 0; i < waiters; i++ {
		if err := awaitRefreshResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("wanted one shared refresh, got %d calls including initialization", calls.Load())
	}
}

func TestJWKSAbandonedRefreshCancelsHTTP(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) > 1 {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		}
		return concurrencyResponse(), nil
	})})
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(ctx, "missing"); result <- err }()
	awaitRefreshSignal(t, started)
	cancel()
	if err := awaitRefreshResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("abandoned refresh leaked HTTP operation")
	}
}

func TestJWKSWarningCanReenterSource(t *testing.T) {
	var calls atomic.Int32
	callback := make(chan error, 1)
	var source *KeySourceJWKS
	source = NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		response := concurrencyResponse()
		if calls.Add(1) == 2 {
			response.Body = closeErrorBody{strings.NewReader(concurrencyJWKS)}
		}
		return response, nil
	}), WarnFunc: func(string) {
		// A miss must be able to complete another refresh from inside the callback.
		_, err := source.FetchPublicKey(context.Background(), "still-missing")
		source.Stop()
		callback <- err
	}})
	defer source.Stop()
	if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	if err := awaitRefreshResult(t, callback); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("reentrant callback did not refresh: %d", calls.Load())
	}
}

func TestRateLimiterReservesSharedBudget(t *testing.T) {
	limiter := rateLimiter{limit: 1, duration: time.Hour}
	now := time.Now()
	if err := limiter.reserve(now, false); err != nil {
		t.Fatal(err)
	}
	if err := limiter.reserve(now, false); !errors.Is(err, errRateLimitExceeded) {
		t.Fatal(err)
	}
}

func TestJWKSRefreshRechecksPreviouslyMissingKey(t *testing.T) {
	var calls atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return concurrencyResponse(), nil
	})})
	defer source.Stop()
	// A lookup that observed a miss before another refresh completed must check
	// the newly published snapshot before reserving another HTTP request.
	if err := source.requestKeysFor(context.Background(), "known", true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("cached key triggered redundant request: %d", calls.Load())
	}
}

func TestJWKSNewWaiterDrainsAbandonedRefresh(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls, active atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if active.Add(1) != 1 {
			t.Error("overlapping HTTP refresh requests")
		}
		defer active.Add(-1)
		call := calls.Add(1)
		if call == 2 {
			close(started)
			<-r.Context().Done()
			close(canceled)
			<-release
			return nil, r.Context().Err()
		}
		response := concurrencyResponse()
		if call > 2 {
			response.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(concurrencyJWKS, "known", "rotated")))
		}
		return response, nil
	})})
	defer source.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(ctx, "rotated"); first <- err }()
	awaitRefreshSignal(t, started)
	cancel()
	if err := awaitRefreshResult(t, first); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	awaitRefreshSignal(t, canceled)
	next := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "rotated"); next <- err }()
	close(release)
	if err := awaitRefreshResult(t, next); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("fresh caller did not refresh after abandoned load: %d", calls.Load())
	}
}

func TestJWKSPostStopLookupDrainsCanceledRefresh(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls, active atomic.Int32
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(r *http.Request) (*http.Response, error) {
		if active.Add(1) != 1 {
			t.Error("overlapping HTTP refresh requests")
		}
		defer active.Add(-1)
		call := calls.Add(1)
		if call == 2 {
			close(started)
			<-r.Context().Done()
			close(canceled)
			<-release
			return nil, r.Context().Err()
		}
		response := concurrencyResponse()
		if call > 2 {
			response.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(concurrencyJWKS, "known", "rotated")))
		}
		return response, nil
	})})
	defer source.Stop()
	first := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "rotated"); first <- err }()
	awaitRefreshSignal(t, started)
	source.Stop()
	awaitRefreshSignal(t, canceled)
	next := make(chan error, 1)
	waiting := &refreshWaitingContext{Context: context.Background(), waiting: make(chan struct{})}
	// Enter the same refresh path as a public cache miss, after its cache lookup.
	// Done is observed when coordination reaches a wait on the current flight.
	go func() { next <- source.requestKeysFor(waiting, "rotated", true) }()
	awaitRefreshSignal(t, waiting.waiting)
	close(release)
	if err := awaitRefreshResult(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("original waiter: %v", err)
	}
	if err := awaitRefreshResult(t, next); err != nil {
		t.Fatalf("post-Stop waiter: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("post-Stop lookup did not reload: %d", calls.Load())
	}
	if _, err := source.FetchPublicKey(context.Background(), "rotated"); err != nil {
		t.Fatal(err)
	}
}

type concurrencyClient func(*http.Request) (*http.Response, error)

func (f concurrencyClient) Do(r *http.Request) (*http.Response, error) { return f(r) }
func concurrencyResponse() *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(concurrencyJWKS))}
}
func awaitRefreshResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation blocked behind refresh")
		return nil
	}
}

func awaitRefreshSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh synchronization did not complete")
	}
}

func awaitRefreshWaiters(t *testing.T, source *KeySourceJWKS, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		source.refreshMu.Lock()
		ready := source.flight != nil && source.flight.waiters >= count
		source.refreshMu.Unlock()
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("callers did not join shared refresh")
		}
		runtime.Gosched()
	}
}

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error { return errors.New("close failed") }

// refreshWaitingContext observes a refresh wait at the private coordination
// boundary, without relying on a count of calls to context methods.
type refreshWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *refreshWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}
