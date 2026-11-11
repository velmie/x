package otelx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// collectorTransport discards collector bodies and rejects redirects before
// http.Client or the exporter can include collector data in an error. The
// runtime owns the underlying transport.
type collectorTransport struct {
	base http.RoundTripper
}

func (t collectorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	response.Body = http.NoBody
	discardErr := errors.Join(readErr, closeErr)
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		redirectErr := fmt.Errorf("export telemetry: collector HTTP %d redirect rejected", response.StatusCode)
		if discardErr != nil {
			return nil, errors.Join(redirectErr, fmt.Errorf("discard collector response body: %w", discardErr))
		}
		return nil, redirectErr
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		endpoint := url.URL{
			Scheme: request.URL.Scheme,
			Host:   request.URL.Host,
			Path:   request.URL.Path,
		}
		statusErr := fmt.Errorf("export telemetry to collector %s: HTTP %d", endpoint.String(), response.StatusCode)
		if discardErr != nil {
			return nil, errors.Join(statusErr, fmt.Errorf("discard collector response body: %w", discardErr))
		}
		return nil, statusErr
	}
	if discardErr != nil {
		return nil, fmt.Errorf("discard collector response body: %w", discardErr)
	}
	return response, nil
}

// collectorStatusInterceptor removes collector messages while preserving local
// connection errors and the rejected span count in successful responses.
func collectorStatusInterceptor(
	ctx context.Context,
	method string,
	req any,
	reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	var remote peer.Peer
	err := invoker(ctx, method, req, reply, cc, append(opts, grpc.Peer(&remote))...)
	if err == nil {
		if response, ok := reply.(*collectortrace.ExportTraceServiceResponse); ok && response.PartialSuccess != nil {
			response.PartialSuccess.ErrorMessage = ""
		}
		return nil
	}
	if remote.Addr == nil {
		return err
	}
	code := status.Code(err)
	return status.Error(code, code.String())
}
