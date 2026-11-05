// Package otelslog adds span-context correlation to native slog handlers.
// It does not export OpenTelemetry logs or configure logging output.
package otelslog

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const (
	traceIDKey = "trace_id"
	spanIDKey  = "span_id"
)

// NewHandler removes caller-supplied correlation attributes and adds valid
// context IDs to the active slog group. next must be non-nil. Valid unsampled
// contexts are included.
// IDs are correlation metadata, not authenticated identity. Wrap the base
// handler before deriving loggers with attributes or groups: attributes already
// bound inside an opaque handler cannot be removed. Secret-key redaction remains
// the responsibility of the underlying handler.
func NewHandler(next slog.Handler) slog.Handler {
	return &handler{
		next: next,
	}
}

type handler struct {
	next slog.Handler
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(
		func(attr slog.Attr) bool {
			if attr, keep := cleanAttribute(attr); keep {
				clean.AddAttrs(attr)
			}
			return true
		},
	)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		clean.AddAttrs(
			slog.String(traceIDKey, sc.TraceID().String()),
			slog.String(spanIDKey, sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, clean)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if attr, keep := cleanAttribute(attr); keep {
			clean = append(clean, attr)
		}
	}
	return &handler{
		next: h.next.WithAttrs(clean),
	}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if reservedKey(name) {
		return h
	}
	return &handler{
		next: h.next.WithGroup(name),
	}
}

func cleanAttribute(attr slog.Attr) (slog.Attr, bool) {
	if reservedKey(attr.Key) {
		return slog.Attr{}, false
	}
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		original := attr.Value.Group()
		clean := make([]slog.Attr, 0, len(original))
		for _, child := range original {
			if child, keep := cleanAttribute(child); keep {
				clean = append(clean, child)
			}
		}
		attr.Value = slog.GroupValue(clean...)
	}
	return attr, true
}

func reservedKey(key string) bool {
	return strings.EqualFold(key, traceIDKey) || strings.EqualFold(key, spanIDKey)
}
