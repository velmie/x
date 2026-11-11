package otelx

import (
	"log/slog"

	"go.opentelemetry.io/otel"
)

// NewErrorHandler returns an asynchronous SDK error handler using logger. It
// logs errors as received without rewriting them and does not install the
// handler globally. Errors must be made safe at the boundary that creates them.
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
