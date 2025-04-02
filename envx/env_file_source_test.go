package envx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvFileSource(t *testing.T) {
	// Create a temporary .env file for testing
	tempDir := t.TempDir()
	envFilePath := filepath.Join(tempDir, ".env")

	// Create test content
	testContent := `
# This is a comment
KEY1=value1
KEY2="quoted value"
KEY3='another quoted value'
KEY_EMPTY=
   SPACES_KEY   =   spaces value   
`
	err := os.WriteFile(envFilePath, []byte(testContent), 0644)
	require.NoError(t, err)

	// Create the source
	source, err := NewEnvFileSource(envFilePath)
	require.NoError(t, err)
	require.NotNil(t, source)

	// Test Name method
	assert.Equal(t, "env-file["+envFilePath+"]", source.Name())

	// Test Lookup for various keys
	t.Run("lookup existing keys", func(t *testing.T) {
		testCases := []struct {
			key      string
			expected string
		}{
			{"KEY1", "value1"},
			{"KEY2", "quoted value"},
			{"KEY3", "another quoted value"},
			{"KEY_EMPTY", ""},
			{"SPACES_KEY", "spaces value"},
		}

		for _, tc := range testCases {
			value, found, err := source.Lookup(tc.key)
			assert.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, tc.expected, value)
		}
	})

	t.Run("lookup non-existent key", func(t *testing.T) {
		value, found, err := source.Lookup("NON_EXISTENT_KEY")
		assert.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, "", value)
	})

	t.Run("invalid file", func(t *testing.T) {
		// Create a file with invalid content
		invalidFilePath := filepath.Join(tempDir, "invalid.env")
		err := os.WriteFile(invalidFilePath, []byte("INVALID_LINE\nKEY=VALUE"), 0644)
		require.NoError(t, err)

		_, err = NewEnvFileSource(invalidFilePath)
		assert.Error(t, err)
	})

	t.Run("reload", func(t *testing.T) {
		// Modify the file
		newContent := "KEY1=new_value\nNEW_KEY=brand_new"
		err := os.WriteFile(envFilePath, []byte(newContent), 0644)
		require.NoError(t, err)

		// Reload the source
		err = source.Reload()
		assert.NoError(t, err)

		// Check if values were updated
		value, found, err := source.Lookup("KEY1")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "new_value", value)

		value, found, err = source.Lookup("NEW_KEY")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "brand_new", value)

		// Original values should no longer exist
		_, found, err = source.Lookup("KEY2")
		assert.NoError(t, err)
		assert.False(t, found)
	})
}
