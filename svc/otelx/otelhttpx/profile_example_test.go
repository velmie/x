package otelhttpx_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/otelx/otelhttpx"
)

func ExampleNewHandler() {
	runtime, err := otelx.Open(context.Background(), otelx.RuntimeConfig{})
	if err != nil {
		panic(err)
	}
	handler, err := otelhttpx.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		otelhttpx.HandlerConfig{
			Operation:  "books.list",
			Route:      "/books",
			Tracer:     runtime.TracerProvider.Tracer("books"),
			Meter:      runtime.MeterProvider.Meter("books"),
			Propagator: runtime.Propagator,
		},
	)
	if err != nil {
		panic(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/books", nil))
	if err := runtime.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println(response.Code)
	// Output: 204
}

func ExampleNewTransport() {
	runtime, err := otelx.Open(context.Background(), otelx.RuntimeConfig{})
	if err != nil {
		panic(err)
	}
	transport, err := otelhttpx.NewTransport(
		roundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("ok")),
				}, nil
			},
		),
		otelhttpx.TransportConfig{
			Operation:  "peer.fetch",
			Tracer:     runtime.TracerProvider.Tracer("client"),
			Meter:      runtime.MeterProvider.Meter("client"),
			Propagator: runtime.Propagator,
		},
	)
	if err != nil {
		panic(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/", nil)
	if err != nil {
		panic(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		panic(err)
	}
	if err := response.Body.Close(); err != nil {
		panic(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println(response.StatusCode)
	// Output: 200
}
