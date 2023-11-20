package otelhttpx

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

//go:generate go run go.uber.org/mock/mockgen@v0.3.0 -source hook.go -destination ./mock/hook.go

// SpanHook is an interface defining a hook that allows custom operations to be
// performed on a trace span in the context of an HTTP request.
//
// Implementations of this interface can be used to modify, annotate, or
// extract information from a trace span based on the details of an incoming
// HTTP request. This can be particularly useful for adding custom metadata
// to spans, handling request-specific logging, or performing other
// request-dependent operations within the span's lifecycle.
//
// The Execute method is the primary method of this interface. It takes an HTTP
// request and a trace span as arguments. Implementors of this interface are
// expected to define the custom logic within this method to interact with
// the span based on the HTTP request's context.
//
// Example:
//
//	type MySpanHook struct {
//	    // ... custom fields ...
//	}
//
//	func (h *MySpanHook) Execute(r *http.Request, span trace.Span) {
//	    // Custom logic to modify the span based on the HTTP request
//	    // ...
//	}
type SpanHook interface {
	// Execute takes an HTTP request and a trace span,
	// allowing custom operations on the span based on the request details.
	Execute(r *http.Request, span trace.Span)
}

// SpanHookFunc is a function type that defines a hook for performing custom operations
// on a trace span given an HTTP request. It adheres to the SpanHook interface.
//
// This type allows the use of ordinary functions as SpanHook handlers. A function
// with the appropriate signature can be converted to a SpanHookFunc, enabling it to
// be used wherever a SpanHook is required without the need to define a separate struct
// implementing the interface.
//
// Example usage:
//
//	var myHook SpanHookFunc = func(r *http.Request, span trace.Span) {
//	    // Custom logic to modify the span based on the HTTP request
//	    // ...
//	}
type SpanHookFunc func(r *http.Request, span trace.Span)

// Execute takes an HTTP request and a trace span,
// allowing custom operations on the span based on the request details.
func (f SpanHookFunc) Execute(r *http.Request, span trace.Span) {
	f(r, span)
}

// SpanHookRoundTripper is an implementation of http.RoundTripper that allows
// attaching SpanHooks to be executed with each HTTP request.
type SpanHookRoundTripper struct {
	base  http.RoundTripper // base is the underlying HTTP transport.
	hooks []SpanHook        // hooks are the SpanHooks to be executed.
}
