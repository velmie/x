package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/velmie/x/svc/slogx"
)

func TestNewHandler(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  slogx.Config
		want string
	}{
		{name: "default", cfg: slogx.Config{}, want: `"level":"INFO"`},
		{name: "case insensitive text", cfg: slogx.Config{Level: "DeBuG", Format: "TeXt"}, want: "level=DEBUG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			handler, err := slogx.NewHandler(&out, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			level := slog.LevelInfo
			if tc.name != "default" {
				level = slog.LevelDebug
			}
			if !handler.Enabled(context.Background(), level) {
				t.Fatal("level not enabled")
			}
			rec := slog.NewRecord(time.Time{}, level, "event", 0)
			rec.AddAttrs(slog.String("password", "secret"), slog.Any("error", errors.New("original cause")))
			if err := handler.Handle(context.Background(), rec); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "[REDACTED]") || !strings.Contains(out.String(), "original cause") || strings.Contains(out.String(), "secret") {
				t.Fatalf("output %q", out.String())
			}
		})
	}
	for _, tc := range []struct {
		cfg   slogx.Config
		field string
		value string
	}{
		{cfg: slogx.Config{Level: "verbose"}, field: "level", value: "verbose"},
		{cfg: slogx.Config{Format: "xml"}, field: "format", value: "xml"},
	} {
		_, err := slogx.NewHandler(&bytes.Buffer{}, tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.field) || !strings.Contains(err.Error(), tc.value) {
			t.Fatalf("error %v", err)
		}
	}
}
