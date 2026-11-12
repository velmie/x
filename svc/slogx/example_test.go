package slogx_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/velmie/x/svc/slogx"
)

func ExampleNewRedactor() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{}),
	})
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "request", 0)
	record.AddAttrs(slog.String("authorization", "Bearer example"), slog.String("operation", "read"))
	if err := handler.Handle(context.Background(), record); err != nil {
		panic(err)
	}
	// Output: {"level":"INFO","msg":"request","authorization":"[REDACTED]","operation":"read"}
}

func ExampleNewRedactor_extended() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{
			AdditionalKeys: []string{"private_id"},
			MatchKey: func(key string) bool {
				return strings.HasSuffix(key, "_token")
			},
		}),
	})
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "request", 0)
	record.AddAttrs(slog.String("private_id", "123"), slog.String("session_token", "example"))
	if err := handler.Handle(context.Background(), record); err != nil {
		panic(err)
	}
	// Output: {"level":"INFO","msg":"request","private_id":"[REDACTED]","session_token":"[REDACTED]"}
}

func ExampleNewRedactor_group() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{
			AdditionalKeys: []string{"credentials"},
		}),
	})
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "db", 0)
	record.AddAttrs(
		slog.Group("db", slog.String("password", "example"), slog.String("host", "db")),
		slog.Group("credentials", slog.String("user", "example"), slog.String("role", "reader")),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		panic(err)
	}
	// Output: {"level":"INFO","msg":"db","db":{"password":"[REDACTED]","host":"db"},"credentials":{"user":"[REDACTED]","role":"[REDACTED]"}}
}

func ExampleDefaultSecretKeys() {
	fmt.Println(strings.Join(slogx.DefaultSecretKeys(), "\n"))
	// Output:
	// access_token
	// api_key
	// authorization
	// client_secret
	// dsn
	// password
	// refresh_token
	// secret
	// token
	// cookie
	// set-cookie
}

func ExampleNewHandler() {
	handler, err := slogx.NewHandler(os.Stdout, slogx.Config{
		Format: "TeXt",
	})
	if err != nil {
		panic(err)
	}
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "ready", 0)
	record.AddAttrs(slog.String("password", "secret"))
	if err := handler.Handle(context.Background(), record); err != nil {
		panic(err)
	}
	// Output: level=INFO msg=ready password=[REDACTED]
}
