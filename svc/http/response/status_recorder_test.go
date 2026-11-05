package response_test

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/velmie/x/svc/http/response"
)

func TestStatusRecorderForwardsAndTracksStatus(t *testing.T) {
	writer := &observedWriter{}
	recorder := response.NewStatusRecorder(writer)
	if recorder.Unwrap() != writer || recorder.StatusCode() != 0 {
		t.Fatal("new recorder must expose its writer and have no final status")
	}
	recorder.Header().Set("X-Test", "forwarded")
	recorder.WriteHeader(http.StatusEarlyHints)
	if recorder.StatusCode() != 0 {
		t.Fatal("informational status must not commit a response")
	}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusAccepted)
	if recorder.StatusCode() != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.StatusCode())
	}
	if !reflect.DeepEqual(writer.statuses, []int{103, 201, 202}) || writer.Header().Get("X-Test") != "forwarded" {
		t.Fatalf("status/header forwarding failed: %#v, %#v", writer.statuses, writer.Header())
	}
	if n, err := recorder.Write([]byte("hello")); n != 5 || err != nil || string(writer.body) != "hello" {
		t.Fatalf("write = %d, %v, body %q", n, err, writer.body)
	}
	if n, err := recorder.Write([]byte(" now")); n != 4 || err != nil || string(writer.body) != "hello now" {
		t.Fatalf("second write = %d, %v, body %q", n, err, writer.body)
	}
}

func TestStatusRecorderImplicitStatusAndWriteFailure(t *testing.T) {
	failure := errors.New("write failed")
	writer := &observedWriter{
		writeErr: failure,
	}
	recorder := response.NewStatusRecorder(writer)
	if _, err := recorder.Write([]byte("payload")); !errors.Is(err, failure) {
		t.Fatalf("write error = %v", err)
	}
	if recorder.StatusCode() != http.StatusOK || !reflect.DeepEqual(writer.statuses, []int{200}) {
		t.Fatalf("implicit status = %d, forwarded %#v", recorder.StatusCode(), writer.statuses)
	}
}

func TestStatusRecorderSwitchingProtocols(t *testing.T) {
	writer := &observedWriter{}
	recorder := response.NewStatusRecorder(writer)
	recorder.WriteHeader(http.StatusSwitchingProtocols)
	if recorder.StatusCode() != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", recorder.StatusCode())
	}
}

func TestStatusRecorderNestedController(t *testing.T) {
	writer := &observedWriter{}
	inner := response.NewStatusRecorder(writer)
	outer := response.NewStatusRecorder(inner)
	deadline := time.Now().Add(time.Minute)
	if err := http.NewResponseController(outer).SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if !writer.deadline.Equal(deadline) {
		t.Fatalf("deadline = %v", writer.deadline)
	}
	if err := http.NewResponseController(outer).Flush(); err != nil {
		t.Fatal(err)
	}
	if !writer.flushed || outer.StatusCode() != 200 || inner.StatusCode() != 200 {
		t.Fatalf("flush = %v, statuses = %d, %d", writer.flushed, outer.StatusCode(), inner.StatusCode())
	}
}

func TestStatusRecorderFlushFailure(t *testing.T) {
	failure := errors.New("flush failed")
	writer := &observedWriter{
		flushErr: failure,
	}
	recorder := response.NewStatusRecorder(writer)
	if err := recorder.FlushError(); !errors.Is(err, failure) || recorder.StatusCode() != 200 {
		t.Fatalf("flush error = %v, status = %d", err, recorder.StatusCode())
	}
}

func TestStatusRecorderInvalidStatusDoesNotCommitOnTransport(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 1)
	srv := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				recorder := response.NewStatusRecorder(w)
				defer func() {
					if recover() == nil {
						statuses <- -1
						return
					}
					statuses <- recorder.StatusCode()
					recorder.WriteHeader(http.StatusInternalServerError)
				}()
				recorder.WriteHeader(99)
			},
		),
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(
		func() {
			_ = srv.Close()
			<-done
		},
	)
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	res, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		t.Fatal(err)
	}
	if status := <-statuses; status != 0 || res.StatusCode != 500 {
		t.Fatalf("status before recovery = %d, client status = %d", status, res.StatusCode)
	}
}

func ExampleNewStatusRecorder() {
	writer := httptest.NewRecorder()
	recorder := response.NewStatusRecorder(writer)
	_, _ = recorder.Write([]byte("ok"))
	fmt.Println(recorder.StatusCode(), writer.Body.String())
	// Output: 200 ok
}

type observedWriter struct {
	header   http.Header
	statuses []int
	body     []byte
	writeErr error
	flushErr error
	flushed  bool
	deadline time.Time
}

func (w *observedWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *observedWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }

func (w *observedWriter) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	w.body = append(w.body, data...)
	return len(data), nil
}

func (w *observedWriter) FlushError() error {
	w.flushed = true
	return w.flushErr
}

func (w *observedWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
