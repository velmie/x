package confload

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type recursiveConfigNode struct {
	Children []recursiveConfigNode `k:"children"`
}

func TestInspectionReorderedDefaultsNeverExposeSecrets(t *testing.T) {
	type endpoint struct {
		URL      string `k:"url"`
		Password string `k:"password" sensitive:"true"`
	}
	type defaultsEndpoint struct {
		Password string `k:"password"`
		URL      string `k:"url"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	defaults := map[string]any{
		"endpoints": map[string]defaultsEndpoint{"one": {
			Password: "secret-canary",
			URL:      "public-url",
		}},
	}
	loader := New(WithDefaults(defaults))
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	rows, err := loader.Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, format := range []string{"json", "text"} {
		var out bytes.Buffer
		require.NoError(t, WriteInspection(&out, format, rows))
		require.NotContains(t, out.String(), "secret-canary")
		require.Contains(t, out.String(), "public-url")
	}
}

func TestLoadPartialCollectionStructWithoutTagDefaults(t *testing.T) {
	type endpoint struct {
		URL  string `k:"url"`
		Name string `k:"name"`
	}
	type partial struct {
		Name string `k:"name"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	defaults := map[string]any{
		"endpoints": map[string]partial{"one": {
			Name: "service",
		}},
	}
	cfg, snapshot, err := LoadIntoWithSnapshot[config](New(WithDefaults(defaults)))
	require.NoError(t, err)
	require.Equal(t, "service", cfg.Endpoints["one"].Name)
	require.Equal(t, SourceDefaults, snapshot.Origins["endpoints.one.name"].Source)
}

func TestInspectionMasksOpaqueSensitiveCollections(t *testing.T) {
	type endpoint struct {
		URL      string `k:"url"`
		Password string `k:"password" sensitive:"true"`
	}
	type group struct {
		Opaque map[int]endpoint `k:"opaque"`
	}
	type config struct {
		Opaque map[int]endpoint   `k:"opaque"`
		Nested []map[int]endpoint `k:"nested"`
		Groups []group            `k:"groups"`
	}
	values := map[int]endpoint{1: {
		URL:      "public",
		Password: "secret-canary",
	}}
	cfg := config{
		Opaque: values,
		Nested: []map[int]endpoint{values},
		Groups: []group{{
			Opaque: values,
		}},
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	loader := New(WithDefaults(map[string]any{"opaque": values, "nested": cfg.Nested, "groups": cfg.Groups}))
	for _, inspect := range []func() ([]InspectionField, error){
		func() ([]InspectionField, error) { return Inspect(fields, cfg, Snapshot{}, InspectOptions{}) },
		func() ([]InspectionField, error) { return loader.Inspect(fields, cfg, Snapshot{}, InspectOptions{}) },
	} {
		rows, err := inspect()
		require.NoError(t, err)
		for _, format := range []string{"json", "text"} {
			var out bytes.Buffer
			require.NoError(t, WriteInspection(&out, format, rows))
			require.NotContains(t, out.String(), "secret-canary")
		}
	}
}

func TestRecursiveCollectionSchemaRejected(t *testing.T) {
	type config struct {
		Nodes []recursiveConfigNode `k:"nodes"`
	}
	_, err := DescribeStruct[config]()
	require.Error(t, err)
	require.ErrorContains(t, err, "children")
	require.ErrorContains(t, err, "recursiveConfigNode")
	_, err = LoadInto[config](New())
	require.Error(t, err)
	require.ErrorContains(t, err, "children")
	require.ErrorContains(t, err, "recursiveConfigNode")
}

func TestInspectionMasksMismatchedSensitiveCollectionDefaults(t *testing.T) {
	type endpoint struct {
		URL      string `k:"url"`
		Password string `k:"password" sensitive:"true"`
	}
	type config struct {
		Routes []endpoint `k:"routes"`
	}
	defaults := map[string]any{
		"routes": map[string]endpoint{"one": {
			URL:      "public",
			Password: "mismatch-secret",
		}},
	}
	loader := New(WithDefaults(defaults))
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	rows, err := loader.Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, format := range []string{"json", "text"} {
		var output bytes.Buffer
		require.NoError(t, WriteInspection(&output, format, rows))
		require.NotContains(t, output.String(), "mismatch-secret")
	}
}

func TestInspectionMasksMismatchedMapKeyDefaults(t *testing.T) {
	type endpoint struct {
		URL      string `k:"url"`
		Password string `k:"password" sensitive:"true"`
	}
	type reordered struct {
		Password string `k:"password"`
		URL      string `k:"url"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	defaults := map[string]any{
		"endpoints": map[int]reordered{1: {
			Password: "key-mismatch-canary",
			URL:      "public",
		}},
	}
	loader := New(WithDefaults(defaults))
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	rows, err := loader.Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, format := range []string{"json", "text"} {
		var output bytes.Buffer
		require.NoError(t, WriteInspection(&output, format, rows))
		require.NotContains(t, output.String(), "key-mismatch-canary")
	}
}

func TestOpaqueCollectionWithSensitiveKeyIsRedacted(t *testing.T) {
	type key struct {
		Token string `sensitive:"true"`
	}
	value := []map[key]string{{{Token: "secret-canary"}: "x"}}
	if got := inspectionOpaqueValue(reflect.TypeOf(value), reflect.ValueOf(value)); got != inspectionRedacted {
		t.Fatalf("opaque collection with a sensitive key: %v", got)
	}
}
