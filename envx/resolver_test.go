package envx_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/velmie/x/envx"
)

func TestStandardResolverWithSingleSource(t *testing.T) {
	mapSource := envx.NewMapSource(map[string]string{
		"EXISTING_VAR": "test_value",
		"EMPTY_VAR":    "",
	}, "Test Source")

	resolver := envx.NewResolver(mapSource)

	// Test existing variable
	result, err := resolver.Get("EXISTING_VAR")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "test_value", result.Val)

	// Test non-existing variable
	result, err = resolver.Get("NON_EXISTENT_VAR")
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)

	// Test empty variable
	result, err = resolver.Get("EMPTY_VAR")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "", result.Val)
}

func TestStandardResolverWithMultipleSources(t *testing.T) {
	mapSource1 := envx.NewMapSource(map[string]string{
		"VAR1": "source1_value",
		"VAR3": "source1_value_for_var3",
	}, "Source 1")

	mapSource2 := envx.NewMapSource(map[string]string{
		"VAR2": "source2_value",
		"VAR3": "source2_value_for_var3", // This should be shadowed by Source 1
	}, "Source 2")

	// Create resolver with sources in priority order (source1 has higher priority)
	resolver := envx.NewResolver(mapSource1, mapSource2)

	// Test variable from first source
	result, err := resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "source1_value", result.Val)

	// Test variable from second source
	result, err = resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "source2_value", result.Val)

	// Test variable exists in both sources (should take from first source)
	result, err = resolver.Get("VAR3")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "source1_value_for_var3", result.Val)
}

func TestStandardResolverCoalesce(t *testing.T) {
	mapSource := envx.NewMapSource(map[string]string{
		"VAR2": "value_for_var2",
		"VAR3": "value_for_var3",
	}, "Test Source")

	resolver := envx.NewResolver(mapSource)

	// Test Coalesce when first variable doesn't exist but second does
	result, err := resolver.Coalesce("VAR1", "VAR2", "VAR3")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "value_for_var2", result.Val)
	assert.Equal(t, "VAR1", result.Name) // The name should be the first in the list
	assert.Equal(t, []string{"VAR1", "VAR2", "VAR3"}, result.AllNames)

	// Test Coalesce when no variables exist
	result, err = resolver.Coalesce("NON_EXISTENT1", "NON_EXISTENT2")
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)
	assert.Equal(t, "NON_EXISTENT1", result.Name)
	assert.Equal(t, []string{"NON_EXISTENT1", "NON_EXISTENT2"}, result.AllNames)

	// Test Coalesce with empty list
	result, err = resolver.Coalesce()
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)
	assert.Equal(t, "", result.Name)
	assert.Nil(t, result.AllNames)
}

// Mock source that always returns an error for testing error handling
type ErrorSource struct{}

func (ErrorSource) Lookup(key string) (string, bool, error) {
	return "", false, errors.New("simulated error")
}

func (ErrorSource) Name() string {
	return "Error Source"
}

func TestStandardResolverWithErrorSourceAndBreakOnError(t *testing.T) {
	errorSource := ErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

	// First source always errors, with default BreakOnError handler
	resolver := envx.NewResolver(errorSource, mapSource)

	// Should return error and not continue to the second source
	result, err := resolver.Get("VAR1")
	assert.Error(t, err)
	assert.Equal(t, "simulated error", err.Error())
	assert.Nil(t, result) // result should be nil when error occurs
}

func TestStandardResolverWithErrorSourceAndContinueOnError(t *testing.T) {
	errorSource := ErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

	// First source always errors, but we configure to continue on errors
	resolver := envx.NewResolver(errorSource, mapSource).WithErrorHandler(envx.ContinueOnError)

	// Should ignore error from first source and continue to the second source
	result, err := resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "fallback_value", result.Val)
}

func TestStandardResolverWithCustomErrorHandler(t *testing.T) {
	errorSource := ErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

	// Define custom error handler that wraps the error with source info
	customErrorHandler := func(err error, sourceName string) (bool, error) {
		return false, errors.New("error from " + sourceName + ": " + err.Error())
	}

	resolver := envx.NewResolver(errorSource, mapSource).WithErrorHandler(customErrorHandler)

	result, err := resolver.Get("VAR1")
	assert.Error(t, err)
	assert.Equal(t, "error from Error Source: simulated error", err.Error())
	assert.Nil(t, result)
}

func TestAddSource(t *testing.T) {
	mapSource1 := envx.NewMapSource(map[string]string{
		"VAR1": "original_value",
	}, "Original Source")

	resolver := envx.NewResolver(mapSource1)

	// Initially, resolver only has the first source
	result, err := resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.False(t, result.Exist)

	// Add a second source with VAR2
	mapSource2 := envx.NewMapSource(map[string]string{
		"VAR2": "added_value",
	}, "Added Source")

	resolver.AddSource(mapSource2)

	// Now VAR2 should be available
	result, err = resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "added_value", result.Val)

	// VAR1 should still be available from the first source
	result, err = resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "original_value", result.Val)
}

// Test the DefaultResolver with backward compatibility
func TestBackwardCompatibility(t *testing.T) {
	// Add a mock source that always errors
	envx.DefaultResolver.AddSource(ErrorSource{})

	// Despite the error from ErrorSource, Get and Coalesce should not return error
	result := envx.Get("ANY_VAR")
	assert.False(t, result.Exist)

	result = envx.Coalesce("ANY_VAR1", "ANY_VAR2")
	assert.False(t, result.Exist)
}
