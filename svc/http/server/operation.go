package server

import "context"

const defaultOperation = "handle_http_request"

// WithOperation associates an application-owned operation name with ctx.
// The name should be stable, bounded, and safe to log, not derived from user
// input or a URL path. It applies to requests using the returned context.
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

// Operation returns the associated operation name, or an empty string.
func Operation(ctx context.Context) string {
	operation, _ := ctx.Value(operationKey{}).(string)
	return operation
}

type operationKey struct{}

func logOperation(ctx context.Context) string {
	operation := Operation(ctx)
	if operation == "" {
		return defaultOperation
	}
	return operation
}
