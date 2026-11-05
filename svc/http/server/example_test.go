package server_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/velmie/x/svc/http/server"
)

func ExampleServe() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// A real service cancels ctx on shutdown, then closes its dependencies
	// after Serve returns and logs any returned failure at its boundary.
	cancel()
	err = server.Serve(ctx, srv, listener, 5*time.Second)
	fmt.Println(err == nil)
	// Output: true
}

func ExampleRecover() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := server.Recover(
		logger,
		http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) {
				panic("internal failure")
			},
		),
		server.RecoveryOptions{},
	)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(writer.Code, writer.Header().Get("Content-Type"))
	// Output: 500 application/json
}

func ExampleAccessLog() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := server.AccessLog(
		logger,
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		),
		server.AccessLogOptions{},
	)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(writer.Code)
	// Output: 204
}

func ExampleWithOperation() {
	ctx := server.WithOperation(context.Background(), "list_orders")
	fmt.Println(server.Operation(ctx))
	// Output: list_orders
}
