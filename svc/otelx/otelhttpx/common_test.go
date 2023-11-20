package otelhttpx_test

import (
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.uber.org/mock/gomock"

	mock_trace "github.com/velmie/x/svc/otelx/otelhttpx/mock"
)

type mockSpan struct {
	*mock_trace.MockSpan
	embedded.Span
}

func newMockSpan(ctrl *gomock.Controller) *mockSpan {
	return &mockSpan{mock_trace.NewMockSpan(ctrl), nil}
}

func (s *mockSpan) relax() *mockSpan {
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: [16]byte{1, 2, 3, 4}, // Provide a valid TraceID
		SpanID:  [8]byte{5, 6, 7, 8},  // Provide a valid SpanID
	})
	s.EXPECT().SpanContext().Return(spanCtx).AnyTimes()
	return s
}
