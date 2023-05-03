package authentication

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v3"
)

const (
	errRateLimitExceeded = Error("rate limit exceeded")
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// KeySourceJWKS is a key source which fetches keys from JWKS endpoint
type KeySourceJWKS struct {
	client              HTTPClient
	refreshInterval     time.Duration
	requestOnUnknownKID bool
	url                 string
	keys                map[string]crypto.PublicKey
	warnFunc            func(string)
	mu                  sync.RWMutex
	rl                  *rateLimiter
	ctx                 context.Context
	started             bool
	cancel              func()
}

// JWKSOptions holds options for JWKS key source
type JWKSOptions struct {
	Client              HTTPClient
	RefreshInterval     time.Duration
	RequestOnUnknownKID bool
	WarnFunc            func(string)
	limit               int
	duration            time.Duration
}

// SetRefreshRateLimit sets rate limit for key requests
func (o *JWKSOptions) SetRefreshRateLimit(limit int, duration time.Duration) {
	o.limit = limit
	o.duration = duration
}

// apply applies options to key source
func (o *JWKSOptions) apply(source *KeySourceJWKS) {
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
	if o.limit > 0 && o.duration > 0 {
		source.rl.limit = o.limit
		source.rl.duration = o.duration
	}
}

// NewKeySourceJWKS creates a new KeySourceJWKS and starts refreshing keys
func NewKeySourceJWKS(jwksURL string, options ...*JWKSOptions) *KeySourceJWKS {
	ctx, cancel := context.WithCancel(context.Background())

	source := &KeySourceJWKS{
		client:          http.DefaultClient,
		refreshInterval: time.Minute,
		url:             jwksURL,
		keys:            make(map[string]crypto.PublicKey),
		rl:              new(rateLimiter),
		ctx:             ctx,
		cancel:          cancel,
	}
	if len(options) > 0 {
		options[0].apply(source)
	}

	source.startRefreshingKeys()

	return source
}

// FetchPublicKey fetches the public key with the specified kid
func (k *KeySourceJWKS) FetchPublicKey(kid string) (crypto.PublicKey, error) {
	k.mu.RLock()
	if key, ok := k.keys[kid]; ok {
		k.mu.RUnlock()
		return key, nil
	}
	k.mu.RUnlock()

	if !k.requestOnUnknownKID {
		return nil, ErrKeyNotFound
	}

	if err := k.requestKeys(); err != nil {
		if errors.Is(err, errRateLimitExceeded) {
			if k.warnFunc != nil {
				k.warnFunc(fmt.Sprintf("%s: caused by the key id '%s'", err, kid))
			}
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("failed to request keys: %w", err)
	}
	k.mu.RLock()
	if key, ok := k.keys[kid]; ok {
		k.mu.RUnlock()
		return key, nil
	}
	k.mu.RUnlock()

	return nil, ErrKeyNotFound
}

// requestKeys requests the JWKS and updates the local keys
func (k *KeySourceJWKS) requestKeys() error {
	return k.rl.Exec(func() error {
		k.mu.Lock()
		defer k.mu.Unlock()

		req, err := http.NewRequestWithContext(k.ctx, http.MethodGet, k.url, http.NoBody)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		response, err := k.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to request keys: %w", err)
		}
		defer func() {
			if closeErr := response.Body.Close(); closeErr != nil {
				if k.warnFunc != nil {
					k.warnFunc(fmt.Sprintf("failed to close response body: %s", closeErr))
				}
			}
		}()

		var body []byte
		body, err = io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP status %d, failed to request keys: %s", response.StatusCode, body)
		}
		jwks := new(jose.JSONWebKeySet)
		if err = json.Unmarshal(body, jwks); err != nil {
			return fmt.Errorf("failed to unmarshal keys: %w", err)
		}

		type publicDeriver interface {
			Public() crypto.PublicKey
		}

		k.keys = make(map[string]crypto.PublicKey, len(jwks.Keys)) // reset keys
		for _, key := range jwks.Keys {
			kk := key.Key
			if deriver, ok := kk.(publicDeriver); ok {
				kk = deriver.Public()
			}
			k.keys[key.KeyID] = kk
		}
		return nil
	})
}

// Stop stops the KeySourceJWKS from refreshing keys
func (k *KeySourceJWKS) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cancel()
	k.ctx = context.Background()
}

// startRefreshingKeys starts refreshing keys
func (k *KeySourceJWKS) startRefreshingKeys() {
	k.mu.Lock()

	if k.started {
		k.mu.Unlock()
		return
	}
	k.started = true
	k.mu.Unlock()
	refreshFunc := func() {
		if err := k.requestKeys(); err != nil {
			if k.warnFunc != nil {
				k.warnFunc(fmt.Sprintf("failed to request keys: %s", err))
			}
		}
	}
	refreshFunc() // initial request
	go func() {
		for {
			select {
			case <-k.ctx.Done():
				return
			case <-time.After(k.refreshInterval):
				refreshFunc()
			}
		}
	}()
}

// rateLimiter limits the number of executions of a function
type rateLimiter struct {
	limit     int
	requests  int
	duration  time.Duration
	lastCheck time.Time
	mu        sync.Mutex
}

// Exec executes the function if the rate limit is not exceeded
func (r *rateLimiter) Exec(fn func() error) error {
	if r.limit == 0 {
		return fn()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.lastCheck) > r.duration {
		r.lastCheck = time.Now()
		r.requests = 0
	}

	if r.requests >= r.limit {
		return errRateLimitExceeded
	}
	r.requests++
	return fn()
}
