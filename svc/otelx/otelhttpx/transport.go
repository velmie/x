package otelhttpx

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

// NewSpanHookRoundTripper creates a new SpanHookRoundTripper.
// It takes a base http.RoundTripper to which the actual network round-trip
// will be delegated.
func NewSpanHookRoundTripper(base http.RoundTripper) *SpanHookRoundTripper {
	return &SpanHookRoundTripper{base: base}
}

// RoundTrip executes a single HTTP transaction, returning
// a Response for the provided Request.
// If the request's context includes a valid OpenTelemetry span,
// RoundTrip will execute all registered SpanHooks on that span before
// delegating the round-trip to the base RoundTripper.
func (h *SpanHookRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if span := trace.SpanFromContext(r.Context()); span.SpanContext().IsValid() {
		for _, hook := range h.hooks {
			hook.Execute(r, span)
		}
	}
	return h.base.RoundTrip(r)
}

// AddHook adds a new SpanHook to the SpanHookRoundTripper.
// The added hook will be executed on each outgoing HTTP request.
func (h *SpanHookRoundTripper) AddHook(hook SpanHook) {
	h.hooks = append(h.hooks, hook)
}
