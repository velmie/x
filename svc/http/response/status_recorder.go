package response

import "net/http"

// StatusRecorder observes the final response status without buffering the body.
// Construct it with NewStatusRecorder. It is request-local and is not safe for
// concurrent writes. Use http.ResponseController for supported optional writer
// operations; flushing through the controller also records the implicit status.
type StatusRecorder struct {
	writer http.ResponseWriter
	status int
}

// NewStatusRecorder observes writes to w while forwarding them immediately.
// The writer must be non-nil and remain valid for the life of the recorder.
func NewStatusRecorder(w http.ResponseWriter) *StatusRecorder {
	return &StatusRecorder{
		writer: w,
	}
}

// Header returns the underlying response headers.
func (r *StatusRecorder) Header() http.Header {
	return r.writer.Header()
}

// WriteHeader forwards the status and records the first accepted final status.
// Informational responses do not commit the final status, except for 101.
func (r *StatusRecorder) WriteHeader(status int) {
	r.writer.WriteHeader(status)
	if r.status == 0 && (status >= http.StatusOK || status == http.StatusSwitchingProtocols) {
		r.status = status
	}
}

// Write forwards body data, recording an implicit 200 when necessary.
func (r *StatusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.writer.Write(body)
}

// FlushError commits an implicit 200 and flushes through ResponseController.
func (r *StatusRecorder) FlushError() error {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(r.writer).Flush()
}

// Unwrap returns the underlying writer for ResponseController.
func (r *StatusRecorder) Unwrap() http.ResponseWriter {
	return r.writer
}

// StatusCode returns the final status, or zero before a final status is written.
func (r *StatusRecorder) StatusCode() int {
	return r.status
}
