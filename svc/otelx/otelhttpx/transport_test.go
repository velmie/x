package otelhttpx_test

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"

	"github.com/velmie/x/svc/otelx/otelhttpx"
	mock_trace "github.com/velmie/x/svc/otelx/otelhttpx/mock"
)

// mockRoundTripper is a mock http.RoundTripper for testing purposes.
type mockRoundTripper struct {
	requests []*http.Request
}

func (m *mockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, r)
	return &http.Response{}, nil
}

func TestSpanHookRoundTripper(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	span := newMockSpan(ctrl).relax()

	mockTripper := &mockRoundTripper{}
	sh := otelhttpx.NewSpanHookRoundTripper(mockTripper)

	hook := mock_trace.NewMockSpanHook(ctrl)
	hook.EXPECT().Execute(gomock.Any(), gomock.Any())
	sh.AddHook(hook)

	ctx := trace.ContextWithSpan(context.Background(), span)

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://example.com", nil)

	_, _ = sh.RoundTrip(req)
}
