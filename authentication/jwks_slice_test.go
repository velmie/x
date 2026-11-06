package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStaticJWKSVerificationAndKeyRejection(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwk := policyJWK(t, &key.PublicKey, "selected", "", "")
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	keyIDs := JWKSKeyIDPolicy{
		Required: true,
		MaxBytes: 8,
	}
	source, err := NewStaticJWKS([]byte(policyDocument(t, jwk)), policy, keyIDs)
	if err != nil {
		t.Fatal(err)
	}
	policy.AllowedAlgorithms[0] = "ES384"
	public, err := source.FetchPublicKeyForAlgorithm(context.Background(), "selected", "ES256")
	if err != nil {
		t.Fatal(err)
	}
	public.(*ecdsa.PublicKey).X.SetInt64(0)
	public, err = source.FetchPublicKeyForAlgorithm(context.Background(), "selected", "ES256")
	if err != nil || public.(*ecdsa.PublicKey).X.Sign() == 0 {
		t.Fatalf("returned key mutation changed static source: %v", err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "selected"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatal(err)
	}
	for _, algorithm := range []string{"", "ES384"} {
		_, err := source.FetchPublicKeyForAlgorithm(context.Background(), "selected", algorithm)
		if !errors.Is(err, ErrKeyRejected) || !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("algorithm %q: %v", algorithm, err)
		}
	}
}

func TestStrictJWKSRejectsDuplicateAcrossEligibilityAndStaticRejectsUnrelated(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	good := policyJWK(t, &key.PublicKey, "duplicate", "sig", "ES256")
	unrelated := policyJWK(t, &key.PublicKey, "duplicate", "enc", "ES256")
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	keyIDs := JWKSKeyIDPolicy{
		Required: true,
		MaxBytes: 128,
	}
	for _, keys := range [][]map[string]any{{good, unrelated}, {unrelated, good}} {
		document := policyDocument(t, keys...)
		if _, err := NewStaticJWKS([]byte(document), policy, keyIDs); !errors.Is(err, ErrJWKSKeyRejected) || errors.Is(err, ErrKeyRejected) {
			t.Fatalf("static document: %v", err)
		}
		for _, idPolicy := range []JWKSKeyIDPolicy{
			{
				RejectDuplicates: true,
			},
			{
				Required:         true,
				MaxBytes:         128,
				RejectDuplicates: true,
			},
		} {
			remote, err := NewManagedKeySourceJWKS(
				"https://example.test/keys",
				&JWKSOptions{
					Client: concurrencyClient(
						func(*http.Request) (*http.Response, error) {
							return responseWithBody(document), nil
						},
					),
					VerificationPolicy: &policy,
					KeyIDPolicy:        idPolicy,
					RefreshInterval:    time.Hour,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := remote.Start(context.Background()); !errors.Is(err, ErrJWKSKeyRejected) || errors.Is(err, ErrKeyRejected) {
				t.Fatalf("remote document: %v", err)
			}
			if err := remote.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	unrelated["kid"] = "other"
	_, err = NewStaticJWKS([]byte(policyDocument(t, good, unrelated)), policy, keyIDs)
	var diagnostic *JWKSError
	if !errors.Is(err, ErrJWKSKeyRejected) || !errors.As(err, &diagnostic) || diagnostic.Field() != "keys[1].use" {
		t.Fatalf("static accepted unrelated key: %v", err)
	}
}

func TestStaticJWKSRejectsInvalidDocumentsAndIDBounds(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := JWKSVerificationPolicy{
		AllowedAlgorithms: []string{"ES256"},
	}
	keyIDs := JWKSKeyIDPolicy{
		Required: true,
		MaxBytes: 4,
	}
	for _, kid := range []string{"", "12345"} {
		document := policyDocument(t, policyJWK(t, &key.PublicKey, kid, "sig", "ES256"))
		if _, err := NewStaticJWKS([]byte(document), policy, keyIDs); !errors.Is(err, ErrJWKSKeyRejected) {
			t.Fatalf("kid length %d: %v", len(kid), err)
		}
	}
	valid := policyDocument(t, policyJWK(t, &key.PublicKey, "1234", "sig", "ES256"))
	if _, err := NewStaticJWKS([]byte(valid), policy, keyIDs); err != nil {
		t.Fatal(err)
	}
	exactLimit := valid + strings.Repeat(" ", MaxStaticJWKSBytes-len(valid))
	if _, err := NewStaticJWKS([]byte(exactLimit), policy, keyIDs); err != nil {
		t.Fatalf("exact document limit rejected: %v", err)
	}
	for _, document := range []string{"", `{}`, `{"keys":[]}`, strings.Repeat(" ", MaxStaticJWKSBytes+1)} {
		if _, err := NewStaticJWKS([]byte(document), policy, keyIDs); err == nil {
			t.Fatalf("accepted document of length %d", len(document))
		}
	}
	keyIDs.MaxBytes = -1
	if _, err := NewStaticJWKS([]byte(valid), policy, keyIDs); err == nil {
		t.Fatal("accepted negative key ID limit")
	}
}

func TestManagedJWKSStartAfterInitialFailureRecoversOnDemand(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := policyDocument(t, policyJWK(t, &key.PublicKey, "known", "sig", "ES256"))
	dependency := errors.New("connection failed")
	diagnostics := make(chan *JWKSError, 1)
	var calls atomic.Int32
	opts := &JWKSOptions{
		Client: concurrencyClient(
			func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return nil, dependency
				}
				return responseWithBody(body), nil
			},
		),
		VerificationPolicy: &JWKSVerificationPolicy{
			AllowedAlgorithms: []string{"ES256"},
		},
		RefreshInterval:          time.Hour,
		MaxCacheAge:              time.Minute,
		RequestOnUnknownKID:      true,
		AllowInitialFetchFailure: true,
		DiagnosticFunc: func(diagnostic *JWKSError) {
			diagnostics <- diagnostic
		},
	}
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(
		func() {
			if err := source.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		},
	)
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case diagnostic := <-diagnostics:
		if !errors.Is(diagnostic, dependency) {
			t.Fatalf("diagnostic lost cause: %v", diagnostic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing initial diagnostic")
	}
	if _, err := source.FetchPublicKeyForAlgorithm(context.Background(), "known", "ES256"); err != nil {
		t.Fatalf("demand refresh failed before periodic timer: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected initial request and one demand refresh, got %d", calls.Load())
	}
}

func TestManagedJWKSRateLimitDistinguishesUnconfirmedAbsence(t *testing.T) {
	dependency := errors.New("connection failed")
	opts := &JWKSOptions{
		Client: concurrencyClient(
			func(*http.Request) (*http.Response, error) {
				return nil, dependency
			},
		),
		RefreshInterval:          time.Hour,
		RequestOnUnknownKID:      true,
		AllowInitialFetchFailure: true,
	}
	opts.SetRefreshRateLimit(1, time.Hour)
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", opts)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = source.FetchPublicKey(context.Background(), "unknown")
	if !errors.Is(err, ErrJWKSRateLimited) || errors.Is(err, ErrKeyNotFound) || !errors.Is(err, dependency) {
		t.Fatalf("unconfirmed absence: %v", err)
	}
}

func TestManagedJWKSNoSnapshotDoesNotHideInitialFailure(t *testing.T) {
	dependency := errors.New("connection failed")
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(*http.Request) (*http.Response, error) {
					return nil, dependency
				},
			),
			RefreshInterval:          time.Hour,
			AllowInitialFetchFailure: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = source.FetchPublicKey(context.Background(), "unknown")
	if !errors.Is(err, dependency) || errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("initial failure was hidden: %v", err)
	}
}

func TestManagedJWKSAllowFailureDoesNotAllowCanceledStartup(t *testing.T) {
	entered := make(chan struct{})
	diagnostics := make(chan *JWKSError, 1)
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(r *http.Request) (*http.Response, error) {
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				},
			),
			AllowInitialFetchFailure: true,
			DiagnosticFunc: func(diagnostic *JWKSError) {
				diagnostics <- diagnostic
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- source.Start(ctx) }()
	awaitRefreshSignal(t, entered)
	cancel()
	if err := awaitRefreshResult(t, started); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled startup succeeded: %v", err)
	}
	if err := source.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case diagnostic := <-diagnostics:
		t.Fatalf("lifetime cancellation emitted a diagnostic: %v", diagnostic)
	default:
	}
}

func TestFreshCheckDoesNotWarmCacheOrConsumeQuota(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	known := policyDocument(t, policyJWK(t, &key.PublicKey, "known", "sig", "ES256"))
	rotated := policyDocument(t, policyJWK(t, &key.PublicKey, "rotated", "sig", "ES256"))
	var calls atomic.Int32
	opts := &JWKSOptions{
		Client: concurrencyClient(
			func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return responseWithBody(known), nil
				}
				return responseWithBody(rotated), nil
			},
		),
		VerificationPolicy: &JWKSVerificationPolicy{
			AllowedAlgorithms: []string{"ES256"},
		},
		RefreshInterval:     time.Hour,
		RequestOnUnknownKID: true,
	}
	opts.SetRefreshRateLimit(2, time.Hour)
	source, err := NewManagedKeySourceJWKS("https://example.test/keys", opts)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "known"); err != nil {
		t.Fatalf("check replaced cache: %v", err)
	}
	if _, err := source.FetchPublicKey(context.Background(), "rotated"); err != nil {
		t.Fatalf("check consumed refresh quota: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected distinct startup, check and refresh I/O: %d", calls.Load())
	}
}

func TestFreshCheckReturnsStatusReadCloseAndEmptyFailures(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	responses := []*http.Response{
		{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("provider body")),
		},
		{
			StatusCode: http.StatusOK,
			Body: responseFailureBody{
				err:      readErr,
				closeErr: closeErr,
			},
		},
		{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"keys":[]}`)),
		},
		{
			StatusCode: http.StatusOK,
			Body: shutdownBody{
				Reader: strings.NewReader(concurrencyJWKS),
				close:  func() error { return closeErr },
			},
		},
	}
	var calls int
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(*http.Request) (*http.Response, error) {
					response := responses[calls]
					calls++
					return response, nil
				},
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Shutdown(context.Background())
	var diagnostic *JWKSError
	if err := source.Check(context.Background()); !errors.As(err, &diagnostic) || diagnostic.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("missing HTTP status: %v", err)
	}
	if err := source.Check(context.Background()); !errors.Is(err, readErr) || !errors.Is(err, closeErr) {
		t.Fatalf("missing read or close cause: %v", err)
	}
	if err := source.Check(context.Background()); err == nil {
		t.Fatal("empty set reported ready")
	}
	if err := source.Check(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("successful response lost close failure: %v", err)
	}
}

func TestShutdownCancelsAndJoinsFreshCheck(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	source, err := NewManagedKeySourceJWKS(
		"https://example.test/keys",
		&JWKSOptions{
			Client: concurrencyClient(
				func(r *http.Request) (*http.Response, error) {
					close(entered)
					<-r.Context().Done()
					<-release
					return nil, r.Context().Err()
				},
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	checked := make(chan error, 1)
	go func() { checked <- source.Check(context.Background()) }()
	awaitRefreshSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := source.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown returned before check finished: %v", err)
	}
	once.Do(func() { close(release) })
	if err := source.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := awaitRefreshResult(t, checked); !errors.Is(err, context.Canceled) {
		t.Fatalf("check did not retain cancellation: %v", err)
	}
}
