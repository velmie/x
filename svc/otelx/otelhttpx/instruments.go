package otelhttpx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	serverCounterName  = "http.server.requests"
	serverDurationName = "http.server.request.duration"
	clientCounterName  = "http.client.requests"
	clientDurationName = "http.client.request.duration"

	operationAttribute = "operation"
	routeAttribute     = "http.route"
	methodAttribute    = "http.request.method"
	statusAttribute    = "http.response.status_code"
	errorAttribute     = "error.type"

	handlerAborted  = "handler_aborted"
	serverError     = "server_error"
	transportFailed = "transport_failed"
	remoteError     = "remote_error"
)

type requestInstruments struct {
	count    metric.Int64Counter
	duration metric.Float64Histogram
}

func newRequestInstruments(meter metric.Meter, countName string, durationName string) (requestInstruments, error) {
	count, err := meter.Int64Counter(countName, metric.WithUnit("{request}"))
	if err != nil {
		return requestInstruments{}, fmt.Errorf("create %s counter: %w", countName, err)
	}
	duration, err := meter.Float64Histogram(durationName, metric.WithUnit("s"))
	if err != nil {
		return requestInstruments{}, fmt.Errorf("create %s histogram: %w", durationName, err)
	}
	return requestInstruments{
		count:    count,
		duration: duration,
	}, nil
}

func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

func validateInstrumentation(
	operation string,
	tracer trace.Tracer,
	meter metric.Meter,
	propagator propagation.TextMapPropagator,
) error {
	switch {
	case strings.TrimSpace(operation) == "":
		return fmt.Errorf("Operation is required: %w", otelx.ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	case tracer == nil:
		return fmt.Errorf("Tracer is required: %w", otelx.ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	case meter == nil:
		return fmt.Errorf("Meter is required: %w", otelx.ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	case propagator == nil:
		return fmt.Errorf("Propagator is required: %w", otelx.ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	default:
		return nil
	}
}

func (i requestInstruments) record(ctx context.Context, started time.Time, attrs []attribute.KeyValue) {
	i.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	i.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
}
