package otelx

import (
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/status"
)

const maxDiagnosticRunes = 4096

var diagnosticURL = regexp.MustCompile(`(?i)https?://[^\s"']+`)

var partialSuccessCount = regexp.MustCompile(`\(-?[0-9]+ (?:spans|metric data points) rejected\)$`)

// NewErrorHandler returns an asynchronous SDK error handler using logger. It
// does not install the handler globally. Native OTLP collector response bodies,
// gRPC response descriptions, and URLs are excluded from serialized diagnostics.
// Original errors remain inspectable through errors.Is and errors.As by the
// logging handler. Custom exporters must keep application-specific secrets out
// of their errors; the logging handler is responsible for any further redaction.
func NewErrorHandler(logger *slog.Logger) otel.ErrorHandler {
	return otel.ErrorHandlerFunc(
		func(err error) {
			logger.Error(
				"OpenTelemetry SDK failure",
				slog.String("operation", "telemetry"),
				slog.String("stage", "sdk"),
				slog.Any(
					"error",
					&diagnosticError{
						cause: err,
					},
				),
			)
		},
	)
}

type diagnosticError struct {
	cause error
}

func (e *diagnosticError) Error() string {
	text := safeErrorText(e.cause)
	runes := []rune(text)
	if len(runes) > maxDiagnosticRunes {
		return string(runes[:maxDiagnosticRunes]) + "…"
	}
	return text
}

func (e *diagnosticError) Unwrap() error {
	return e.cause
}

func safeErrorText(err error) string {
	if err == nil {
		return ""
	}
	if rpc, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
		s := rpc.GRPCStatus()
		return "gRPC " + s.Code().String() + grpcDiagnostic(s.Message()) + " (collector response omitted)"
	}
	if network, ok := err.(*url.Error); ok {
		return network.Op + " collector endpoint: " + safeErrorText(network.Err)
	}
	text := err.Error()
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range many.Unwrap() {
			if child != nil {
				text = strings.Replace(text, child.Error(), safeErrorText(child), 1)
			}
		}
	} else if child := errors.Unwrap(err); child != nil {
		text = strings.Replace(text, child.Error(), safeErrorText(child), 1)
	} else if strings.HasPrefix(text, "OTLP partial success:") {
		count := partialSuccessCount.FindString(text)
		if count == "" {
			return "OTLP partial success (collector response omitted)"
		}
		return "OTLP partial success " + count + " (collector response omitted)"
	} else if strings.HasPrefix(text, "body: ") {
		return "collector response body omitted"
	}
	if i := strings.Index(text, " (body: "); i >= 0 {
		text = text[:i] + " (collector response body omitted)"
	}
	return diagnosticURL.ReplaceAllString(text, "[collector endpoint]")
}

// Only fixed facts are retained because gRPC status descriptions may come from
// an untrusted peer and are indistinguishable from local transport messages.
func grpcDiagnostic(message string) string {
	message = strings.ToLower(message)
	for _, fact := range []string{
		"connection refused",
		"no such host",
		"network is unreachable",
		"context deadline exceeded",
		"certificate signed by unknown authority",
		"certificate has expired or is not yet valid",
		"first record does not look like a tls handshake",
	} {
		if strings.Contains(message, fact) {
			return ": " + fact
		}
	}
	return ""
}
