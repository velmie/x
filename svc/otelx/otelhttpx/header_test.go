package otelhttpx_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/mock/gomock"

	"github.com/velmie/x/svc/otelx/otelhttpx"
)

func TestHTTPHeaderToSpanAttributesHook(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	span := newMockSpan(ctrl)

	headers := []string{
		"X-Test-Header1",
		"X-Test-Header2",
	}

	r, _ := http.NewRequest("", "", http.NoBody)
	r.Header.Set(headers[0], "some-value")
	// the header second is not set, but expected to be added to attributes as empty value

	hook := otelhttpx.NewHTTPHeaderToSpanAttributesHook(headers)
	hook.SetNameFormatter(func(name string) string {
		return name
	})

	span.EXPECT().SetAttributes(gomock.Any(), gomock.Any()).Do(func(attrs ...attribute.KeyValue) {
		require.Len(t, attrs, 2)
		require.Equal(t, string(attrs[0].Key), headers[0])
		require.Equal(t, attrs[0].Value.AsString(), "some-value")
		require.Equal(t, string(attrs[1].Key), headers[1])
		require.Equal(t, attrs[1].Value.AsString(), "")
	})

	hook.Execute(r, span)
}
