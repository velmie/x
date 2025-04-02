package envx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/velmie/x/envx"
)

type ResolverTestConfig struct {
	String  string   `env:"TEST_STRING;required"`
	Int     int      `env:"TEST_INT;required"`
	Boolean bool     `env:"TEST_BOOL"`
	Slice   []string `env:"TEST_SLICE"`
}

func TestStructLoaderWithCustomResolver(t *testing.T) {
	// Create a custom resolver with a MapSource
	mapSource := envx.NewMapSource(map[string]string{
		"TEST_STRING": "test_value",
		"TEST_INT":    "42",
		"TEST_BOOL":   "true",
		"TEST_SLICE":  "one,two,three",
	}, "Test Source")

	resolver := envx.NewResolver(mapSource)

	// Test loading with custom resolver
	var config ResolverTestConfig
	err := envx.Load(&config, envx.WithResolver(resolver))

	assert.NoError(t, err)
	assert.Equal(t, "test_value", config.String)
	assert.Equal(t, 42, config.Int)
	assert.Equal(t, true, config.Boolean)
	assert.Equal(t, []string{"one", "two", "three"}, config.Slice)
}

func TestStructLoaderWithDefaultResolver(t *testing.T) {
	// Save old DefaultResolver and restore it after test
	oldResolver := envx.DefaultResolver
	defer func() { envx.DefaultResolver = oldResolver }()

	// Replace DefaultResolver with a custom one
	mapSource := envx.NewMapSource(map[string]string{
		"TEST_STRING": "default_value",
		"TEST_INT":    "100",
		"TEST_BOOL":   "false",
		"TEST_SLICE":  "a,b,c",
	}, "Default Source")

	envx.DefaultResolver = envx.NewResolver(mapSource)

	// Test loading with default resolver
	var config ResolverTestConfig
	err := envx.Load(&config)

	assert.NoError(t, err)
	assert.Equal(t, "default_value", config.String)
	assert.Equal(t, 100, config.Int)
	assert.Equal(t, false, config.Boolean)
	assert.Equal(t, []string{"a", "b", "c"}, config.Slice)
}

func TestMultipleSourcesWithStructLoader(t *testing.T) {
	// First source has higher priority
	highPrioritySource := envx.NewMapSource(map[string]string{
		"TEST_STRING": "high_priority",
		"TEST_INT":    "999",
	}, "High Priority")

	// Second source has lower priority
	lowPrioritySource := envx.NewMapSource(map[string]string{
		"TEST_STRING": "low_priority", // Should be shadowed by first source
		"TEST_BOOL":   "true",         // Only exists in second source
		"TEST_SLICE":  "x,y,z",        // Only exists in second source
	}, "Low Priority")

	// Create resolver with both sources
	resolver := envx.NewResolver(highPrioritySource, lowPrioritySource)

	// Test loading with multi-source resolver
	var config ResolverTestConfig
	err := envx.Load(&config, envx.WithResolver(resolver))

	assert.NoError(t, err)
	assert.Equal(t, "high_priority", config.String)        // From high priority source
	assert.Equal(t, 999, config.Int)                       // From high priority source
	assert.Equal(t, true, config.Boolean)                  // From low priority source
	assert.Equal(t, []string{"x", "y", "z"}, config.Slice) // From low priority source
}
