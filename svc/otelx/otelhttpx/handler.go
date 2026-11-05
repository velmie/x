package otelhttpx

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/velmie/x/svc/http/response"
	"github.com/velmie/x/svc/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HandlerConfig supplies fixed labels and explicit instrumentation dependencies.
type HandlerConfig struct {
	// Operation is the required, fixed operation label and server span name.
	Operation string
	// Route is the required, fixed route template, never an incoming raw URL.
	Route string
	// Tracer creates server spans and is required, including in disabled mode.
	Tracer trace.Tracer
	// Meter creates the request counter and duration histogram and is required.
	Meter metric.Meter
	// Propagator extracts the incoming parent and baggage and is required.
	Propagator propagation.TextMapPropagator
}

// NewHandler records one server span and bounded request metrics around next.
// The operation and route must be fixed labels. Install the handler outside
// recovery to observe aborted responses and exclude operational routes at the
// application's router. Invalid required fields wrap otelx.ErrInvalidConfig;
// instrument-construction errors preserve their causes. Do not install native
// and bounded HTTP instrumentation for the same request.
func NewHandler(next http.Handler, cfg HandlerConfig) (http.Handler, error) {
	if next == nil {
		return nil, fmt.Errorf("next handler is required: %w", otelx.ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.Route) == "" {
		return nil, fmt.Errorf("Route is required: %w", otelx.ErrInvalidConfig)
	}
	if err := validateInstrumentation(cfg.Operation, cfg.Tracer, cfg.Meter, cfg.Propagator); err != nil {
		return nil, err
	}
	instruments, err := newRequestInstruments(cfg.Meter, serverCounterName, serverDurationName)
	if err != nil {
		return nil, err
	}
	return &serverHandler{
		next:        next,
		config:      cfg,
		instruments: instruments,
	}, nil
}

type serverHandler struct {
	next        http.Handler
	config      HandlerConfig
	instruments requestInstruments
}

func (h *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	attrs := []attribute.KeyValue{
		attribute.String(operationAttribute, h.config.Operation),
		attribute.String(routeAttribute, h.config.Route),
		attribute.String(methodAttribute, normalizeMethod(r.Method)),
	}
	ctx := h.config.Propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := h.config.Tracer.Start(
		ctx,
		h.config.Operation,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
	recorder := response.NewStatusRecorder(w)
	completed := false
	defer func() {
		defer span.End()
		status := recorder.StatusCode()
		if status == 0 && completed {
			status = http.StatusOK
		}
		attrs = append(attrs, attribute.Int(statusAttribute, status))
		if !completed {
			attrs = append(attrs, attribute.String(errorAttribute, handlerAborted))
			span.SetStatus(codes.Error, handlerAborted)
		} else if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, serverError)
		}
		span.SetAttributes(attrs...)
		h.instruments.record(ctx, started, attrs)
	}()
	h.next.ServeHTTP(recorder, r.WithContext(ctx))
	completed = true
}
