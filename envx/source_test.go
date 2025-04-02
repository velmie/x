package envx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/velmie/x/envx"
)

func TestEnvSource(t *testing.T) {
	source := envx.EnvSource{}

	t.Setenv("TEST_ENV_VAR", "test_value")

	// Test existing variable
	val, found, err := source.Lookup("TEST_ENV_VAR")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "test_value", val)

	// Test non-existing variable
	val, found, err = source.Lookup("NON_EXISTENT_VAR")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", val)

	// Test source name
	assert.Equal(t, "Environment", source.Name())
}

func TestMapSource(t *testing.T) {
	data := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}

	source := envx.NewMapSource(data, "Test Map")

	// Test existing key
	val, found, err := source.Lookup("KEY1")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value1", val)

	// Test non-existing key
	val, found, err = source.Lookup("KEY3")
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", val)

	// Test source name
	assert.Equal(t, "Test Map", source.Name())

	// Test default name
	defaultSource := envx.NewMapSource(data, "")
	assert.Equal(t, "Map", defaultSource.Name())
}
