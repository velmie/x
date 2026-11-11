package otelx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCollectorTransportDiscardsSuccessfulBody(t *testing.T) {
	body := &testResponseBody{
		reader: strings.NewReader("collector body"),
	}
	transport := collectorTransport{
		base: testRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil || len(contents) != 0 || !body.closed {
		t.Fatalf("response body not discarded: %q %v", contents, err)
	}
}

func TestCollectorTransportDiscardsSuccessfulBodyFailures(t *testing.T) {
	readErr := errors.New("response read failed")
	closeErr := errors.New("response close failed")
	transport := collectorTransport{
		base: testRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: &testResponseBody{
					readErr:  readErr,
					closeErr: closeErr,
				},
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if !errors.Is(err, readErr) || !errors.Is(err, closeErr) {
		t.Fatalf("lost body errors: %v", err)
	}
}

func TestCollectorTransportOmitsURLCredentials(t *testing.T) {
	transport := collectorTransport{
		base: testRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader("secret body")),
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://user:secret@example.test/v1/traces?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "user") || !strings.Contains(err.Error(), "https://example.test/v1/traces") {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
}

func TestCollectorTransportRejectsAllRedirectStatuses(t *testing.T) {
	for code := 300; code < 400; code++ {
		body := &testResponseBody{
			reader: strings.NewReader("response secret"),
		}
		transport := collectorTransport{
			base: testRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: code,
					Header: http.Header{
						"Location": []string{"https://example.test/?token=secret"},
					},
					Body: body,
				}, nil
			}),
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if err == nil || response != nil || !body.closed || !strings.Contains(err.Error(), strconv.Itoa(code)) || !strings.Contains(err.Error(), "export") {
			t.Fatalf("HTTP %d was not safely rejected", code)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.test") {
			t.Fatalf("HTTP %d leaked redirect information", code)
		}
	}
}

func TestCollectorTransportPreservesReadAndCloseFailures(t *testing.T) {
	readErr := errors.New("response read failed")
	closeErr := errors.New("response close failed")
	transport := collectorTransport{
		base: testRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body: &testResponseBody{
					readErr:  readErr,
					closeErr: closeErr,
				},
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if !errors.Is(err, readErr) || !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("missing response status, read or close cause: %v", err)
	}
}

func TestCollectorRedirectPreservesReadAndCloseFailures(t *testing.T) {
	readErr := errors.New("redirect body read failed")
	closeErr := errors.New("redirect body close failed")
	transport := collectorTransport{
		base: testRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Body: &testResponseBody{
					readErr:  readErr,
					closeErr: closeErr,
				},
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(request)
	if !errors.Is(err, readErr) || !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "307") || !strings.Contains(err.Error(), "export") {
		t.Fatalf("missing redirect status, read or close cause: %v", err)
	}
}

func TestCollectorStatusInterceptorRetainsCodeAndDropsDetails(t *testing.T) {
	st, err := status.New(codes.Unauthenticated, "collector message").WithDetails(&errdetails.ErrorInfo{
		Reason: "collector detail",
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, opts ...grpc.CallOption) error {
		for _, opt := range opts {
			if p, ok := opt.(grpc.PeerCallOption); ok {
				p.PeerAddr.Addr = &net.TCPAddr{}
			}
		}
		return st.Err()
	}
	err = collectorStatusInterceptor(context.Background(), "", nil, nil, nil, invoke)
	got := status.Convert(err)
	if got.Code() != codes.Unauthenticated || got.Message() != codes.Unauthenticated.String() || len(got.Details()) != 0 {
		t.Fatalf("status code, message or details changed incorrectly: %v", got)
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testResponseBody struct {
	reader   *strings.Reader
	readErr  error
	closeErr error
	closed   bool
}

func (b *testResponseBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.reader.Read(p)
}
func (b *testResponseBody) Close() error { b.closed = true; return b.closeErr }
