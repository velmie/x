package otelhttpx

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPHeaderToSpanAttributesHook is a struct that allows for the transformation of
// HTTP header values into span attributes in an OpenTelemetry trace.
type HTTPHeaderToSpanAttributesHook struct {
	headers       []string
	nameFormatter func(string) string
}

// NewHTTPHeaderToSpanAttributesHook creates a new HTTPHeaderToSpanAttributesHook
// with the provided list of headers. These headers are used to extract values
// from the HTTP request and add them as attributes to the span.
func NewHTTPHeaderToSpanAttributesHook(headers []string) *HTTPHeaderToSpanAttributesHook {
	return &HTTPHeaderToSpanAttributesHook{headers: headers}
}

// Execute is called with each HTTP request to extract the specified headers and
// add them as attributes to the given span. If a name formatter is set, it will
// be used to format the header names before adding them as attribute keys.
func (h *HTTPHeaderToSpanAttributesHook) Execute(r *http.Request, span trace.Span) {
	formatter := defaultNameFormatter
	if h.nameFormatter != nil {
		formatter = h.nameFormatter
	}

	attrs := make([]attribute.KeyValue, len(h.headers))
	for i, name := range h.headers {
		attrs[i] = attribute.String(formatter(name), r.Header.Get(name))
	}
	span.SetAttributes(attrs...)
}

// SetNameFormatter sets a custom function for formatting the header names before
// they are added as attribute keys in the span. The formatter function takes a
// header name as input and returns the formatted string.
func (h *HTTPHeaderToSpanAttributesHook) SetNameFormatter(nameFormatter func(hdrName string) string) {
	h.nameFormatter = nameFormatter
}

// defaultNameFormatter is the default function for formatting header names.
// It prefixes each header name with 'http.header.' and converts the name to lower case.
func defaultNameFormatter(name string) string {
	return "http.header." + strings.ToLower(name)
}
