// Package slogx provides attribute redaction for log/slog handlers.
//
// NewRedactor supplies slog.HandlerOptions.ReplaceAttr for native JSON and
// text handlers. It matches attribute keys and enclosing group names.
//
// Callers own logger configuration and must keep messages, attribute names,
// and unstructured values safe. Redaction does not inspect error text or
// fields inside arbitrary values passed to slog.Any.
package slogx

import (
	"log/slog"
	"strings"
)

const redacted = "[REDACTED]"

// RedactorOptions adds service-specific matches to the default secret keys.
// Its zero value uses only the default keys.
type RedactorOptions struct {
	// AdditionalKeys adds case-insensitive exact key matches. NewRedactor copies this slice.
	AdditionalKeys []string
	// MatchKey adds matches using a lower-case attribute or group key.
	// Nil adds no matches. The function must be safe for concurrent calls.
	MatchKey func(key string) bool
}

// DefaultSecretKeys returns an independent copy of the default secret keys.
func DefaultSecretKeys() []string {
	return []string{
		"access_token",
		"api_key",
		"authorization",
		"client_secret",
		"dsn",
		"password",
		"refresh_token",
		"secret",
		"token",
		"cookie",
		"set-cookie",
	}
}

// NewRedactor returns an attribute rewriter for slog.HandlerOptions.ReplaceAttr.
// A zero RedactorOptions uses only the default secret keys.
// It preserves empty-key attributes and replaces values whose key or enclosing
// group matches with "[REDACTED]", retaining the original key. Other attributes
// are unchanged. The returned function is safe for concurrent calls if MatchKey is.
func NewRedactor(opts RedactorOptions) func(groups []string, attr slog.Attr) slog.Attr {
	keys := make(map[string]struct{}, len(DefaultSecretKeys())+len(opts.AdditionalKeys))
	for _, key := range DefaultSecretKeys() {
		keys[key] = struct{}{}
	}
	for _, key := range opts.AdditionalKeys {
		keys[strings.ToLower(key)] = struct{}{}
	}
	matchKey := opts.MatchKey
	matches := func(key string) bool {
		key = strings.ToLower(key)
		_, found := keys[key]
		return found || (matchKey != nil && matchKey(key))
	}
	return func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == "" {
			return attr
		}
		if matches(attr.Key) {
			return slog.String(attr.Key, redacted)
		}
		for _, group := range groups {
			if matches(group) {
				return slog.String(attr.Key, redacted)
			}
		}
		return attr
	}
}
