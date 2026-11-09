package slogx

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Config controls the minimum level and output format of a handler.
type Config struct {
	Level  string `k:"level" default:"info" hint:"Minimum log level" validate:"omitempty,oneof=debug info warn error"`
	Format string `k:"format" default:"json" hint:"Log output format" validate:"omitempty,oneof=json text"`
}

// NewHandler returns a JSON or text handler with the standard secret-key redactor.
// Empty values select info and JSON. Values are case-insensitive.
func NewHandler(w io.Writer, cfg Config) (slog.Handler, error) {
	var level slog.Level
	if cfg.Level != "" {
		if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
			return nil, fmt.Errorf("invalid log level: %w", err)
		}
	}
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: NewRedactor(RedactorOptions{})}
	switch strings.ToLower(cfg.Format) {
	case "", "json":
		return slog.NewJSONHandler(w, opts), nil
	case "text":
		return slog.NewTextHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("invalid log format %q", cfg.Format)
	}
}
