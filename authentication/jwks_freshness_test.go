package authentication

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWKSCacheAgeBoundaryAndRecovery(t *testing.T) {
	clock := newJWKSClock()
	var calls atomic.Int32
	fail := atomic.Bool{}
	empty := atomic.Bool{}
	dependency := errors.New("endpoint unavailable")
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{MaxCacheAge: time.Minute, RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		if fail.Load() {
			return nil, dependency
		}
		if empty.Load() {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"keys":[]}`))}, nil
		}
		return concurrencyResponse(), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	source.now = clock.Now
	defer source.Stop()
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute - time.Nanosecond)
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if err := source.requestKeys(context.Background()); !errors.Is(err, dependency) {
		t.Fatal(err)
	}
	clock.Advance(time.Nanosecond)
	for _, kid := range []string{"known", "missing"} {
		if _, err := source.FetchPublicKey(context.Background(), kid); !errors.Is(err, ErrKeySetExpired) {
			t.Fatalf("%s: %v", kid, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("expired read triggered foreground refresh")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "subject"})
	token.Header["kid"] = "known"
	encoded, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	method := NewViaJWT(NewJWTv5Parser(jwt.NewParser()), source)
	if _, err := method.Authenticate(context.Background(), encoded); !errors.Is(err, ErrKeySetExpired) {
		t.Fatal(err)
	}
	fail.Store(false)
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := method.Authenticate(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}
	empty.Store(true)
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A fresh empty snapshot means confirmed absence, not expiration.
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != ErrKeyNotFound {
		t.Fatal(err)
	}
}

func TestJWKSNoSuccessfulSnapshotExpiresAndZeroAgePreservesLegacy(t *testing.T) {
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{MaxCacheAge: time.Minute, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") })})
	defer source.Stop()
	if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeySetExpired) {
		t.Fatal(err)
	}
	legacy, err := NewManagedKeySourceJWKS("https://example.test/keys", &JWKSOptions{RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil })})
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Stop()
	clock := newJWKSClock()
	legacy.now = clock.Now
	if err := legacy.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(365 * 24 * time.Hour)
	if _, err := legacy.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
}

func TestJWKSReservedCapacityAllowsScheduledRotation(t *testing.T) {
	clock := newJWKSClock()
	var calls atomic.Int32
	options := &JWKSOptions{ReserveRefreshCapacity: true, RefreshInterval: time.Hour, RequestOnUnknownKID: true, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { calls.Add(1); return concurrencyResponse(), nil })}
	options.SetRefreshRateLimit(3, time.Minute)
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", options)
	if err != nil {
		t.Fatal(err)
	}
	source.now = clock.Now
	defer source.Stop()
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Initial load and first miss consume two slots. Further misses cannot use the final slot.
	for i := 0; i < 16; i++ {
		if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeyNotFound) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("misses consumed reserved capacity: %d", calls.Load())
	}
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatalf("scheduled rotation denied: %v", err)
	}
	if err := source.requestKeys(context.Background()); !errors.Is(err, errRateLimitExceeded) {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("total budget exceeded: %d", calls.Load())
	}
	clock.Advance(time.Minute)
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatalf("window did not renew: %v", err)
	}
}

func TestJWKSFreshnessAndCapacityConfiguration(t *testing.T) {
	for _, options := range []JWKSOptions{{MaxCacheAge: -1}, {ReserveRefreshCapacity: true}} {
		if _, err := NewManagedKeySourceJWKS("https://example.test/keys", &options); err == nil {
			t.Fatalf("accepted invalid configuration %+v", options)
		}
	}
	source := NewKeySourceJWKS("https://example.test/keys", &JWKSOptions{MaxCacheAge: -1, ReserveRefreshCapacity: true, RequestOnUnknownKID: true, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { return concurrencyResponse(), nil })})
	defer source.Stop()
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "missing"); err != ErrKeyNotFound {
		t.Fatal(err)
	}
}

func TestJWKSReservedSlotSurvivesDeniedMissAndCoalescesRealLoad(t *testing.T) {
	warning, releaseWarning := make(chan struct{}), make(chan struct{})
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		select {
		case <-releaseWarning:
		default:
			close(releaseWarning)
		}
	}()
	var calls atomic.Int32
	options := &JWKSOptions{ReserveRefreshCapacity: true, RefreshInterval: time.Hour, RequestOnUnknownKID: true, WarnFunc: func(string) { close(warning); <-releaseWarning }, Client: concurrencyClient(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return concurrencyResponse(), nil
		}
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(concurrencyJWKS, "known", "rotated")))}, nil
	})}
	options.SetRefreshRateLimit(2, time.Hour)
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", options)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Stop()
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	denied := make(chan error, 1)
	go func() { _, err := source.FetchPublicKey(context.Background(), "missing"); denied <- err }()
	awaitRefreshSignal(t, warning)
	// Even while the rejected caller is reporting its outcome, it must not own
	// a flight that would mask the reserved slot from scheduled refresh.
	scheduled := make(chan error, 1)
	go func() { scheduled <- source.requestKeys(context.Background()) }()
	awaitRefreshSignal(t, started)
	const waiters = 8
	results := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() { _, err := source.FetchPublicKey(context.Background(), "rotated"); results <- err }()
	}
	awaitRefreshWaiters(t, source, waiters+1)
	close(release)
	if err := awaitRefreshResult(t, scheduled); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < waiters; i++ {
		if err := awaitRefreshResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	close(releaseWarning)
	if err := awaitRefreshResult(t, denied); !errors.Is(err, ErrKeyNotFound) || err == ErrKeyNotFound {
		t.Fatalf("rate denial must be distinguishable from confirmed absence: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("reserved or coalesced load changed total budget: %d", calls.Load())
	}
}

func TestJWKSReservedSingleSlotOnlyAllowsScheduledRefresh(t *testing.T) {
	clock := newJWKSClock()
	var calls atomic.Int32
	options := &JWKSOptions{ReserveRefreshCapacity: true, RequestOnUnknownKID: true, RefreshInterval: time.Hour, Client: concurrencyClient(func(*http.Request) (*http.Response, error) { calls.Add(1); return concurrencyResponse(), nil })}
	options.SetRefreshRateLimit(1, time.Minute)
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", options)
	if err != nil {
		t.Fatal(err)
	}
	source.now = clock.Now
	defer source.Stop()
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("demand used only scheduled slot")
	}
	if err := source.requestKeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("scheduled request did not use reserved slot")
	}
}

type jwksClock struct{ nanoseconds atomic.Int64 }

func newJWKSClock() *jwksClock {
	clock := new(jwksClock)
	clock.nanoseconds.Store(time.Unix(1700000000, 0).UnixNano())
	return clock
}
func (c *jwksClock) Now() time.Time          { return time.Unix(0, c.nanoseconds.Load()) }
func (c *jwksClock) Advance(d time.Duration) { c.nanoseconds.Add(int64(d)) }
