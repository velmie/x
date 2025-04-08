package envx_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velmie/x/envx"
)

// We'll use the setupTestEnv function from struct_loader_test.go

// ErrorSource is a source that always returns an error
type ErrorSource struct{}

func (s ErrorSource) Lookup(key string) (string, bool, error) {
	return "", false, errors.New("forced error from error source")
}

func (s ErrorSource) Name() string {
	return "ErrorSource"
}

func withModifiedDefaultResolver(resolver envx.Resolver, f func()) {
	oldResolver := envx.DefaultResolver
	envx.DefaultResolver = resolver
	defer func() {
		envx.DefaultResolver = oldResolver
	}()
	f()
}

func TestDefaultResolverInitialization(t *testing.T) {
	srcOther := NewMockSource("other", map[string]string{"DEFAULT_INIT_VAR": "other_value"})

	testResolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(envx.EnvSource{}, envx.WithLabels("env", "default"))
	testResolver.AddSource(srcOther, envx.WithLabels("other"), envx.IsExplicitOnly())

	withModifiedDefaultResolver(testResolver, func() {
		cleanup := setupTestEnv(map[string]string{"DEFAULT_INIT_VAR": "actual_env_value"})
		defer cleanup()

		v := envx.Get("DEFAULT_INIT_VAR")
		assert.True(t, v.Exist)
		assert.Equal(t, "actual_env_value", v.Val)

		v = envx.Coalesce("NON_EXISTENT", "DEFAULT_INIT_VAR")
		assert.True(t, v.Exist)
		assert.Equal(t, "actual_env_value", v.Val)

		envx.DefaultResolver.AddSource(srcOther, envx.WithLabels("other"), envx.IsExplicitOnly())
		v = envx.Get("DEFAULT_INIT_VAR")
		assert.Equal(t, "actual_env_value", v.Val)

		envx.DefaultResolver.AddSource(ErrorSource{})
		v = envx.Get("DEFAULT_INIT_VAR")
		assert.True(t, v.Exist)
		assert.Equal(t, "actual_env_value", v.Val)
	})
}

func TestDefaultResolverUsage(t *testing.T) {
	originalResolver := envx.DefaultResolver
	envx.DefaultResolver = envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	envx.DefaultResolver.AddSource(envx.EnvSource{}, envx.WithLabels("env", "default"))
	defer func() { envx.DefaultResolver = originalResolver }()

	key := "TEST_DEFAULT_RESOLVER_VAR"
	expectedValue := "hello world"
	cleanup := setupTestEnv(map[string]string{key: expectedValue})
	defer cleanup()

	vGet := envx.Get(key)
	require.True(t, vGet.Exist)
	assert.Equal(t, expectedValue, vGet.Val)
	valGet, errGet := vGet.String()
	require.NoError(t, errGet)
	assert.Equal(t, expectedValue, valGet)

	vCoalesce := envx.Coalesce("NON_EXISTENT_DEFAULT", key)
	require.True(t, vCoalesce.Exist)
	assert.Equal(t, expectedValue, vCoalesce.Val)
	valCoalesce, errCoalesce := vCoalesce.String()
	require.NoError(t, errCoalesce)
	assert.Equal(t, expectedValue, valCoalesce)

	vGetNE := envx.Get("NON_EXISTENT_DEFAULT_NE")
	assert.False(t, vGetNE.Exist)
}

func TestDefaultResolverWithCustomSources(t *testing.T) {
	// Save original resolver and restore after test
	originalResolver := envx.DefaultResolver
	defer func() { envx.DefaultResolver = originalResolver }()

	// Create a test resolver with custom sources
	testResolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)

	// Add different sources with different priorities
	mapSource1 := envx.NewMapSource(map[string]string{
		"MAP_VAR1":   "map1_value",
		"SHARED_VAR": "map1_value",
	}, "map1")

	mapSource2 := envx.NewMapSource(map[string]string{
		"MAP_VAR2":   "map2_value",
		"SHARED_VAR": "map2_value",
	}, "map2")

	// Add sources with different priorities (order matters)
	testResolver.AddSource(mapSource1)
	testResolver.AddSource(mapSource2)
	testResolver.AddSource(envx.EnvSource{})

	// Set the test resolver as the default
	envx.DefaultResolver = testResolver

	// Test variable from first map source
	v := envx.Get("MAP_VAR1")
	assert.True(t, v.Exist)
	assert.Equal(t, "map1_value", v.Val)

	// Test variable from second map source
	v = envx.Get("MAP_VAR2")
	assert.True(t, v.Exist)
	assert.Equal(t, "map2_value", v.Val)

	// Test shared variable (should get first one due to priority)
	v = envx.Get("SHARED_VAR")
	assert.True(t, v.Exist)
	assert.Equal(t, "map1_value", v.Val)

	// Test Coalesce with multiple variables
	v = envx.Coalesce("NON_EXISTENT", "MAP_VAR1", "MAP_VAR2")
	assert.True(t, v.Exist)
	assert.Equal(t, "map1_value", v.Val)

	// Test with environment variable that overrides map sources
	cleanup := setupTestEnv(map[string]string{"SHARED_VAR": "env_value"})
	defer cleanup()

	// Re-create resolver to pick up env changes (since EnvSource doesn't cache)
	testResolver = envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(mapSource1)
	testResolver.AddSource(mapSource2)
	testResolver.AddSource(envx.EnvSource{})
	envx.DefaultResolver = testResolver

	// Should still get map1 value because map sources are added before env source
	v = envx.Get("SHARED_VAR")
	assert.Equal(t, "map1_value", v.Val)

	// Create a new resolver with different source order
	testResolver = envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(envx.EnvSource{}) // Env source first now
	testResolver.AddSource(mapSource1)
	testResolver.AddSource(mapSource2)
	envx.DefaultResolver = testResolver

	// Now should get env value due to priority change
	v = envx.Get("SHARED_VAR")
	assert.Equal(t, "env_value", v.Val)
}

func TestDefaultResolverWithErrorHandling(t *testing.T) {
	// Save original resolver and restore after test
	originalResolver := envx.DefaultResolver
	defer func() { envx.DefaultResolver = originalResolver }()

	// Create test sources
	normalSource := envx.NewMapSource(map[string]string{
		"NORMAL_VAR": "normal_value",
	}, "normal")

	errorSource := ErrorSource{}

	// Test with ContinueOnError handler
	testResolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(errorSource) // This source will error
	testResolver.AddSource(normalSource)
	envx.DefaultResolver = testResolver

	// Should skip error source and get value from normal source
	v := envx.Get("NORMAL_VAR")
	assert.True(t, v.Exist)
	assert.Equal(t, "normal_value", v.Val)

	// Test package-level functions error handling
	v = envx.Get("SOME_VAR") // This will trigger error from errorSource but Get ignores it
	assert.False(t, v.Exist)

	v = envx.Coalesce("ERROR_VAR", "NORMAL_VAR") // Should skip error and return normal_var
	assert.True(t, v.Exist)
	assert.Equal(t, "normal_value", v.Val)
}

func TestDefaultPackageFunctions(t *testing.T) {
	// Save original resolver and restore after test
	originalResolver := envx.DefaultResolver
	defer func() { envx.DefaultResolver = originalResolver }()

	// Set up a simple test environment
	cleanup := setupTestEnv(map[string]string{
		"TEST_VAR1": "value1",
		"TEST_VAR2": "value2",
	})
	defer cleanup()

	// Create a fresh resolver that uses the environment
	testResolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(envx.EnvSource{})
	envx.DefaultResolver = testResolver

	// Test Get function
	v := envx.Get("TEST_VAR1")
	assert.True(t, v.Exist)
	assert.Equal(t, "value1", v.Val)

	// Test Get with non-existent variable
	v = envx.Get("NON_EXISTENT_VAR")
	assert.False(t, v.Exist)
	assert.Equal(t, "", v.Val)

	// Test Coalesce with first variable existing
	v = envx.Coalesce("TEST_VAR1", "TEST_VAR2")
	assert.True(t, v.Exist)
	assert.Equal(t, "value1", v.Val)

	// Test Coalesce with first variable not existing
	v = envx.Coalesce("NON_EXISTENT_VAR", "TEST_VAR2")
	assert.True(t, v.Exist)
	assert.Equal(t, "value2", v.Val)

	// Test Coalesce with no variables existing
	v = envx.Coalesce("NON_EXISTENT_VAR1", "NON_EXISTENT_VAR2")
	assert.False(t, v.Exist)
	assert.Equal(t, "", v.Val)
}
