package otelhttpx

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// AttributesGetter is an interface that abstracts the process of extracting
// attributes from an HTTP request. Implementers of this interface
// provide a mechanism to extract relevant telemetry information
// as key-value pairs from an HTTP request.
type AttributesGetter interface {
	// Attributes extracts telemetry attributes from the provided HTTP request.
	// The returned slice of KeyValue pairs is intended to be used for
	// enriching OpenTelemetry spans with HTTP request-related information.
	Attributes(r *http.Request) []attribute.KeyValue
}

// AttributesHook is a struct that holds a reference to an implementation
// of the AttributesGetter interface. It is used to execute the attribute
// extraction process and apply these attributes to a given OpenTelemetry span.
type AttributesHook []AttributesGetter

func (h AttributesHook) Execute(r *http.Request, span trace.Span) {
	for _, g := range h {
		span.SetAttributes(g.Attributes(r)...)
	}
}

type AttributesGetterFunc func(r *http.Request) []attribute.KeyValue

func (g AttributesGetterFunc) Attributes(r *http.Request) []attribute.KeyValue {
	return g(r)
}
