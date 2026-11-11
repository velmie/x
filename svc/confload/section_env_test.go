package confload

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionEnvPrefix(t *testing.T) {
	type section struct {
		Headers map[string]string `k:"headers"`
		Name    string            `k:"name"`
	}
	type config struct {
		Telemetry section `k:"telemetry"`
		Other     section `k:"other"`
	}
	t.Setenv("APP_TELEMETRY_NAME", "old")
	t.Setenv("OTELX_HEADERS_TOKEN", "new")
	t.Setenv("ALIAS_NAME", "alias")
	t.Setenv("OTHER_NAME", "other")
	loader := New(
		WithEnvPrefix("APP"),
		WithSectionEnvPrefix("telemetry", "OTELX"),
		WithSectionEnvPrefix("other", "OTHER"),
		WithEnvAliases(map[string]string{"telemetry.name": "ALIAS_NAME"}),
		WithReader("config.json", strings.NewReader(`{"telemetry":{"headers":{"token":"file"},"name":"file"}}`)),
	)
	cfg, snapshot, err := LoadIntoWithSnapshot[config](loader)
	require.NoError(t, err)
	require.Equal(t, "new", cfg.Telemetry.Headers["token"])
	require.Equal(t, "OTELX_HEADERS_TOKEN", snapshot.Origins["telemetry.headers.token"].Identifier)
	require.Equal(t, "alias", cfg.Telemetry.Name)
	require.Equal(t, "other", cfg.Other.Name)
	require.Equal(t, "OTELX_HEADERS_TOKEN", loader.EnvName("telemetry.headers.token"))
	require.Equal(t, "OTELX_", loader.EnvName("telemetry"))
	require.Equal(t, "APP_TELEMETRYS_NAME", loader.EnvName("telemetrys.name"))
}

func TestSectionEnvPrefixWholeMapAndLongest(t *testing.T) {
	type section struct {
		Headers map[string]string `k:"headers"`
	}
	type config struct {
		Telemetry section `k:"telemetry"`
	}
	t.Setenv("OTELX_HEADERS", `{"token":"whole"}`)
	t.Setenv("APP_TELEMETRY_HEADERS_TOKEN", "old")
	loader := New(WithEnvPrefix("APP"), WithSectionEnvPrefix("telemetry", "OTELX"), WithDecodeHooks(JSONStringMapHook()))
	cfg, err := LoadInto[config](loader)
	require.NoError(t, err)
	require.Equal(t, "whole", cfg.Telemetry.Headers["token"])
	nested := New(WithSectionEnvPrefix("telemetry", "OTELX"), WithSectionEnvPrefix("telemetry.headers", "HEADER"), WithEnvKeyFunc(func(prefix, path string) string { return prefix + strings.ToUpper(strings.ReplaceAll(path, ".", "__")) }))
	require.Equal(t, "HEADER_TOKEN", nested.EnvName("telemetry.headers.token"))
	require.Equal(t, "OTELX_NAME", nested.EnvName("telemetry.name"))
}

func TestSectionEnvPrefixIgnoresRootName(t *testing.T) {
	type section struct {
		Name string `k:"name"`
	}
	type config struct {
		Telemetry section `k:"telemetry"`
	}
	t.Setenv("APP_TELEMETRY_NAME", "old")
	cfg, err := LoadInto[config](New(WithEnvPrefix("APP"), WithSectionEnvPrefix("telemetry", "OTELX")))
	require.NoError(t, err)
	require.Empty(t, cfg.Telemetry.Name)
}
