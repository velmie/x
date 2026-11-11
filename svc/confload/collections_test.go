package confload

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStrictCollectionElements(t *testing.T) {
	type endpoint struct {
		Timeout string `k:"timeout"`
	}
	type route struct {
		Endpoint string `k:"endpoint"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
		Routes    []route             `k:"routes"`
	}
	loader := New(WithStrict(), WithReader("input.yaml", strings.NewReader("endpoints:\n  crm:\n    timout: 2s\nroutes:\n  - endpont: crm\n")))
	_, err := LoadInto[config](loader)
	var unknown *UnknownKeysError
	require.True(t, errors.As(err, &unknown))
	require.Equal(t, []string{"endpoints.crm.timout", "routes.0.endpont"}, unknown.Keys)
	require.Equal(t, SourceReader, unknown.Origins["endpoints.crm.timout"].Source)
	require.Equal(t, SourceReader, unknown.Origins["routes.0.endpont"].Source)
}

func TestStrictCollectionFormatsAndSubset(t *testing.T) {
	type endpoint struct {
		URL string `k:"url"`
	}
	type route struct {
		Endpoint string `k:"endpoint"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
		Routes    []route             `k:"routes"`
		Labels    map[string]string   `k:"labels"`
		Anything  map[string]any      `k:"anything"`
	}
	tests := []struct {
		name  string
		input string
	}{
		{name: "input.yaml", input: "endpoints:\n  'crm.eu':\n    typo: x\nroutes:\n  - wrong: x\n"},
		{name: "input.json", input: `{"endpoints":{"crm.eu":{"typo":"x"}},"routes":[{"wrong":"x"}]}`},
		{name: "input.toml", input: "[endpoints.\"crm.eu\"]\ntypo = 'x'\n[[routes]]\nwrong = 'x'\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadInto[config](New(WithStrict(), WithReader(tc.name, strings.NewReader(tc.input))))
			var unknown *UnknownKeysError
			require.ErrorAs(t, err, &unknown)
			require.Equal(t, []string{`endpoints["crm.eu"].typo`, "routes.0.wrong"}, unknown.Keys)
			require.Equal(t, SourceReader, unknown.Origins[`endpoints["crm.eu"].typo`].Source)
			require.Equal(t, SourceReader, unknown.Origins["routes.0.wrong"].Source)
		})
	}
	input := `{"section":{"labels":{"free":"value"},"anything":{"free":{"child":1}}},"unrelated":{"unknown":true}}`
	cfg, err := LoadSubset[config](New(WithStrict(), WithReader("input.json", strings.NewReader(input))), "section")
	require.NoError(t, err)
	require.Equal(t, "value", cfg.Labels["free"])
}

func TestStrictCollectionLiteralKeyOrigins(t *testing.T) {
	type inner struct {
		Valid string `k:"valid"`
	}
	type endpoint struct {
		B inner `k:"b"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	loader := New(
		WithStrict(),
		WithReader("first.json", strings.NewReader(`{"endpoints":{"a":{"b":{"typo":"one"}}}}`)),
		WithReader("second.json", strings.NewReader(`{"endpoints":{"a.b":{"typo":"two"}}}`)),
	)
	_, err := LoadInto[config](loader)
	var unknown *UnknownKeysError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, []string{"endpoints.a.b.typo", `endpoints["a.b"].typo`}, unknown.Keys)
	require.Equal(t, "first.json", unknown.Origins["endpoints.a.b.typo"].Identifier)
	require.Equal(t, "second.json", unknown.Origins[`endpoints["a.b"].typo`].Identifier)
}

func TestStrictCollectionTypedStruct(t *testing.T) {
	type endpoint struct {
		URL string `k:"url"`
	}
	type invalidEndpoint struct {
		Typo string `k:"typo"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	valid := New(WithStrict(), WithDefaults(map[string]any{"endpoints": map[string]endpoint{"crm": {URL: "url"}}}))
	_, err := LoadInto[config](valid)
	require.NoError(t, err)
	invalid := New(WithStrict(), WithDefaults(map[string]any{"endpoints": map[string]invalidEndpoint{"crm": {Typo: "value"}}}))
	_, err = LoadInto[config](invalid)
	var unknown *UnknownKeysError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, []string{"endpoints.crm.typo"}, unknown.Keys)
	require.Equal(t, SourceDefaults, unknown.Origins["endpoints.crm.typo"].Source)
}

func TestStrictCollectionOverrideOrigin(t *testing.T) {
	type endpoint struct {
		URL string `k:"url"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	loader := New(
		WithStrict(),
		WithReader("first.json", strings.NewReader(`{"endpoints":{"crm":{"typo":"one"}}}`)),
		WithOverrides(map[string]any{"endpoints.crm.typo": "two"}),
	)
	_, err := LoadInto[config](loader)
	var unknown *UnknownKeysError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, SourceOverride, unknown.Origins["endpoints.crm.typo"].Source)
}

func TestStrictCollectionLiteralKeyDefaultOrigin(t *testing.T) {
	type inner struct {
		Valid string `k:"valid"`
	}
	type endpoint struct {
		B inner `k:"b"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	loader := New(
		WithStrict(),
		WithDefaults(map[string]any{"endpoints": map[string]any{"crm.eu": map[string]any{"typo": "first"}}}),
		WithReader("second.json", strings.NewReader(`{"endpoints":{"crm":{"eu":{"typo":"second"}}}}`)),
	)
	_, err := LoadInto[config](loader)
	var unknown *UnknownKeysError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, SourceDefaults, unknown.Origins[`endpoints["crm.eu"].typo`].Source)
	require.Equal(t, SourceReader, unknown.Origins["endpoints.crm.eu.typo"].Source)
}
