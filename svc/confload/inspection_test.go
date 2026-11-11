package confload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/stretchr/testify/require"
)

const (
	testJSONFormat = "json"
	testTextFormat = "text"
)

type inspectionConfig struct {
	Name      string              `k:"name" default:"default-name"`
	Zero      int                 `k:"zero"`
	Values    map[string]string   `k:"values"`
	Secret    inspectionSecret    `k:"secret" sensitive:"true" default:"secret-default"`
	Protected inspectionProtected `k:"protected" sensitive:"true"`
	Optional  *inspectionOptional `k:"optional"`
}

type inspectionProtected struct {
	Value inspectionSecret `k:"value" default:"nested-secret-default"`
}

type inspectionOptional struct {
	Count int `k:"count"`
}

type inspectionSecret string

func (inspectionSecret) String() string               { panic("sensitive value formatted") }
func (inspectionSecret) MarshalJSON() ([]byte, error) { panic("sensitive value marshaled") }

type inspectionRecursive struct {
	Next   *inspectionRecursive
	Secret inspectionSecret `sensitive:"true"`
}

func TestInspectMasksCollectionsWithSensitiveDescendants(t *testing.T) {
	type credentials struct {
		Password inspectionSecret `k:"password" sensitive:"true"`
	}
	type endpoint struct {
		Name        string        `k:"name"`
		Credentials **credentials `k:"credentials"`
	}
	type config struct {
		Endpoints map[string]*endpoint   `k:"endpoints"`
		Routes    []endpoint             `k:"routes"`
		Arrays    [1][]*endpoint         `k:"arrays"`
		Public    []string               `k:"public"`
		Recursive []*inspectionRecursive `k:"recursive"`
	}
	secret := &credentials{
		Password: "decoded-secret",
	}
	endpointValue := endpoint{
		Name:        "service",
		Credentials: &secret,
	}
	cfg := config{
		Endpoints: map[string]*endpoint{"one": &endpointValue},
		Routes:    []endpoint{endpointValue},
		Arrays:    [1][]*endpoint{{&endpointValue}},
		Public:    []string{"visible"},
	}
	defaultSecret := &credentials{
		Password: "default-secret",
	}
	defaultEndpoint := endpoint{
		Name:        "default",
		Credentials: &defaultSecret,
	}
	defaults := map[string]any{
		"endpoints": map[string]*endpoint{"default": &defaultEndpoint},
		"routes":    []endpoint{defaultEndpoint},
		"arrays":    [1][]*endpoint{{&defaultEndpoint}},
		"public":    []string{"default-visible"},
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	loader := New(WithDefaults(defaults))
	for _, inspect := range []struct {
		name string
		fn   func(any) ([]InspectionField, error)
	}{
		{
			name: "package",
			fn: func(value any) ([]InspectionField, error) {
				return Inspect(fields, value, Snapshot{}, InspectOptions{})
			},
		},
		{
			name: "loader",
			fn: func(value any) ([]InspectionField, error) {
				return loader.Inspect(fields, value, Snapshot{}, InspectOptions{})
			},
		},
	} {
		t.Run(inspect.name, func(t *testing.T) {
			for _, value := range []any{nil, cfg} {
				rows, err := inspect.fn(value)
				require.NoError(t, err)
				for _, row := range rows {
					switch row.Field.Path {
					case "arrays", "endpoints", "recursive", "routes":
						require.Equal(t, "[REDACTED]", row.Field.Default)
						require.NotEqual(t, "true", row.Field.Attributes["sensitive"])
						if value != nil {
							require.Equal(t, "[REDACTED]", row.Value)
						}
					case "public":
						if value != nil {
							require.Equal(t, []string{"visible"}, row.Value)
						}
					}
				}
				for _, format := range []string{testTextFormat, testJSONFormat} {
					var out bytes.Buffer
					require.NoError(t, WriteInspection(&out, format, rows))
					require.NotContains(t, out.String(), "decoded-secret")
					require.NotContains(t, out.String(), "default-secret")
				}
			}
		})
	}
	require.Equal(t, inspectionSecret("decoded-secret"), secret.Password)
	require.Equal(t, inspectionSecret("default-secret"), defaultSecret.Password)
	require.Equal(t, "service", cfg.Endpoints["one"].Name)
	require.Equal(t, "default", defaults["endpoints"].(map[string]*endpoint)["default"].Name)
	descriptor, ok := fields.Lookup("endpoints")
	require.True(t, ok)
	require.NotEqual(t, "true", descriptor.Attributes["sensitive"])
}

func TestInspectAndWriteInspection(t *testing.T) {
	fields, err := DescribeStruct[inspectionConfig]()
	require.NoError(t, err)
	cfg := inspectionConfig{
		Name: "decoded",
		Values: map[string]string{
			"message": "line1\nline2",
		},
		Secret: "secret-decoded",
		Protected: inspectionProtected{
			Value: "nested-secret-decoded",
		},
	}
	snapshot := Snapshot{
		Values: map[string]any{
			"name": "raw-snapshot-value",
		},
		Origins: map[string]ValueOrigin{
			"name": {
				Source:     SourceEnv,
				Identifier: "RAW_ENV_NAME",
			},
			"zero": {
				Source:     SourceFile,
				Identifier: "fixture-private-path",
			},
			"values": {
				Source:     SourceReader,
				Identifier: "fixture-private-reader",
			},
			"secret": {
				Source: SourceOverride,
			},
		},
		Files: []string{"fixture-private-path"},
	}
	aliases := map[string]string{
		"name": "ACTUAL_NAME",
	}
	loader := New(WithEnvAliases(aliases))
	rows, err := Inspect(
		fields,
		cfg,
		snapshot,
		InspectOptions{
			EnvName:       loader.EnvName,
			OverrideLabel: "cli",
		},
	)
	require.NoError(t, err)
	require.Equal(t, fields.Len(), len(rows))
	var out bytes.Buffer
	require.NoError(t, WriteInspection(&out, testJSONFormat, rows))
	text := out.String()
	for _, absent := range []string{
		"secret-default",
		"secret-decoded",
		"nested-secret-default",
		"nested-secret-decoded",
		"raw-snapshot-value",
		"fixture-private-path",
		"fixture-private-reader",
		"RAW_ENV_NAME",
	} {
		require.NotContains(t, text, absent)
	}
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	byName := make(map[string]map[string]any)
	for _, row := range decoded {
		byName[row["name"].(string)] = row
	}
	require.Equal(t, "decoded", byName["name"]["value"])
	require.Equal(t, "ACTUAL_NAME", byName["name"]["env"])
	require.Equal(t, float64(0), byName["zero"]["value"])
	require.Equal(t, "file[1]", byName["zero"]["origin"].(map[string]any)["identifier"])
	require.Nil(t, byName["optional.count"]["value"])
	for _, path := range []string{"secret", "protected.value"} {
		require.Equal(t, true, byName[path]["sensitive"])
		require.Equal(t, "[REDACTED]", byName[path]["default"])
		require.Equal(t, "[REDACTED]", byName[path]["value"])
	}
	out.Reset()
	require.NoError(t, WriteInspection(&out, testTextFormat, rows))
	require.Contains(t, out.String(), `line1\nline2`)
	require.NotContains(t, out.String(), "line1\nline2")
	require.NotContains(t, out.String(), "fixture-private")
}

func TestInspectSchemaOnlyAndTypeMismatch(t *testing.T) {
	fields, err := DescribeStruct[inspectionConfig]()
	require.NoError(t, err)
	rows, err := Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, WriteInspection(&out, "", rows))
	require.NotContains(t, out.String(), " value=")
	out.Reset()
	require.NoError(t, WriteInspection(&out, testJSONFormat, rows))
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	for _, row := range decoded {
		_, hasValue := row["value"]
		_, hasOrigin := row["origin"]
		require.False(t, hasValue)
		require.False(t, hasOrigin)
	}
	_, err = Inspect(fields, struct{}{}, Snapshot{}, InspectOptions{})
	require.Error(t, err)
}

func TestWriteInspectionErrors(t *testing.T) {
	cause := errors.New("writer fixture failure")
	fields, err := DescribeStruct[inspectionConfig]()
	require.NoError(t, err)
	rows, err := Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, format := range []string{testTextFormat, testJSONFormat} {
		err := WriteInspection(
			failingInspectionWriter{
				cause: cause,
			},
			format,
			rows,
		)
		require.ErrorIs(t, err, cause)
		require.Same(t, cause, err)
	}
	require.Error(t, WriteInspection(io.Discard, "other", nil))
}

type failingInspectionWriter struct {
	cause error
}

func (w failingInspectionWriter) Write([]byte) (int, error) { return 0, w.cause }

func TestParseOverrides(t *testing.T) {
	fields, err := DescribeStruct[inspectionConfig]()
	require.NoError(t, err)
	values, err := ParseOverrides(fields, []string{"name=first", "name=a=b", "zero="})
	require.NoError(t, err)
	require.Equal(
		t,
		map[string]any{
			"name": "a=b",
			"zero": "",
		},
		values,
	)
	for _, entry := range []string{"", "no-equals", "=secret-value"} {
		_, err = ParseOverrides(fields, []string{entry})
		require.ErrorIs(t, err, ErrInvalidOverride)
		require.NotContains(t, err.Error(), "secret-value")
	}
	_, err = ParseOverrides(fields, []string{"unknown=secret-value"})
	var unknown *UnknownKeysError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, []string{"unknown"}, unknown.Keys)
	require.NotContains(t, err.Error(), "secret-value")
	path := strings.Repeat("x", 200) + "\n"
	_, err = ParseOverrides(fields, []string{path + "=secret-value"})
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, []string{path}, unknown.Keys)
	require.Contains(t, err.Error(), path)
	require.NotContains(t, err.Error(), "secret-value")
}

func TestConversionDiagnosticsPreserveCausesWithoutValues(t *testing.T) {
	type config struct {
		Signed   int           `k:"signed"`
		Unsigned uint          `k:"unsigned"`
		Fraction float64       `k:"fraction"`
		Boolean  bool          `k:"boolean"`
		Duration time.Duration `k:"duration"`
	}
	overrides := map[string]any{
		"signed":   "secret-invalid-int",
		"unsigned": "secret-invalid-uint",
		"fraction": "secret-invalid-float",
		"boolean":  "secret-invalid-bool",
		"duration": "secret-invalid-duration",
	}
	loader := New(WithOverrides(overrides))
	_, err := LoadInto[config](loader)
	require.Error(t, err)
	for _, field := range []string{"signed", "unsigned", "fraction", "boolean", "duration"} {
		require.Contains(t, err.Error(), field)
	}
	for _, expected := range []string{"integer", "number", "boolean", "duration"} {
		require.Contains(t, err.Error(), expected)
	}
	require.NotContains(t, err.Error(), "secret-invalid")
	require.ErrorIs(t, err, strconv.ErrSyntax)
	var decode *mapstructure.DecodeError
	require.ErrorAs(t, err, &decode)
	require.Contains(t, err.Error(), strconv.ErrSyntax.Error())
	var number *strconv.NumError
	require.NotErrorAs(t, err, &number)
}

func TestConversionDiagnosticsInJSONAndTextLogs(t *testing.T) {
	type config struct {
		Retries int     `k:"retries"`
		Ratio   float64 `k:"ratio"`
	}
	overrides := map[string]any{
		"retries": "private-invalid-count",
		"ratio":   "private-invalid-ratio",
	}
	loader := New(WithOverrides(overrides))
	_, err := LoadInto[config](loader)
	require.Error(t, err)
	for _, format := range []string{testJSONFormat, testTextFormat} {
		var out bytes.Buffer
		var handler slog.Handler
		if format == testJSONFormat {
			handler = slog.NewJSONHandler(&out, nil)
		} else {
			handler = slog.NewTextHandler(&out, nil)
		}
		logger := slog.New(handler)
		logger.LogAttrs(
			context.Background(),
			slog.LevelError,
			"configuration failed",
			slog.String("stage", "decode"),
			slog.Any("error", err),
		)
		require.Contains(t, out.String(), "retries")
		require.Contains(t, out.String(), "ratio")
		require.Contains(t, out.String(), "integer")
		require.Contains(t, out.String(), "number")
		require.Contains(t, out.String(), strconv.ErrSyntax.Error())
		require.NotContains(t, out.String(), "private-invalid")
	}
}

func TestURLConversionRetainsNativeCause(t *testing.T) {
	type config struct {
		Endpoint *url.URL `k:"endpoint"`
	}
	overrides := map[string]any{
		"endpoint": "https://example-user:example-secret@example.invalid/%zz",
	}
	loader := New(WithOverrides(overrides))
	_, err := LoadInto[config](loader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint")
	require.Contains(t, err.Error(), "absolute URL")
	require.NotContains(t, err.Error(), "example-user")
	require.NotContains(t, err.Error(), "example-secret")
	var escape url.EscapeError
	require.ErrorAs(t, err, &escape)
	require.ErrorIs(t, err, escape)
	require.Contains(t, err.Error(), escape.Error())
}

func TestInspectFullAndSubsetLoads(t *testing.T) {
	type databaseConfig struct {
		Port int `k:"port"`
	}
	type appConfig struct {
		Database databaseConfig `k:"database"`
	}
	overrides := map[string]any{
		"database.port": "42",
	}
	loader := New(WithOverrides(overrides))
	cfg, snapshot, err := LoadIntoWithSnapshot[appConfig](loader)
	require.NoError(t, err)
	fields, err := DescribeStruct[appConfig]()
	require.NoError(t, err)
	rows, err := Inspect(fields, cfg, snapshot, InspectOptions{})
	require.NoError(t, err)
	require.Equal(t, 42, rows[0].Value)
	require.Equal(t, SourceOverride, rows[0].Origin.Source)

	subset, err := LoadSubset[databaseConfig](loader, "database")
	require.NoError(t, err)
	subsetFields, err := DescribeStruct[databaseConfig]()
	require.NoError(t, err)
	fullSnapshot := loader.Snapshot()
	projected := Snapshot{
		Origins: make(map[string]ValueOrigin),
		Files:   fullSnapshot.Files,
	}
	for _, field := range subsetFields.Fields() {
		if origin, ok := fullSnapshot.Origins["database."+field.Path]; ok {
			projected.Origins[field.Path] = origin
		}
	}
	rows, err = Inspect(subsetFields, subset, projected, InspectOptions{})
	require.NoError(t, err)
	require.Equal(t, 42, rows[0].Value)
	require.Equal(t, SourceOverride, rows[0].Origin.Source)
}

func TestConversionRangeAndDurationErrors(t *testing.T) {
	_, err := stringToNumberHook(reflect.TypeFor[string](), reflect.TypeFor[int8](), "999")
	require.ErrorIs(t, err, strconv.ErrRange)
	require.Contains(t, err.Error(), strconv.ErrRange.Error())
	require.NotContains(t, err.Error(), "999")

	_, err = stringToDurationHook(reflect.TypeFor[string](), reflect.TypeFor[time.Duration](), "not-duration")
	require.EqualError(t, err, "expected a Go duration such as 5s or 1m")
	require.Nil(t, errors.Unwrap(err))
}

func TestInspectKeepsOverrideLabel(t *testing.T) {
	fields, err := DescribeStruct[inspectionConfig]()
	require.NoError(t, err)
	label := "cli-" + strings.Repeat("x", 70)
	snapshot := Snapshot{
		Origins: map[string]ValueOrigin{
			"name": {
				Source: SourceOverride,
			},
		},
	}
	rows, err := Inspect(
		fields,
		inspectionConfig{},
		snapshot,
		InspectOptions{
			OverrideLabel: label,
		},
	)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Field.Path == "name" {
			require.Equal(t, label, row.Origin.Identifier)
			return
		}
	}
	t.Fatal("name field not found")
}

func TestConversionSentinels(t *testing.T) {
	_, err := stringToURLHook(reflect.TypeFor[string](), urlType, "relative")
	require.ErrorIs(t, err, errInvalidAbsoluteURL)
	_, err = stringToBoolHook(reflect.TypeFor[string](), reflect.TypeFor[bool](), "maybe")
	require.ErrorIs(t, err, errInvalidBool)
}
