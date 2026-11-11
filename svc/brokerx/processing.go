package brokerx

import (
	"log/slog"

	"github.com/velmie/broker"
)

// ProcessingConfig selects processing instrumentation and error handling.
type ProcessingConfig struct {
	// Trace is optional. When supplied, it must preserve processing results.
	Trace broker.HandlerMiddleware
	// Errors is required, normally HandleErrors or RetryErrors.
	Errors broker.HandlerMiddleware
	// Log configures the broker's native processing diagnostics.
	Log broker.ProcessingLogConfig
}

// WrapProcessing composes tracing, logging, error handling, and panic recovery
// around handler, preserving declared requirements. Handler and logger are
// required. It starts no work.
func WrapProcessing(handler broker.Handler, logger *slog.Logger, cfg ProcessingConfig) broker.Handler {
	middleware := make([]broker.HandlerMiddleware, 0, 4)
	if cfg.Trace.Wrap != nil {
		middleware = append(middleware, cfg.Trace)
	}
	middleware = append(middleware, broker.LogProcessing(logger, cfg.Log), cfg.Errors, broker.RecoverPanics())
	return handler.WithMiddleware(middleware...)
}
