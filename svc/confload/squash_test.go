package confload

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type SquashConnection struct {
	URL     string `k:"url" default:"nats://localhost" sensitive:"true"`
	Timeout string `k:"timeout" default:"2s"`
}

type squashConfig struct {
	SquashConnection `k:",squash"`
	Events           struct {
		Stream string `k:"stream" default:"events"`
	} `k:"events"`
}

type hiddenConnection struct {
	URL string `k:"url"`
}

func TestSquashRejectsUnexportedEmbeddedField(t *testing.T) {
	type config struct {
		hiddenConnection `k:",squash"`
	}
	_, err := DescribeStruct[config]()
	require.ErrorContains(t, err, "squash on hiddenConnection")
	_, err = LoadInto[config](New())
	require.ErrorContains(t, err, "squash on hiddenConnection")
}

func TestSquashedConfiguration(t *testing.T) {
	t.Setenv("APP_BROKER_URL", "nats://environment")
	loader := New(
		WithEnvPrefix("APP"),
		WithStrict(),
		WithDefaults(map[string]any{"broker.timeout": "5s"}),
		WithReader("config.yaml", strings.NewReader("broker:\n  events:\n    stream: custom\n")),
	)
	cfg, err := LoadSubset[squashConfig](loader, "broker")
	require.NoError(t, err)
	require.Equal(t, "nats://environment", cfg.URL)
	require.Equal(t, "5s", cfg.Timeout)
	require.Equal(t, "custom", cfg.Events.Stream)
	fields, err := DescribeStruct[squashConfig]()
	require.NoError(t, err)
	field, ok := fields.Lookup("url")
	require.True(t, ok)
	require.Equal(t, []int{0, 0}, field.Index)
	require.Equal(t, "true", field.Attributes["sensitive"])
	require.Equal(t, "nats://localhost", field.Default)
	bad := New(WithStrict(), WithReader("config.yaml", strings.NewReader("broker:\n  connection:\n    url: bad\n")))
	_, err = LoadSubset[squashConfig](bad, "broker")
	require.Error(t, err)
}

func TestSquashRejectsCollisionsBeforeSources(t *testing.T) {
	type collision struct {
		SquashConnection `k:",squash"`
		URL              string `k:"url"`
	}
	_, err := DescribeStruct[collision]()
	require.ErrorContains(t, err, "url")
	require.ErrorContains(t, err, "SquashConnection.URL")
	require.ErrorContains(t, err, "URL")
	_, err = LoadInto[collision](New(WithFiles("missing.yaml")))
	require.ErrorContains(t, err, "SquashConnection.URL")
}

func TestSquashRejectsLeafContainerCollision(t *testing.T) {
	type collision struct {
		Value string `k:"server"`
		Other string `k:"server.url"`
	}
	_, err := DescribeStruct[collision]()
	require.ErrorContains(t, err, "server")
	require.ErrorContains(t, err, "Value")
	require.ErrorContains(t, err, "Other")
}

func TestSquashedDefaultsAndInspection(t *testing.T) {
	loader := New(WithDefaults(map[string]any{"timeout": "9s"}))
	cfg, err := LoadInto[squashConfig](loader)
	require.NoError(t, err)
	require.Equal(t, "nats://localhost", cfg.URL)
	require.Equal(t, "9s", cfg.Timeout)
	fields, err := DescribeStruct[squashConfig]()
	require.NoError(t, err)
	rows, err := loader.Inspect(fields, cfg, loader.Snapshot(), InspectOptions{})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Field.Path == "timeout" {
			require.Equal(t, "9s", row.Field.Default)
			require.Equal(t, "9s", row.Value)
			return
		}
	}
	t.Fatal("timeout is missing from inspection")
}
