package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/velmie/x/svc/http/response"
)

const (
	jsonMediaType             = "application/json"
	operationReadiness        = "readiness"
	stageProbe                = "probe"
	stageEncode               = "encode"
	outcomeUnavailable        = "unavailable"
	outcomeDeliveryFailed     = "delivery_failed"
	reasonResponseWriteFailed = "response_write_failed"
)

// HandlerOptions configures the total readiness sampling budget.
type HandlerOptions struct {
	// Timeout bounds the complete sample; zero selects DefaultHandlerTimeout.
	// It must be nonnegative. Executing probes must cooperate with cancellation.
	Timeout time.Duration
}

// NewHandler returns a GET/HEAD readiness handler that logs failed probe causes.
// It snapshots the supplied probe configurations. The logger and probe checks
// are required and must be safe for concurrent requests. Logger handlers must
// redact sensitive information in errors while retaining useful diagnostic
// causes. Names and custom reasons are application-owned safe public values.
//
// GET and HEAD both execute the complete sample and return 200 or 503. HEAD
// has no body. Other methods return 405 with Allow: GET, HEAD without probing.
// All responses use JSON and Cache-Control: no-store. The handler logs each
// failed component once at WARN and response write failures at ERROR. A probe
// panic propagates to surrounding recovery middleware.
func NewHandler(logger *slog.Logger, opts HandlerOptions, probes ...Probe) http.Handler {
	selected := append([]Probe(nil), probes...)
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				err := response.WriteErrors(
					w,
					r,
					http.StatusMethodNotAllowed,
					response.Error(
						&response.HTTPError{
							Code:   response.ErrCodeMethodNotAllowed,
							Title:  "Method not allowed.",
							Target: response.TargetCommon,
						},
					),
				)
				logDeliveryFailure(r.Context(), logger, err)
				return
			}
			timeout := opts.Timeout
			if timeout == 0 {
				timeout = DefaultHandlerTimeout
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			report := Check(ctx, selected...)
			for _, component := range report.Components {
				if !component.Ready {
					logger.LogAttrs(
						r.Context(),
						slog.LevelWarn,
						"Dependency readiness failed",
						slog.String("operation", operationReadiness),
						slog.String("stage", stageProbe),
						slog.String("component", component.Name),
						slog.String("reason", component.Reason),
						slog.String("outcome", outcomeUnavailable),
						slog.Any("error", component.Cause),
					)
				}
			}
			status := http.StatusOK
			if !report.Ready {
				status = http.StatusServiceUnavailable
			}
			w.Header().Set("Content-Type", jsonMediaType)
			w.WriteHeader(status)
			if r.Method != http.MethodHead {
				logDeliveryFailure(r.Context(), logger, json.NewEncoder(w).Encode(response.OK(report)))
			}
		},
	)
}

func logDeliveryFailure(ctx context.Context, logger *slog.Logger, err error) {
	if err == nil {
		return
	}
	logger.LogAttrs(
		ctx,
		slog.LevelError,
		"Readiness response failed",
		slog.String("operation", operationReadiness),
		slog.String("stage", stageEncode),
		slog.String("reason", reasonResponseWriteFailed),
		slog.String("outcome", outcomeDeliveryFailed),
		slog.Any("error", err),
	)
}
