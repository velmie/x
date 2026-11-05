package otelx

import (
	"log/slog"

	"go.opentelemetry.io/otel"
)

// NewErrorHandler returns an asynchronous SDK error handler using logger. It
// passes the original error to the logger without rewriting it and does not
// install the handler globally. The service's logging handler is responsible
// for redaction. Services that need a different summary can implement their own
// otel.ErrorHandler.
func NewErrorHandler(logger *slog.Logger) otel.ErrorHandler {
	return otel.ErrorHandlerFunc(
		func(err error) {
			logger.Error(
				"OpenTelemetry SDK failure",
				slog.String("operation", "telemetry"),
				slog.String("stage", "sdk"),
				slog.Any("error", err),
			)
		},
	)
}
