package slogx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/slogx"
)

func TestRedactorKeys(t *testing.T) {
	defaults := []string{
		"access_token", "api_key", "authorization", "client_secret", "dsn", "password",
		"refresh_token", "secret", "token", "cookie", "set-cookie",
	}
	if got := slogx.DefaultSecretKeys(); !reflect.DeepEqual(got, defaults) {
		t.Fatalf("defaults = %v, want %v", got, defaults)
	}
	redact := slogx.NewRedactor(slogx.RedactorOptions{
		AdditionalKeys: []string{"Private_ID"},
		MatchKey: func(key string) bool {
			return strings.HasSuffix(key, "_token")
		},
	})
	for _, key := range append(defaults, "PASSWORD", "pRiVaTe_Id", "Session_TOKEN") {
		t.Run(key, func(t *testing.T) {
			got := redact(nil, slog.String(key, "sensitive"))
			if want := slog.String(key, "[REDACTED]"); !got.Equal(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
	for _, group := range []string{"PRIVATE_ID", "Session_TOKEN"} {
		if got := redact([]string{group}, slog.String("child", "sensitive")); !got.Equal(slog.String("child", "[REDACTED]")) {
			t.Errorf("extended group %q: got %v", group, got)
		}
	}
	for _, key := range []string{"password_hint", "secretary", "session"} {
		attr := slog.String(key, "visible")
		if got := redact(nil, attr); !got.Equal(attr) {
			t.Errorf("unmatched %q: got %v, want %v", key, got, attr)
		}
	}
}

func TestHandlerRedaction(t *testing.T) {
	for _, tc := range []struct {
		name       string
		newHandler func(*bytes.Buffer, *slog.HandlerOptions) slog.Handler
		wantGroup  string
		wantAttr   string
	}{
		{
			name: "json",
			newHandler: func(b *bytes.Buffer, opts *slog.HandlerOptions) slog.Handler {
				return slog.NewJSONHandler(b, opts)
			},
			wantGroup: `{"level":"INFO","msg":"event","before":"visible",` +
				`"Authorization":{"attached":"[REDACTED]","nested":{"leaf":"[REDACTED]"}}}` + "\n",
			wantAttr: `{"level":"INFO","msg":"event","request":{"cookie":{"item":"[REDACTED]"},` +
				`"access_token":"[REDACTED]","error":"dependency detail"}}` + "\n",
		},
		{
			name: "text",
			newHandler: func(b *bytes.Buffer, opts *slog.HandlerOptions) slog.Handler {
				return slog.NewTextHandler(b, opts)
			},
			wantGroup: "level=INFO msg=event before=visible Authorization.attached=[REDACTED] " +
				"Authorization.nested.leaf=[REDACTED]\n",
			wantAttr: "level=INFO msg=event request.cookie.item=[REDACTED] " +
				"request.access_token=[REDACTED] request.error=\"dependency detail\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			handler := tc.newHandler(&out, &slog.HandlerOptions{
				ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{}),
			})
			handler = handler.WithAttrs([]slog.Attr{
				slog.String("before", "visible"),
			})
			handler = handler.WithGroup("Authorization").WithAttrs([]slog.Attr{
				slog.String("attached", "private"),
			})
			handler = handler.WithGroup("nested")
			record := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
			record.AddAttrs(slog.String("leaf", "private"))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.wantGroup {
				t.Errorf("WithGroup got %q, want %q", got, tc.wantGroup)
			}
			var second bytes.Buffer
			handler = tc.newHandler(&second, &slog.HandlerOptions{
				ReplaceAttr: slogx.NewRedactor(slogx.RedactorOptions{}),
			})
			record = slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
			resolved := false
			record.AddAttrs(slog.Group("request",
				slog.Group("cookie", slog.String("item", "private")),
				slog.Any("access_token", secretValue{resolved: &resolved}), slog.Any("error", errors.New("dependency detail")),
			))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if !resolved {
				t.Error("LogValuer was not resolved")
			}
			if got := second.String(); got != tc.wantAttr {
				t.Errorf("Group got %q, want %q", got, tc.wantAttr)
			}
		})
	}
}

type secretValue struct {
	resolved *bool
}

func (value secretValue) LogValue() slog.Value {
	*value.resolved = true
	return slog.StringValue("private")
}

func TestPreservationAndCopies(t *testing.T) {
	keys := []string{"Custom"}
	redact := slogx.NewRedactor(slogx.RedactorOptions{
		AdditionalKeys: keys,
	})
	keys[0] = "changed"
	if got := redact(nil, slog.String("custom", "secret")); !got.Equal(slog.String("custom", "[REDACTED]")) {
		t.Fatalf("copied options: %v", got)
	}
	if got := redact(nil, slog.String("changed", "visible")); !got.Equal(slog.String("changed", "visible")) {
		t.Fatalf("options changed after construction: %v", got)
	}
	first := slogx.DefaultSecretKeys()
	first[0] = "changed"
	if second := slogx.DefaultSecretKeys(); second[0] != "access_token" {
		t.Fatalf("mutated defaults: %v", second)
	}
	groups := []string{"password", "nested"}
	before := append([]string(nil), groups...)
	zero := slog.Attr{}
	if got := redact(groups, zero); !got.Equal(zero) {
		t.Fatalf("zero attr changed: %v", got)
	}
	if got := redact(groups, slog.Int("", 7)); !got.Equal(slog.Int("", 7)) {
		t.Fatalf("empty key changed: %v", got)
	}
	if !reflect.DeepEqual(groups, before) {
		t.Fatalf("groups modified: %v", groups)
	}
	cause := errors.New("dependency detail")
	attr := slog.Any("error", cause)
	if got := redact(nil, attr); !got.Equal(attr) || got.Value.Any() != cause {
		t.Fatalf("error value changed: %v", got)
	}
}

func TestConcurrentRedaction(t *testing.T) {
	redact := slogx.NewRedactor(slogx.RedactorOptions{
		AdditionalKeys: []string{"private"},
		MatchKey: func(key string) bool {
			return strings.HasSuffix(key, "_token")
		},
	})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got := redact([]string{"private"}, slog.Int("count", j))
				if !got.Equal(slog.String("count", "[REDACTED]")) {
					t.Errorf("unexpected result: %v", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}
