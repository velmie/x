package confload

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testBooleanField  = "boolean"
	testDurationField = "duration"
	testFirstValue    = "one"
	testCountField    = "count"
	testSecondValue   = "two"
)

func TestBlankNumericTokensAndPrecedence(t *testing.T) {
	type config struct {
		Signed   int           `k:"signed" default:"7"`
		Unsigned uint          `k:"unsigned" default:"7"`
		Float    float64       `k:"float" default:"7"`
		Boolean  bool          `k:"boolean"`
		Duration time.Duration `k:"duration"`
	}
	for _, field := range []string{"signed", "unsigned", "float", testBooleanField, testDurationField} {
		for _, raw := range []string{"", " \t"} {
			t.Run(field+strconv.Itoa(len(raw)), func(t *testing.T) {
				t.Setenv(EnvName("TEST_", field), raw)
				_, err := LoadInto[config](New(WithEnvPrefix("TEST_")))
				require.Error(t, err)
				require.Contains(t, err.Error(), field)
				if field == "signed" || field == "unsigned" || field == "float" {
					require.ErrorIs(t, err, strconv.ErrSyntax)
				}
			})
		}
	}
	for _, field := range []string{"signed", "unsigned", "float"} {
		for _, source := range []Option{
			WithOverrides(map[string]any{field: "  "}),
			WithReader("config.yaml", strings.NewReader(field+": '  '\n")),
		} {
			_, err := LoadInto[config](New(source))
			require.ErrorIs(t, err, strconv.ErrSyntax)
			require.Contains(t, err.Error(), field)
		}
	}
	for _, value := range []any{"0", 0} {
		cfg, err := LoadInto[config](New(WithOverrides(map[string]any{"signed": value, "unsigned": value, "float": value})))
		require.NoError(t, err)
		require.Zero(t, cfg.Signed)
		require.Zero(t, cfg.Unsigned)
		require.Zero(t, cfg.Float)
	}
	t.Setenv("TEST_SIGNED", "")
	cfg, err := LoadInto[config](New(WithEnvPrefix("TEST_"), WithOverrides(map[string]any{"signed": "2"})))
	require.NoError(t, err)
	require.Equal(t, 2, cfg.Signed)
	cfg, err = LoadInto[config](New())
	require.NoError(t, err)
	require.Equal(t, 7, cfg.Signed)
}

func TestBlankFilePaths(t *testing.T) {
	type config struct {
		Value string `k:"value"`
	}
	for _, opt := range []Option{WithFiles(""), WithFiles(" \t"), WithOptionalFiles(""), WithOptionalFiles(" \t")} {
		_, err := LoadInto[config](New(opt))
		require.ErrorContains(t, err, "configuration file path is empty")
	}
	_, err := LoadInto[config](New(WithOptionalFiles(t.TempDir() + "/missing.yaml")))
	require.NoError(t, err)
	_, err = LoadInto[config](New(WithFiles(), WithOptionalFiles()))
	require.NoError(t, err)
}

func TestLoaderInspectEffectiveDefaultsWithoutSources(t *testing.T) {
	type config struct {
		Private []string          `k:"private" sensitive:"true"`
		Count   int               `k:"count" default:"9"`
		Enabled bool              `k:"enabled" default:"true"`
		Labels  map[string]string `k:"labels"`
		Secret  string            `k:"secret" default:"tag-secret" sensitive:"true"`
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	inspectionFormatted.Store(0)
	defaults := map[string]any{
		testCountField: 3,
		"labels.first": testFirstValue,
		"secret":       make(chan int),
		"private":      []any{inspectionStringer{}},
	}
	invoked := false
	loader := New(
		WithEnvPrefix("APP_"),
		WithFiles("/nonexistent.yaml"),
		WithDefaults(defaults),
		WithDefaults(map[string]any{testCountField: 0, "enabled": false, "labels.second": testSecondValue}),
		WithDecodeHooks(func(_, _ reflect.Type, data any) (any, error) { invoked = true; return data, nil }),
	)
	t.Setenv("APP_COUNT", "invalid")
	unusedSnapshot := Snapshot{
		Values: map[string]any{testCountField: 77},
	}
	rows, err := loader.Inspect(fields, nil, unusedSnapshot, InspectOptions{})
	require.NoError(t, err)
	require.False(t, invoked)
	require.Zero(t, inspectionFormatted.Load())
	byPath := make(map[string]InspectionField)
	for _, row := range rows {
		byPath[row.Field.Path] = row
		require.Nil(t, row.Origin)
	}
	require.Equal(t, 0, byPath[testCountField].Field.Default)
	require.Equal(t, false, byPath["enabled"].Field.Default)
	require.Equal(t, map[string]any{"first": testFirstValue, "second": testSecondValue}, byPath["labels"].Field.Default)
	require.Equal(t, "[REDACTED]", byPath["secret"].Field.Default)
	require.Equal(t, "[REDACTED]", byPath["private"].Field.Default)
	require.Equal(t, "APP_COUNT", byPath[testCountField].Env)
	var out bytes.Buffer
	require.NoError(t, WriteInspection(&out, "json", rows))
	require.NotContains(t, out.String(), "tag-secret")
	tagRows, err := Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, row := range tagRows {
		if row.Field.Path == testCountField {
			require.Equal(t, "9", row.Field.Default)
		}
	}
	original, _ := fields.Lookup(testCountField)
	require.Equal(t, "9", original.Default)
	require.Equal(t, 3, defaults[testCountField])
	snapshot := Snapshot{
		Origins: map[string]ValueOrigin{
			testCountField: {
				Source: SourceEnv,
			},
		},
	}
	opts := InspectOptions{
		EnvName: func(string) string { return "CUSTOM" },
	}
	cfg := config{
		Count: 8,
	}
	rows, err = loader.Inspect(fields, cfg, snapshot, opts)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Field.Path == testCountField {
			require.Equal(t, "CUSTOM", row.Env)
			require.Equal(t, 8, row.Value)
			require.Equal(t, "CUSTOM", row.Origin.Identifier)
		}
	}
}

func TestJSONStringMapHook(t *testing.T) {
	type config struct {
		Labels map[string]string `k:"labels"`
	}
	loader := func(value any) *Loader {
		return New(WithDecodeHooks(JSONStringMapHook()), WithOverrides(map[string]any{"labels": value}))
	}
	cfg, err := LoadInto[config](loader(`{"a":"b"}`))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"a": "b"}, cfg.Labels)
	cfg, err = LoadInto[config](loader(map[string]string{"x": "y"}))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"x": "y"}, cfg.Labels)
	_, err = LoadInto[config](loader(`{"a":`))
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
	for _, value := range []string{"", "null", `{"a":1}`} {
		_, err = LoadInto[config](loader(value))
		require.Error(t, err)
	}
	_, err = LoadInto[config](New(WithOverrides(map[string]any{"labels": `{"a":"b"}`})))
	require.Error(t, err)
}

func TestLoaderInspectPreservesTypedSliceDefaults(t *testing.T) {
	type config struct {
		Values []any `k:"values"`
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	defaults := []any{1, false, nil}
	loader := New(WithDefaults(map[string]any{"values": defaults}))
	rows, err := loader.Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	require.Equal(t, defaults, rows[0].Field.Default)
}

var inspectionFormatted atomic.Int32

type inspectionStringer struct{}

func (inspectionStringer) String() string {
	inspectionFormatted.Add(1)
	return "sensitive"
}
