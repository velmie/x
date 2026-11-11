package confload

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectionInspection(t *testing.T) {
	type endpoint struct {
		URL      string `k:"url"`
		Password string `k:"password" sensitive:"true"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	elements, ok := fields.Element("endpoints")
	require.True(t, ok)
	_, ok = elements.Lookup("password")
	require.True(t, ok)
	rows, err := Inspect(fields, config{Endpoints: map[string]endpoint{"crm": {URL: "https://example.com", Password: "secret"}}}, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"crm": map[string]any{"url": "https://example.com", "password": "[REDACTED]"}}, rows[0].Value)
}

func TestCollectionInspectionProjectsNestedValues(t *testing.T) {
	type credentials struct {
		Password string `cfg:"password" sensitive:"true"`
	}
	type endpoint struct {
		URL    string       `cfg:"url"`
		Auth   *credentials `cfg:"auth"`
		Hidden string
	}
	type config struct {
		Endpoints map[string]*endpoint `cfg:"endpoints"`
		Routes    [][]*endpoint        `cfg:"routes"`
	}
	original := config{
		Endpoints: map[string]*endpoint{"crm": {URL: "https://example.com", Auth: &credentials{Password: "secret"}, Hidden: "omit"}, "nil": nil},
		Routes:    [][]*endpoint{{{URL: "url", Auth: &credentials{Password: "route-secret"}}, nil}},
	}
	fields, err := DescribeStructWithTag[config]("cfg")
	require.NoError(t, err)
	loader := New(WithTagName("cfg"), WithFiles("missing.yaml"))
	schema, err := loader.Inspect(fields, nil, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	require.Contains(t, inspectionPaths(schema), "endpoints.<name>.auth.password")
	require.Contains(t, inspectionPaths(schema), "routes[][].auth.password")
	rows, err := loader.Inspect(fields, original, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	require.Contains(t, inspectionPaths(rows), "routes.0.0.auth.password")
	for _, format := range []string{"text", "json"} {
		var output bytes.Buffer
		require.NoError(t, WriteInspection(&output, format, rows))
		require.Contains(t, output.String(), "https://example.com")
		require.NotContains(t, output.String(), "secret")
		require.NotContains(t, output.String(), "Hidden")
	}
	require.Equal(t, "secret", original.Endpoints["crm"].Auth.Password)
	require.Nil(t, original.Endpoints["nil"])
}

func inspectionPaths(rows []InspectionField) []string {
	paths := make([]string, 0, len(rows))
	for i := range rows {
		paths = append(paths, rows[i].Field.Path)
	}
	return paths
}

type secretJSON string

func (secretJSON) MarshalJSON() ([]byte, error) {
	panic("sensitive marshaler invoked")
}

func TestCollectionInspectionRedactsProvidedDefaults(t *testing.T) {
	type endpoint struct {
		URL      string     `k:"url"`
		Password secretJSON `k:"password" sensitive:"true"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	defaults := map[string]any{"endpoints": map[string]endpoint{"crm": {URL: "url", Password: "private-value"}}}
	loader := New(WithDefaults(defaults))
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	rows, err := loader.Inspect(fields, config{Endpoints: defaults["endpoints"].(map[string]endpoint)}, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	for _, format := range []string{"text", "json"} {
		var output bytes.Buffer
		require.NoError(t, WriteInspection(&output, format, rows))
		require.NotContains(t, output.String(), "private-value")
		require.Contains(t, output.String(), "url")
	}
	require.Equal(t, secretJSON("private-value"), defaults["endpoints"].(map[string]endpoint)["crm"].Password)
}

func ExampleFieldCollection_Element() {
	type endpoint struct {
		URL     string `k:"url" hint:"Destination URL"`
		Timeout string `k:"timeout" default:"10s"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	fields, err := DescribeStruct[config]()
	if err != nil {
		fmt.Println(err)
		return
	}
	elements, ok := fields.Element("endpoints")
	if !ok {
		fmt.Println("no structured elements")
		return
	}
	timeout, _ := elements.Lookup("timeout")
	fmt.Println(timeout.Path, timeout.Default)
	// Output: timeout 10s
}

func TestSensitiveCollectionDoesNotExposeKeys(t *testing.T) {
	type item struct {
		URL string `k:"url"`
	}
	type endpoint struct {
		Private map[string]item `k:"private" sensitive:"true"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints" sensitive:"true"`
		Other     map[string]endpoint `k:"other"`
	}
	fields, err := DescribeStruct[config]()
	require.NoError(t, err)
	rows, err := Inspect(fields, config{
		Endpoints: map[string]endpoint{"private-key": {}},
		Other:     map[string]endpoint{"public": {Private: map[string]item{"nested-private-key": {URL: "url"}}}},
	}, Snapshot{}, InspectOptions{})
	require.NoError(t, err)
	require.NotContains(t, inspectionPaths(rows), "endpoints.private-key.private")
	require.NotContains(t, inspectionPaths(rows), "other.public.private.nested-private-key.url")
	for _, format := range []string{"text", "json"} {
		var output bytes.Buffer
		require.NoError(t, WriteInspection(&output, format, rows))
		require.NotContains(t, output.String(), "private-key")
	}
}

func ExampleLoader_collectionEnvironment() {
	type endpoint struct {
		URL string `k:"url"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	const name = "CONFLOAD_EXAMPLE_ENDPOINTS_CRM_EU_URL"
	old, existed := os.LookupEnv(name)
	if err := os.Setenv(name, "https://example.com"); err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if existed {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	}()
	loader := New(
		WithEnvPrefix("CONFLOAD_EXAMPLE"),
		WithReader("config.yaml", strings.NewReader("endpoints:\n  crm_eu: {}\n")),
	)
	cfg, err := LoadInto[config](loader)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cfg.Endpoints["crm_eu"].URL)
	// Output: https://example.com
}
