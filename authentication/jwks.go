package authentication

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const maxInt64 int64 = 1<<63 - 1

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// KeySourceJWKS is a key source which fetches keys from JWKS endpoint
type KeySourceJWKS struct {
	client                 HTTPClient
	maxResponseBytes       int64
	maxCacheAge            time.Duration
	lastSuccess            time.Time
	now                    func() time.Time
	reserveRefreshCapacity bool
	refreshInterval        time.Duration
	requestOnUnknownKID    bool
	url                    string
	snapshot               *jwksSnapshot
	verificationPolicy     *JWKSVerificationPolicy
	policyErr              error
	warnFunc               func(string)
	diagnosticFunc         func(*JWKSError)
	mu                     sync.RWMutex
	rl                     *rateLimiter
	started                bool
	managed                bool
	starting               bool
	stopped                bool
	closed                 bool
	activeWork             int
	shutdownDone           chan struct{}
	lifetime               context.Context
	refreshMu              sync.Mutex
	flight                 *keyRefresh
	cancel                 func()
}

// JWKSOptions holds options for JWKS key source
type JWKSOptions struct {
	Client HTTPClient
	// VerificationPolicy opts into public JWT verification keys. Nil preserves
	// legacy acceptance. Construction copies the policy and its algorithms.
	VerificationPolicy *JWKSVerificationPolicy
	// MaxResponseBytes limits a successful response body. Zero leaves it unlimited.
	MaxResponseBytes int64
	// MaxCacheAge rejects snapshots at or beyond this age. Zero leaves age unlimited.
	MaxCacheAge time.Duration
	// ReserveRefreshCapacity reserves the final rate-limit slot for periodic refresh.
	ReserveRefreshCapacity bool
	RefreshInterval        time.Duration
	RequestOnUnknownKID    bool
	// WarnFunc receives safe diagnostic text when DiagnosticFunc is unset.
	WarnFunc func(string)
	// DiagnosticFunc receives structured refresh failures and warnings. It takes
	// precedence over WarnFunc. Callbacks may run concurrently, execute outside
	// source locks, and are awaited by Shutdown. They may call Stop, but must not
	// call Shutdown, which would wait for the callback itself.
	DiagnosticFunc func(*JWKSError)
	limit          int
	duration       time.Duration
	rateLimitSet   bool
}

// SetRefreshRateLimit sets rate limit for key requests
func (o *JWKSOptions) SetRefreshRateLimit(limit int, duration time.Duration) {
	o.rateLimitSet = true
	o.limit = limit
	o.duration = duration
}

// NewKeySourceJWKS creates a new KeySourceJWKS and starts refreshing keys
func NewKeySourceJWKS(jwksURL string, options ...*JWKSOptions) *KeySourceJWKS {
	source := newKeySourceJWKS(jwksURL, options...)
	ctx, cancel := context.WithCancel(context.Background())
	source.cancel = cancel
	if source.refreshInterval < 0 {
		source.refreshInterval = time.Minute
		source.reportDiagnostic(&JWKSError{operation: "jwks_config", stage: "validate", reason: "negative_refresh_interval", field: "RefreshInterval", outcome: "defaulted"})
	}
	source.startRefreshingKeys(ctx)
	return source
}

// NewManagedKeySourceJWKS validates configuration without making requests or
// starting goroutines. Start must succeed before FetchPublicKey can be used.
func NewManagedKeySourceJWKS(jwksURL string, options ...*JWKSOptions) (*KeySourceJWKS, error) {
	endpoint, err := url.Parse(jwksURL)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Hostname() == "" {
		return nil, &JWKSError{stage: "config", reason: "invalid_endpoint", field: "endpoint", cause: err}
	}
	if len(options) > 0 {
		o := options[0]
		if o.RefreshInterval < 0 {
			return nil, &JWKSError{stage: "config", reason: "negative_refresh_interval", field: "RefreshInterval"}
		}
		if o.MaxCacheAge < 0 {
			return nil, &JWKSError{stage: "config", reason: "negative_cache_age", field: "MaxCacheAge"}
		}
		if o.ReserveRefreshCapacity && (o.limit <= 0 || o.duration <= 0) {
			return nil, &JWKSError{stage: "config", reason: "missing_refresh_rate_limit", field: "ReserveRefreshCapacity"}
		}
		if o.MaxResponseBytes < 0 {
			return nil, &JWKSError{stage: "config", reason: "negative_response_limit", field: "MaxResponseBytes"}
		}
		if (o.rateLimitSet || o.limit != 0 || o.duration != 0) && (o.limit <= 0 || o.duration <= 0) {
			return nil, &JWKSError{stage: "config", reason: "invalid_rate_limit", field: "SetRefreshRateLimit"}
		}
	}
	source := newKeySourceJWKS(jwksURL, options...)
	source.managed = true
	if source.policyErr != nil {
		return nil, source.policyErr
	}
	return source, nil
}

// Start loads the initial keys and starts periodic refresh using ctx as the
// managed lifetime. A failed initial load can be retried. Concurrent startup
// returns ErrKeySourceStarting. Successful repeated calls retain the original
// lifetime. Stop is terminal for managed sources. Legacy sources already start
// in their constructor, so Start leaves their lifetime unchanged.
func (k *KeySourceJWKS) Start(ctx context.Context) error {
	k.refreshMu.Lock()
	if k.closed {
		k.refreshMu.Unlock()
		return ErrKeySourceStopped
	}
	if !k.managed {
		k.refreshMu.Unlock()
		return nil
	}
	if err := k.lifecycleErrorLocked(false); err != nil {
		k.refreshMu.Unlock()
		return err
	}
	if k.started {
		k.refreshMu.Unlock()
		return nil
	}
	if k.starting {
		k.refreshMu.Unlock()
		return ErrKeySourceStarting
	}
	if err := ctx.Err(); err != nil {
		k.refreshMu.Unlock()
		return err
	}
	lifetime, cancel := context.WithCancel(ctx)
	k.lifetime = lifetime
	k.cancel = cancel
	k.starting = true
	k.activeWork++
	k.refreshMu.Unlock()
	defer k.finishWork()
	err := k.requestKeys(lifetime)
	k.refreshMu.Lock()
	k.starting = false
	if stateErr := k.lifecycleErrorLocked(false); stateErr != nil && err == nil {
		err = stateErr
	}
	if err != nil {
		cancel()
		if !k.stopped {
			k.lifetime = nil
			k.cancel = nil
		}
		k.refreshMu.Unlock()
		return err
	}
	k.started = true
	k.activeWork++
	k.refreshMu.Unlock()
	go k.refreshLoop(lifetime)
	return nil
}

// FetchPublicKey fetches the public key with the specified kid
func (k *KeySourceJWKS) FetchPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	return k.fetchPublicKey(ctx, kid, "")
}

// FetchPublicKeyForAlgorithm also enforces the selected key's algorithm when a
// verification policy is enabled. JWTv5Parser uses this optional capability;
// custom parsers should call it when checking tokens against that policy.
func (k *KeySourceJWKS) FetchPublicKeyForAlgorithm(ctx context.Context, kid, algorithm string) (crypto.PublicKey, error) {
	if k.verificationPolicy != nil && algorithm == "" {
		return nil, rejectedJWK("lookup", "missing_algorithm", "header.alg", nil)
	}
	return k.fetchPublicKey(ctx, kid, algorithm)
}

// Stop stops background refresh and cancels the current shared request.
// For managed sources Stop is terminal. Legacy sources retain cached reads
// and allow later requests for unknown keys.
func (k *KeySourceJWKS) Stop() {
	k.refreshMu.Lock()
	k.stopLocked()
	k.refreshMu.Unlock()
}

// Shutdown permanently closes the source and waits for its startup, refreshes,
// response cleanup, and diagnostic callbacks. It does not close the shared HTTP
// client or wait for external authentication handlers. The caller owns ctx's
// deadline; a context error means work may still be finishing. Subsequent calls
// can continue waiting. Call Shutdown outside diagnostic callbacks; callbacks
// may use the nonblocking Stop instead.
func (k *KeySourceJWKS) Shutdown(ctx context.Context) error {
	k.refreshMu.Lock()
	if !k.closed {
		k.closed = true
		k.stopLocked()
		if k.activeWork == 0 {
			close(k.shutdownDone)
		}
	}
	done := k.shutdownDone
	k.refreshMu.Unlock()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (k *KeySourceJWKS) fetchPublicKey(ctx context.Context, kid, algorithm string) (crypto.PublicKey, error) {
	select {
	default:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := k.lifecycleError(); err != nil {
		return nil, err
	}
	key, found, err := k.cachedKey(kid, algorithm)
	if err != nil || found {
		return key, err
	}

	if !k.requestOnUnknownKID {
		return nil, ErrKeyNotFound
	}

	if err := k.requestKeysFor(ctx, kid, true); err != nil {
		if lifecycleErr := k.lifecycleError(); lifecycleErr != nil {
			return nil, errors.Join(lifecycleErr, err)
		}
		if errors.Is(err, ErrJWKSRateLimited) {
			return nil, &classifiedError{category: ErrKeyNotFound, cause: err}
		}
		return nil, fmt.Errorf("failed to request keys: %w", err)
	}
	if err := k.lifecycleError(); err != nil {
		return nil, err
	}
	key, found, err = k.cachedKey(kid, algorithm)
	if err != nil || found {
		return key, err
	}

	return nil, ErrKeyNotFound
}

func (k *KeySourceJWKS) stopLocked() {
	if k.managed {
		k.stopped = true
	}
	if k.cancel != nil {
		k.cancel()
	}
	if k.flight != nil {
		k.flight.abandoned = true
		k.flight.cancel()
	}
}

func newKeySourceJWKS(jwksURL string, options ...*JWKSOptions) *KeySourceJWKS {
	source := &KeySourceJWKS{client: http.DefaultClient, refreshInterval: time.Minute, url: jwksURL, snapshot: &jwksSnapshot{keys: make(map[string]crypto.PublicKey)}, rl: new(rateLimiter), now: time.Now, shutdownDone: make(chan struct{})}
	if len(options) > 0 {
		options[0].apply(source)
	}
	return source
}

func (k *KeySourceJWKS) cachedKey(kid, algorithm string) (crypto.PublicKey, bool, error) {
	if k.policyErr != nil {
		return nil, false, k.policyErr
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.maxCacheAge > 0 && (k.lastSuccess.IsZero() || k.now().Sub(k.lastSuccess) >= k.maxCacheAge) {
		return nil, false, fmt.Errorf("%w: operation=jwks_cache stage=validate reason=key_set_expired outcome=rejected", ErrKeySetExpired)
	}
	if k.verificationPolicy != nil && k.lastSuccess.IsZero() {
		return nil, false, rejectedJWK("lookup", "key_set_unavailable", "keys", nil)
	}
	key, found := k.snapshot.keys[kid]
	if found && algorithm != "" && k.verificationPolicy != nil && k.snapshot.algorithms[kid] != algorithm {
		return nil, false, rejectedJWK("lookup", "algorithm_mismatch", "header.alg", nil)
	}
	if !found {
		if _, rejected := k.snapshot.rejected[kid]; rejected {
			return nil, false, rejectedJWK("lookup", "key_not_for_verification", "header.kid", nil)
		}
	}
	return key, found, nil
}

func (k *KeySourceJWKS) lifecycleError() error {
	k.refreshMu.Lock()
	defer k.refreshMu.Unlock()
	return k.lifecycleErrorLocked(true)
}

func (k *KeySourceJWKS) lifecycleErrorLocked(requireStarted bool) error {
	if k.closed {
		return ErrKeySourceStopped
	}
	if !k.managed {
		return nil
	}
	if k.lifetime != nil && k.lifetime.Err() != nil {
		return errors.Join(ErrKeySourceStopped, k.lifetime.Err())
	}
	if k.stopped {
		return ErrKeySourceStopped
	}
	if requireStarted && !k.started {
		return ErrKeySourceNotStarted
	}
	return nil
}

// apply applies options to key source
func (o *JWKSOptions) apply(source *KeySourceJWKS) {
	if o.VerificationPolicy != nil {
		source.verificationPolicy = &JWKSVerificationPolicy{AllowedAlgorithms: append([]string(nil), o.VerificationPolicy.AllowedAlgorithms...)}
		source.policyErr = source.verificationPolicy.validate()
	}
	if o.MaxCacheAge > 0 {
		source.maxCacheAge = o.MaxCacheAge
	}
	source.reserveRefreshCapacity = o.ReserveRefreshCapacity
	if o.MaxResponseBytes > 0 {
		source.maxResponseBytes = o.MaxResponseBytes
	}
	if o.Client != nil {
		source.client = o.Client
	}
	if o.RefreshInterval != 0 {
		source.refreshInterval = o.RefreshInterval
	}
	if o.RequestOnUnknownKID {
		source.requestOnUnknownKID = o.RequestOnUnknownKID
	}
	if o.WarnFunc != nil {
		source.warnFunc = o.WarnFunc
	}
	source.diagnosticFunc = o.DiagnosticFunc
	if o.limit > 0 && o.duration > 0 {
		source.rl.limit = o.limit
		source.rl.duration = o.duration
	}
}

// requestKeys joins a single shared refresh. A caller owns only its wait, not
// the HTTP operation: canceling one wait must not abort another caller's load.
func (k *KeySourceJWKS) requestKeys(ctx context.Context) error {
	return k.requestKeysFor(ctx, "", false)
}

func (k *KeySourceJWKS) requestKeysFor(ctx context.Context, kid string, missingOnly bool) error {
	k.refreshMu.Lock()
	if err := k.lifecycleErrorLocked(false); err != nil {
		k.refreshMu.Unlock()
		return err
	}
	k.activeWork++
	k.refreshMu.Unlock()
	defer k.finishWork()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		k.refreshMu.Lock()
		if err := k.lifecycleErrorLocked(false); err != nil {
			k.refreshMu.Unlock()
			return err
		}
		if err := ctx.Err(); err != nil {
			k.refreshMu.Unlock()
			return err
		}
		if missingOnly {
			_, found, err := k.cachedKey(kid, "")
			if found || err != nil {
				k.refreshMu.Unlock()
				return err
			}
		}
		flight := k.flight
		if flight != nil && flight.abandoned {
			// Let the canceled operation finish before starting another network call.
			k.refreshMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-flight.done:
				continue
			}
		}
		if flight == nil {
			// Denied demand calls must not publish a flight that could hide reserved
			// capacity from a concurrent scheduled refresh.
			if err := k.rl.reserve(k.now(), k.reserveRefreshCapacity && missingOnly); err != nil {
				k.refreshMu.Unlock()
				diagnostic := &JWKSError{stage: "limit", reason: "rate_limit", cause: err}
				k.reportDiagnostic(diagnostic)
				return diagnostic
			}
			requestCtx, cancel := context.WithCancel(refreshValues{ctx})
			flight = &keyRefresh{done: make(chan struct{}), cancel: cancel}
			k.flight = flight
			k.activeWork++
			// One goroutine per shared load lets every waiter honor its own context.
			go k.refreshKeys(requestCtx, flight)
		}
		flight.waiters++
		k.refreshMu.Unlock()
		var err error
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-flight.done:
			err = flight.err
			if ctx.Err() != nil {
				err = ctx.Err()
			}
		}
		k.refreshMu.Lock()
		flight.waiters--
		if flight.waiters == 0 && k.flight == flight {
			flight.abandoned = true
			flight.cancel()
		}
		k.refreshMu.Unlock()
		return err
	}
}

func (k *KeySourceJWKS) refreshKeys(ctx context.Context, flight *keyRefresh) {
	defer k.finishWork()
	keys, warning, err := k.loadKeys(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		k.mu.Lock()
		k.snapshot = keys
		k.lastSuccess = k.now()
		k.mu.Unlock()
	}
	flight.err = err
	flight.cancel()
	k.refreshMu.Lock()
	k.flight = nil
	close(flight.done)
	k.refreshMu.Unlock()
	// Callbacks may reenter FetchPublicKey or Stop. Complete the flight first.
	var diagnostic *JWKSError
	// Only cancellation of the primary operation suppresses its diagnostic.
	// A secondary close failure may itself match context.Canceled.
	if errors.As(flight.err, &diagnostic) && !errors.Is(diagnostic.cause, context.Canceled) {
		k.reportDiagnostic(diagnostic)
	} else if warning != nil {
		k.reportDiagnostic(warning)
	}
}

func (k *KeySourceJWKS) loadKeys(ctx context.Context) (keys *jwksSnapshot, warning *JWKSError, err error) {
	if k.policyErr != nil {
		return nil, nil, k.policyErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, http.NoBody)
	if err != nil {
		return nil, nil, &JWKSError{stage: "request", reason: "request_invalid", field: "endpoint", cause: err}
	}
	response, err := k.client.Do(req)
	if err != nil {
		return nil, nil, &JWKSError{stage: "request", reason: "request_failed", cause: err}
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			warning = &JWKSError{stage: "close", reason: "body_close_failed", outcome: "warning", cause: closeErr}
			if err != nil {
				// All load errors are JWKSError values. Copy before attaching the
				// secondary cause so the primary diagnostic retains precedence.
				var primary *JWKSError
				if errors.As(err, &primary) {
					combined := *primary
					combined.closeCause = closeErr
					err = &combined
				}
			}
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, nil, &JWKSError{stage: "response", reason: "http_status", status: response.StatusCode}
	}
	var reader io.Reader = response.Body
	if k.maxResponseBytes > 0 {
		limit := k.maxResponseBytes
		if limit < maxInt64 {
			limit++
		}
		reader = io.LimitReader(reader, limit)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, &JWKSError{stage: "read", reason: "body_read_failed", cause: err}
	}
	if k.maxResponseBytes > 0 && int64(len(body)) > k.maxResponseBytes {
		return nil, nil, &JWKSError{stage: "read", reason: "response_too_large"}
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			return nil, nil, &JWKSError{stage: "decode", reason: "invalid_json", cause: err}
		}
		return nil, nil, &JWKSError{stage: "validate", reason: "invalid_envelope", cause: err}
	}
	rawKeys := bytes.TrimSpace(envelope["keys"])
	if len(rawKeys) == 0 || rawKeys[0] != '[' {
		return nil, nil, &JWKSError{stage: "validate", reason: "invalid_envelope", field: "keys"}
	}
	keys, err = decodeJWKSSnapshot(rawKeys, k.verificationPolicy)
	return keys, nil, err
}

func (k *KeySourceJWKS) reportDiagnostic(diagnostic *JWKSError) {
	if k.diagnosticFunc != nil {
		k.diagnosticFunc(diagnostic)
	} else if k.warnFunc != nil {
		k.warnFunc(diagnostic.Error())
	}
}

// keyRefresh publishes err by closing done. It is never reused.
type keyRefresh struct {
	done      chan struct{}
	cancel    context.CancelFunc
	err       error
	waiters   int
	abandoned bool
}

// refreshValues retains request-scoped client values without inheriting a
// waiter's cancellation. Stop or the last departing waiter cancels the load.
type refreshValues struct{ context.Context }

func (refreshValues) Deadline() (time.Time, bool) { return time.Time{}, false }
func (refreshValues) Done() <-chan struct{}       { return nil }
func (refreshValues) Err() error                  { return nil }

// startRefreshingKeys starts refreshing keys
func (k *KeySourceJWKS) startRefreshingKeys(ctx context.Context) {
	k.started = true
	_ = k.requestKeys(ctx)
	k.refreshMu.Lock()
	if k.closed {
		k.refreshMu.Unlock()
		return
	}
	k.activeWork++
	k.refreshMu.Unlock()
	go k.refreshLoop(ctx)
}

func (k *KeySourceJWKS) refreshLoop(ctx context.Context) {
	defer k.finishWork()
	if k.managed {
		defer k.Stop()
	}
	timer := time.NewTimer(k.refreshInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = k.requestKeys(ctx)
			timer.Reset(k.refreshInterval)
		}
	}
}

// Work is admitted under refreshMu before starting a goroutine or releasing
// the lock. Closing admission before waiting prevents late work after Shutdown.
func (k *KeySourceJWKS) finishWork() {
	k.refreshMu.Lock()
	k.activeWork--
	if k.closed && k.activeWork == 0 {
		close(k.shutdownDone)
	}
	k.refreshMu.Unlock()
}

// rateLimiter reserves a bounded number of refresh requests per time window.
type rateLimiter struct {
	limit     int
	requests  int
	duration  time.Duration
	lastCheck time.Time
	mu        sync.Mutex
}

// reserve accounts for one actual HTTP load. A reserved final slot is available
// to scheduled and initial loads, while all loads share the same total budget.
func (r *rateLimiter) reserve(now time.Time, reserveFinalSlot bool) error {
	if r.limit == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastCheck.IsZero() || now.Sub(r.lastCheck) >= r.duration {
		r.lastCheck = now
		r.requests = 0
	}
	limit := r.limit
	if reserveFinalSlot {
		limit--
	}
	if r.requests >= limit {
		return ErrJWKSRateLimited
	}
	r.requests++
	return nil
}
