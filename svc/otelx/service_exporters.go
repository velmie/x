package otelx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// collectorTransport removes collector error bodies and rejects redirects before
// http.Client or the exporter can include collector data in an error. The
// runtime owns the underlying transport.
type collectorTransport struct {
	base http.RoundTripper
}

func (t collectorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return response, err
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	response.Body = http.NoBody
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		redirectErr := fmt.Errorf("export telemetry: collector HTTP %d redirect rejected", response.StatusCode)
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(redirectErr, fmt.Errorf("discard collector response body: %w", errors.Join(readErr, closeErr)))
		}
		return nil, redirectErr
	}
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("discard collector HTTP %d response body: %w", response.StatusCode, errors.Join(readErr, closeErr))
	}
	return response, nil
}

// collectorStatusInterceptor excludes collector-provided status messages and
// details while retaining the gRPC status code.
func collectorStatusInterceptor(
	ctx context.Context,
	method string,
	req any,
	reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err == nil {
		return nil
	}
	code := status.Code(err)
	return status.Error(code, code.String())
}
