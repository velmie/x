package health_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/velmie/x/svc/http/health"
)

func ExampleCheck() {
	probe := health.Probe{
		Name: "database",
		Check: func(context.Context) error {
			return nil
		},
	}
	report := health.Check(context.Background(), probe)
	fmt.Println(report.Ready, report.Components[0].Name)
	// Output: true database
}

func ExampleCheck_customReason() {
	staleKeys := errors.New("key set is stale")
	probe := health.Probe{
		Name: "key_set",
		Check: func(context.Context) error {
			return staleKeys
		},
		Reason: func(err error) string {
			if errors.Is(err, staleKeys) {
				return "keys_stale"
			}
			return ""
		},
	}
	report := health.Check(context.Background(), probe)
	fmt.Println(report.Ready, report.Components[0].Reason, errors.Is(report.Components[0].Cause, staleKeys))
	// Output: false keys_stale true
}

func ExampleNewHandler() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := health.NewHandler(logger, health.HandlerOptions{})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	fmt.Print(writer.Code, " ", writer.Body.String())
	// Output: 200 {"data":{"ready":true,"components":[]}}
}
