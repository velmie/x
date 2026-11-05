# HTTP service components

## Response

The `response` package provides organization response envelopes and an
application-error mapper. `response.NewStatusRecorder(w)` observes the final
status without buffering the response body. It forwards informational responses,
records an implicit 200 on writing or flushing, and exposes the underlying writer
to `http.ResponseController`. A recorder is request-local and must not be used
for concurrent writes. See [response](response/readme.md) for error envelopes.
