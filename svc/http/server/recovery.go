package server

import (
	"log/slog"
	"net/http"
	"runtime"

	"github.com/velmie/x/svc/http/response"
)

const maxPanicStack = 64 * 1024

// PanicResponder writes a protocol-specific internal failure response. If it
// returns false, it must leave the response untouched and return a nil error.
// If it returns true with an error, Recover logs that error and does not retry.
type PanicResponder func(w http.ResponseWriter, r *http.Request) (handled bool, err error)

// RecoveryOptions configures protocol-specific panic responses.
type RecoveryOptions struct {
	// Responder optionally handles a request before the default JSON response.
	Responder PanicResponder
}

// Recover logs recovered panics and replaces uncommitted responses or aborts
// delivery. It re-panics http.ErrAbortHandler without logging. The required
// logger receives the original panic object and a bounded current-goroutine
// stack. Its handler must redact sensitive values while retaining useful causes.
// Before a final status, pending headers are cleared and Responder is called,
// or the standard 500 JSON error envelope is written. After a final status,
// Recover logs once and panics with http.ErrAbortHandler to abort delivery.
func Recover(logger *slog.Logger, next http.Handler, opts RecoveryOptions) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			recorder := response.NewStatusRecorder(w)
			defer func() {
				value := recover()
				if value == nil {
					return
				}
				if value == http.ErrAbortHandler {
					panic(http.ErrAbortHandler)
				}
				stack := make([]byte, maxPanicStack)
				stack = stack[:runtime.Stack(stack, false)]
				outcome := outcomeFailed
				if recorder.StatusCode() != 0 {
					outcome = outcomeAborted
				}
				attrs := []slog.Attr{
					slog.String(keyOperation, logOperation(r.Context())),
					slog.String(keyComponent, componentHTTPServer),
					slog.String(keyStage, stageHandle),
					slog.String(keyReason, reasonHandlerPanic),
					slog.String(keyOutcome, outcome),
					slog.Any("panic", value),
					slog.String("stack", string(stack)),
				}
				if recorder.StatusCode() != 0 {
					logger.LogAttrs(
						r.Context(),
						slog.LevelError,
						"HTTP handler failed",
						attrs...,
					)
					panic(http.ErrAbortHandler)
				}
				clear(recorder.Header())
				handled := false
				var responseErr error
				if opts.Responder != nil {
					handled, responseErr = opts.Responder(recorder, r)
				}
				if !handled && responseErr == nil {
					responseErr = response.WriteErrors(
						recorder,
						r,
						http.StatusInternalServerError,
						response.Error(
							&response.HTTPError{
								Code:   response.ErrCodeInternalServerError,
								Title:  "Could not complete request.",
								Target: response.TargetCommon,
							},
						),
					)
				}
				if responseErr != nil {
					attrs = append(attrs, slog.Any("response_error", responseErr))
				}
				logger.LogAttrs(
					r.Context(),
					slog.LevelError,
					"HTTP handler failed",
					attrs...,
				)
			}()
			next.ServeHTTP(recorder, r)
		},
	)
}
