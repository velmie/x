package otelhttpx_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/otelx/otelhttpx"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func ExampleNewTransport_webhookEgress() {
	received := make(chan bool, 1)
	recipient := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := true
		for _, name := range []string{"traceparent", "tracestate", "baggage", "b3", "x-b3-traceid", "x-b3-spanid", "x-b3-parentspanid", "x-b3-sampled", "x-b3-flags"} {
			if len(r.Header.Values(name)) != 0 {
				clean = false
			}
		}
		received <- clean
		w.WriteHeader(http.StatusNoContent)
	}))
	defer recipient.Close()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			panic(err)
		}
	}()
	transport, err := otelhttpx.NewTransport(recipient.Client().Transport, otelhttpx.TransportConfig{
		Operation:  "webhook.deliver",
		Tracer:     provider.Tracer("webhooks"),
		Meter:      noop.NewMeterProvider().Meter("webhooks"),
		Propagator: propagation.NewCompositeTextMapPropagator(),
	})
	if err != nil {
		panic(err)
	}
	// Restore the stored W3C parent, then create fresh outbound headers.
	parent, ok := otelx.ParseTraceparent("00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	if !ok {
		panic("invalid queued parent")
	}
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, recipient.URL, nil)
	if err != nil {
		panic(err)
	}
	response, err := (&http.Client{
		Transport: transport,
	}).Do(request)
	if err != nil {
		panic(err)
	}
	if err := response.Body.Close(); err != nil {
		panic(err)
	}
	spans := recorder.Ended()
	fmt.Println("recipient headers clean:", <-received)
	fmt.Println("queued parent retained:", len(spans) == 1 && spans[0].Parent().SpanID() == parent.SpanID() && spans[0].SpanContext().TraceID() == parent.TraceID())
	// Output:
	// recipient headers clean: true
	// queued parent retained: true
}
