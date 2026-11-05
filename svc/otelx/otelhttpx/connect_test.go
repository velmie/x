package otelhttpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/velmie/x/svc/otelx"
	"github.com/velmie/x/svc/otelx/otelhttpx"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestBoundedHandlerWithRealConnectUnary(t *testing.T) {
	cases := []struct {
		name       string
		code       connect.Code
		wantStatus int64
		wantError  bool
	}{
		{
			name:       "success",
			wantStatus: 200,
		},
		{
			name:       "invalid argument",
			code:       connect.CodeInvalidArgument,
			wantStatus: 400,
		},
		{
			name:       "internal",
			code:       connect.CodeInternal,
			wantStatus: 500,
			wantError:  true,
		},
	}
	for _, tc := range cases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				spans, tracer, reader, meter := profileSignals(t)
				policy, err := otelx.NewPropagation(otelx.PropagationPolicy{})
				if err != nil {
					t.Fatal(err)
				}
				const route = "/example.EchoService/Echo"
				service := connect.NewUnaryHandler(
					route,
					func(_ context.Context, request *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
						if tc.code != 0 {
							return nil, connect.NewError(tc.code, errors.New("private detail"))
						}
						return connect.NewResponse(wrapperspb.String(request.Msg.Value)), nil
					},
				)
				handler, err := otelhttpx.NewHandler(
					service,
					otelhttpx.HandlerConfig{
						Operation:  "echo",
						Route:      route,
						Tracer:     tracer,
						Meter:      meter,
						Propagator: policy,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan struct{})
				server := httptest.NewServer(
					http.HandlerFunc(
						func(w http.ResponseWriter, r *http.Request) {
							defer close(done)
							handler.ServeHTTP(w, r)
						},
					),
				)
				defer server.Close()
				client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](server.Client(), server.URL+route)
				_, err = client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hello")))
				if (err != nil) != (tc.code != 0) {
					t.Fatalf("unexpected Connect error: %v", err)
				}
				<-done
				if ended := spans.Ended(); len(ended) != 1 || (ended[0].Status().Code == codes.Error) != tc.wantError {
					t.Fatalf("unexpected Connect span: %v", ended)
				}
				assertProfileMetrics(t, reader, "http.server", tc.wantStatus, "")
			},
		)
	}
}
