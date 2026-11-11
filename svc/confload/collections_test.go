package confload

import (
	"errors"
	"strings"
	"testing"
	"time"

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

func TestCollectionElementDefaults(t *testing.T) {
	type endpoint struct {
		Timeout time.Duration `k:"timeout" default:"10s"`
	}
	type route struct {
		Weight int `k:"weight" default:"7"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
		Routes    []route             `k:"routes"`
	}
	loader := New(WithReader("input.yaml", strings.NewReader("endpoints:\n  crm: {}\nroutes:\n  - {}\n")))
	cfg, snapshot, err := LoadIntoWithSnapshot[config](loader)
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, cfg.Endpoints["crm"].Timeout)
	require.Equal(t, 7, cfg.Routes[0].Weight)
	require.Equal(t, SourceStructDefault, snapshot.Origins["endpoints.crm.timeout"].Source)
	require.Equal(t, SourceStructDefault, snapshot.Origins["routes.0.weight"].Source)
}

func TestCollectionDefaultsRespectPresenceAndReplacement(t *testing.T) {
	type nested struct {
		Enabled bool `k:"enabled" default:"true"`
	}
	type endpoint struct {
		Count   int      `k:"count" default:"9"`
		Enabled bool     `k:"enabled" default:"true"`
		Label   string   `k:"label" default:"fallback"`
		Values  []string `k:"values" default:"a,b"`
		Nested  *nested  `k:"nested"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
		Routes    []endpoint          `k:"routes"`
	}
	loader := New(
		WithReader("first.yaml", strings.NewReader("endpoints:\n  zeros:\n    count: 0\n    enabled: false\n    label: ''\n    values: []\n    nested: null\nroutes:\n  - count: 42\n")),
		WithReader("second.yaml", strings.NewReader("endpoints:\n  later: {}\nroutes:\n  - label: later\n")),
	)
	cfg, snapshot, err := LoadIntoWithSnapshot[config](loader)
	require.NoError(t, err)
	zero := cfg.Endpoints["zeros"]
	require.Zero(t, zero.Count)
	require.False(t, zero.Enabled)
	require.Empty(t, zero.Label)
	require.Empty(t, zero.Values)
	require.Nil(t, zero.Nested)
	require.Equal(t, 9, cfg.Endpoints["later"].Count)
	require.True(t, cfg.Endpoints["later"].Nested.Enabled)
	require.Len(t, cfg.Routes, 1)
	require.Equal(t, 9, cfg.Routes[0].Count)
	require.Equal(t, "later", cfg.Routes[0].Label)
	require.Equal(t, SourceStructDefault, snapshot.Origins["routes.0.count"].Source)
	require.Equal(t, SourceReader, snapshot.Origins["routes"].Source)
}

func TestCollectionDefaultsPreserveTypedZeroValues(t *testing.T) {
	type endpoint struct {
		Count int `k:"count" default:"9"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	provided := map[string]endpoint{"crm": {Count: 0}}
	cfg, err := LoadInto[config](New(WithDefaults(map[string]any{"endpoints": provided})))
	require.NoError(t, err)
	require.Zero(t, cfg.Endpoints["crm"].Count)
	require.Zero(t, provided["crm"].Count)
}

func TestCollectionDefaultsDoNotCreateElements(t *testing.T) {
	type endpoint struct {
		Count int `k:"count" default:"9"`
	}
	type config struct {
		Endpoints map[string]*endpoint `k:"endpoints"`
		Routes    []*endpoint          `k:"routes"`
	}
	cfg, _, err := LoadIntoWithSnapshot[config](New(WithReader("input.json", strings.NewReader(`{"endpoints":{"nil":null},"routes":[null]}`))))
	require.NoError(t, err)
	require.Len(t, cfg.Endpoints, 1)
	require.Nil(t, cfg.Endpoints["nil"])
	require.Len(t, cfg.Routes, 1)
	require.Nil(t, cfg.Routes[0])
}

func TestCollectionDefaultsFromTypedMapsAndPartialStructs(t *testing.T) {
	type endpoint struct {
		Name  string `k:"name"`
		Count int    `k:"count" default:"9"`
	}
	type partial struct {
		Name string `k:"name"`
	}
	type config struct {
		Endpoints map[string]endpoint `k:"endpoints"`
	}
	tests := []struct {
		name         string
		value        any
		expectedName string
	}{
		{name: "map", value: map[string]map[string]int{"crm": {}}},
		{name: "struct", value: map[string]partial{"crm": {Name: "partial"}}, expectedName: "partial"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, snapshot, err := LoadIntoWithSnapshot[config](New(WithDefaults(map[string]any{"endpoints": tc.value})))
			require.NoError(t, err)
			require.Equal(t, 9, cfg.Endpoints["crm"].Count)
			require.Equal(t, tc.expectedName, cfg.Endpoints["crm"].Name)
			require.Equal(t, SourceStructDefault, snapshot.Origins["endpoints.crm.count"].Source)
			if original, ok := tc.value.(map[string]map[string]int); ok {
				require.Empty(t, original["crm"])
			}
		})
	}
}

func TestCollectionDefaultsFromNestedTypedMap(t *testing.T) {
	type endpoint struct {
		Count int `k:"count" default:"9"`
	}
	type section struct {
		Items map[string]endpoint `k:"items"`
	}
	type config struct {
		Section section `k:"section"`
	}
	defaults := map[string]map[string]map[string]int{"items": {"crm": {}}}
	cfg, snapshot, err := LoadIntoWithSnapshot[config](New(WithDefaults(map[string]any{"section": defaults})))
	require.NoError(t, err)
	require.Equal(t, 9, cfg.Section.Items["crm"].Count)
	require.Equal(t, SourceStructDefault, snapshot.Origins["section.items.crm.count"].Source)
	require.Empty(t, defaults["items"]["crm"])
}

func TestCollectionDefaultsKeepOmittedZeroField(t *testing.T) {
	type endpoint struct {
		Count  int `k:"count" default:"9"`
		Weight int `k:"weight" default:"7"`
	}
	type partial struct {
		Count int `k:"count,omitempty"`
	}
	type config struct {
		Items map[string]endpoint `k:"items"`
	}
	cfg, snapshot, err := LoadIntoWithSnapshot[config](New(WithDefaults(map[string]any{"items": map[string]partial{"crm": {Count: 0}}})))
	require.NoError(t, err)
	require.Zero(t, cfg.Items["crm"].Count)
	require.Equal(t, 7, cfg.Items["crm"].Weight)
	require.Equal(t, SourceStructDefault, snapshot.Origins["items.crm.weight"].Source)
}

func TestCollectionDefaultsKeepUntaggedTypedZero(t *testing.T) {
	type endpoint struct {
		Count  int `k:"count" default:"9"`
		Weight int `k:"weight" default:"7"`
	}
	type partial struct{ Count int }
	type config struct {
		Items map[string]endpoint `k:"items"`
	}
	cfg, err := LoadInto[config](New(WithStrict(), WithDefaults(map[string]any{"items": map[string]partial{"crm": {Count: 0}}})))
	require.NoError(t, err)
	require.Zero(t, cfg.Items["crm"].Count)
	require.Equal(t, 7, cfg.Items["crm"].Weight)
}
