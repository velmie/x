package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/velmie/x/svc/http/response"
)

// OperationalRoutes contains the handlers and identity for an operational listener.
// Readiness is required. Metrics may be nil, which leaves /metrics unregistered.
type OperationalRoutes struct {
	Readiness http.Handler
	Build     any
	Metrics   http.Handler
}

// RegisterOperationalRoutes registers /readyz, /build and optional /metrics.
// GET and HEAD are supported. Other methods return 405 without invoking handlers.
// The caller owns the listener, middleware and lifecycle.
func RegisterOperationalRoutes(mux *http.ServeMux, logger *slog.Logger, routes OperationalRoutes) {
	mux.Handle("/readyz", operationalMethods(logger, routes.Readiness))
	mux.Handle("/build", operationalMethods(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			if err := json.NewEncoder(w).Encode(response.OK(routes.Build)); err != nil {
				logger.ErrorContext(r.Context(), "Operational response failed",
					slog.String("operation", "build"),
					slog.String("stage", "encode"),
					slog.String("outcome", "failed"),
					slog.Any("error", err),
				)
			}
		}
	})))
	if routes.Metrics != nil {
		mux.Handle("/metrics", operationalMethods(logger, routes.Metrics))
	}
}

func operationalMethods(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", "GET, HEAD")
		err := response.WriteErrors(w, r, http.StatusMethodNotAllowed, response.Error(&response.HTTPError{
			Code:   response.ErrCodeMethodNotAllowed,
			Title:  "Method not allowed.",
			Target: response.TargetCommon,
		}))
		if err != nil {
			logger.ErrorContext(r.Context(), "Operational response failed",
				slog.String("operation", "routing"),
				slog.String("stage", "encode"),
				slog.String("outcome", "failed"),
				slog.Any("error", err),
			)
		}
	})
}
