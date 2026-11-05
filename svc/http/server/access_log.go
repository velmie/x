package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/velmie/x/svc/http/response"
)

// AccessLogOptions configures additional request-log attributes.
type AccessLogOptions struct {
	// Attributes optionally returns bounded, safe, service-owned attributes.
	// It must not read the body or reuse time, level, msg, source, operation,
	// component, stage, reason, outcome, method, status, or duration_ms. The caller owns attribute redaction.
	Attributes func(r *http.Request) []slog.Attr
}

// AccessLog records the selected response status, duration, and outcome at INFO.
// A completed outcome means the handler returned, not that it succeeded or the
// client received every byte. An abort before a final status records status 0;
// an ordinary empty return records 200. Unknown methods are recorded as OTHER.
// It does not automatically log URLs, queries, headers, bodies, or client
// addresses. The required logger must safely serialize application attributes.
func AccessLog(logger *slog.Logger, next http.Handler, opts AccessLogOptions) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := response.NewStatusRecorder(w)
			completed := false
			defer func() {
				status := recorder.StatusCode()
				outcome := outcomeAborted
				if completed {
					outcome = outcomeCompleted
					if status == 0 {
						status = http.StatusOK
					}
				}
				attrs := []slog.Attr{
					slog.String(keyOperation, logOperation(r.Context())),
					slog.String(keyComponent, componentHTTPServer),
					slog.String(keyStage, stageHandle),
					slog.String(keyOutcome, outcome),
					slog.String("method", knownMethod(r.Method)),
					slog.Int("status", status),
					slog.Int64("duration_ms", time.Since(started).Milliseconds()),
				}
				if opts.Attributes != nil {
					attrs = append(attrs, opts.Attributes(r)...)
				}
				logger.LogAttrs(
					r.Context(),
					slog.LevelInfo,
					"HTTP request",
					attrs...,
				)
			}()
			next.ServeHTTP(recorder, r)
			completed = true
		},
	)
}

func knownMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}
