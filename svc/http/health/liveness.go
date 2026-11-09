package health

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/velmie/x/svc/http/response"
)

// NewLivenessHandler returns an independent GET/HEAD liveness handler.
// It does not inspect dependencies or start workers.
func NewLivenessHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			err := response.WriteErrors(w, r, http.StatusMethodNotAllowed, response.Error(&response.HTTPError{
				Code:   response.ErrCodeMethodNotAllowed,
				Title:  "Method not allowed.",
				Target: response.TargetCommon,
			}))
			logLivenessFailure(r, logger, err)
			return
		}
		w.Header().Set("Content-Type", jsonMediaType)
		if r.Method == http.MethodGet {
			body := struct {
				Status string `json:"status"`
			}{Status: "ok"}
			logLivenessFailure(r, logger, json.NewEncoder(w).Encode(response.OK(body)))
		}
	})
}

func logLivenessFailure(r *http.Request, logger *slog.Logger, err error) {
	if err != nil {
		logger.ErrorContext(r.Context(), "Health response failed",
			slog.String("operation", "health"),
			slog.String("stage", "encode"),
			slog.String("outcome", "delivery_failed"),
			slog.Any("error", err),
		)
	}
}
