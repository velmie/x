package authx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/authx"
)

func TestJWTMethodConcurrentInitialization(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{})
	var arrival, response sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrival.Do(func() { close(arrived) })
		<-release
		jwksHandler(w, r)
	}))
	defer server.Close()
	defer response.Do(func() { close(release) })
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	method, err := authx.NewJWTMethod(authx.WithJWKSSource(endpoint), authx.WithJWKSMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-arrived:
	case <-ctx.Done():
		t.Fatal("initial request did not reach the endpoint")
	}
	firstRead := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, initialErr := method.Authenticate(ctx, validToken)
		close(firstRead)
		if initialErr == nil {
			done <- nil
			return
		}
		for ctx.Err() == nil {
			if _, err := method.Authenticate(ctx, validToken); err == nil {
				done <- nil
				return
			}
			runtime.Gosched()
		}
		done <- ctx.Err()
	}()
	<-firstRead
	response.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("source did not become usable for concurrent authentication: %v", err)
	}
}
