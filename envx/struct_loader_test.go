package envx_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velmie/x/envx"
)

func setupTestEnv(vars map[string]string) func() {
	originalValues := make(map[string]string)
	var setVars []string

	for k, v := range vars {
		original, existed := os.LookupEnv(k)
		if existed {
			originalValues[k] = original
		}
		os.Setenv(k, v)
		setVars = append(setVars, k)
	}

	return func() {
		for _, k := range setVars {
			original, existed := originalValues[k]
			if existed {
				os.Setenv(k, original)
			} else {
				os.Unsetenv(k)
			}
		}
	}
}

func newTestResolver() (*envx.StandardResolver, *MockSource, *MockSource, *MockSource, *MockSource) {
	srcEnv := NewMockSource("env", map[string]string{})
	srcFile := NewMockSource("file", map[string]string{})
	srcVault := NewMockSource("vault", map[string]string{})
	srcAPI := NewMockSource("api", map[string]string{})

	resolver := envx.NewResolver()
	resolver.AddSource(srcEnv, envx.WithLabels("env", "local"))
	resolver.AddSource(srcFile, envx.WithLabels("file", "local"))
	resolver.AddSource(srcVault, envx.WithLabels("vault", "secure"), envx.IsExplicitOnly())
	resolver.AddSource(srcAPI, envx.WithLabels("api", "remote"), envx.IsExplicitOnly())

	return resolver, srcEnv, srcFile, srcVault, srcAPI
}

func TestTagParserWithLabels(t *testing.T) {
	parser := envx.NewTagParser()

	tests := []struct {
		tag         string
		expectNames []string
		expectDirs  int
	}{
		{"VAR1[env],VAR2", []string{"VAR1", "VAR2"}, 0},
		{"VAR1[env];required", []string{"VAR1"}, 1},
		{"VAR1[env], VAR2[file]; required ; default(abc)", []string{"VAR1", "VAR2"}, 2},
		{"'QUOTED'[env], NORMAL", []string{"'QUOTED'", "NORMAL"}, 0},
		{";required", []string{}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			tagInfo, err := parser.Parse(tt.tag)
			require.NoError(t, err)
			assert.Equal(t, tt.expectNames, tagInfo.Names, "Names mismatch")
			assert.Len(t, tagInfo.Directives, tt.expectDirs, "Directives count mismatch")
		})
	}
}

func TestStructLoaderWithLabels(t *testing.T) {
	resolver, srcEnv, srcFile, srcVault, srcAPI := newTestResolver()

	srcEnv.data = map[string]string{
		"FROM_ENV":           "env_val",
		"FROM_MULTI":         "env_multi",
		"FROM_DEF":           "env_def",
		"EMPTY_BRACKETS_VAL": "env_empty_brackets",
	}
	srcFile.data = map[string]string{
		"FROM_FILE":  "file_val",
		"FROM_MULTI": "file_multi",
		"FROM_DEF":   "file_def",
	}
	srcVault.data = map[string]string{
		"FROM_VAULT": "vault_val",
		"FROM_MULTI": "vault_multi",
		"FROM_DEF":   "vault_def",
	}
	srcAPI.data = map[string]string{
		"FROM_API": "api_val",
		"FROM_DEF": "api_def",
	}

	type Config struct {
		EnvOnly       string `env:"FROM_ENV[env]"`
		FileOnly      string `env:"FROM_FILE[file]"`
		VaultOnly     string `env:"FROM_VAULT[vault]"`
		ApiOnly       string `env:"FROM_API[api]"`
		LocalMulti    string `env:"FROM_MULTI[local]"`
		SecureMulti   string `env:"FROM_MULTI[secure]"`
		Default       string `env:"FROM_DEF"`
		DefaultReq    string `env:"FROM_DEF;required"`
		VaultReq      string `env:"FROM_VAULT[vault];required"`
		NotFound      string `env:"NON_EXISTENT[file]"`
		NotFoundDef   string `env:"NON_EXISTENT_DEF"`
		Implicit      string
		EmptyBrackets string `env:"EMPTY_BRACKETS_VAL[]"`
	}
	srcEnv.data["IMPLICIT"] = "implicit_env"

	var cfg Config
	err := envx.Load(&cfg, envx.WithResolver(resolver))

	require.NoError(t, err)
	assert.Equal(t, "env_val", cfg.EnvOnly)
	assert.Equal(t, "file_val", cfg.FileOnly)
	assert.Equal(t, "vault_val", cfg.VaultOnly)
	assert.Equal(t, "api_val", cfg.ApiOnly)
	assert.Equal(t, "env_multi", cfg.LocalMulti)
	assert.Equal(t, "vault_multi", cfg.SecureMulti)
	assert.Equal(t, "env_def", cfg.Default)
	assert.Equal(t, "env_def", cfg.DefaultReq)
	assert.Equal(t, "vault_val", cfg.VaultReq)
	assert.Equal(t, "", cfg.NotFound)
	assert.Equal(t, "", cfg.NotFoundDef)
	assert.Equal(t, "implicit_env", cfg.Implicit)
	assert.Equal(t, "env_empty_brackets", cfg.EmptyBrackets)
}

func TestStructLoaderWithLabelsAndMissingRequired(t *testing.T) {
	resolver, _, _, _, _ := newTestResolver()

	type Config struct {
		MissingLabelReq string `env:"MISSING_VAR[vault];required"`
		MissingDefReq   string `env:"MISSING_DEF;required"`
		PresentDefReq   string `env:"PRESENT_DEF;required"`
	}
	resolver.AddSource(NewMockSource("env", map[string]string{"PRESENT_DEF": "present"}), envx.WithLabels("env"))

	var cfg Config
	err := envx.Load(&cfg, envx.WithResolver(resolver))

	require.Error(t, err)
	errText := err.Error()
	assert.Contains(t, errText, `"MISSING_VAR" is not set`)
	assert.Contains(t, errText, `"MISSING_DEF" is not set`)
	assert.NotContains(t, errText, "PRESENT_DEF")
}

func TestStructLoaderLabelInteractionWithPrefix(t *testing.T) {
	resolver, srcEnv, srcFile, srcVault, _ := newTestResolver()

	t.Run("Mixed Tags with Prefix", func(t *testing.T) {
		srcVault.data["SECRET"] = "vault_secret"
		srcEnv.data["P_SIMPLE"] = "prefixed_simple_env"
		srcFile.data["SIMPLE"] = "unprefixed_simple_file"

		type ConfigMixed struct {
			Secret string `env:"SECRET[vault]"`
			Simple string `env:"SIMPLE"`
		}

		var cfg ConfigMixed
		err := envx.Load(&cfg, envx.WithPrefix("P_"), envx.WithResolver(resolver), envx.WithPrefixFallback(true))

		require.NoError(t, err)
		assert.Equal(t, "vault_secret", cfg.Secret)
		assert.Equal(t, "prefixed_simple_env", cfg.Simple)
	})

	t.Run("No Labels with Prefix", func(t *testing.T) {
		srcEnv.data["P_VAR1"] = "prefixed_var1_env"
		srcFile.data["VAR2"] = "unprefixed_var2_file"

		type ConfigNoLabels struct {
			Var1 string `env:"VAR1"`
			Var2 string `env:"VAR2"`
		}

		var cfg ConfigNoLabels
		err := envx.Load(&cfg, envx.WithPrefix("P_"), envx.WithResolver(resolver), envx.WithPrefixFallback(true))

		require.NoError(t, err)
		assert.Equal(t, "prefixed_var1_env", cfg.Var1)
		assert.Equal(t, "unprefixed_var2_file", cfg.Var2)
	})

	t.Run("Nested Mixed Tags with Prefix", func(t *testing.T) {
		srcVault.data["NESTED_SECRET"] = "nested_vault_secret"
		srcEnv.data["P_NESTED_SIMPLE"] = "prefixed_nested_simple_env"
		srcEnv.data["P_OUTER_SIMPLE"] = "prefixed_outer_simple_env"

		type NestedConfig struct {
			Secret string `env:"NESTED_SECRET[vault]"`
			Simple string `env:"NESTED_SIMPLE"`
		}
		type ConfigOuter struct {
			Nested      NestedConfig
			OuterSimple string `env:"OUTER_SIMPLE"`
		}

		var cfg ConfigOuter
		err := envx.Load(&cfg, envx.WithPrefix("P_"), envx.WithResolver(resolver), envx.WithPrefixFallback(true))

		require.NoError(t, err)
		assert.Equal(t, "nested_vault_secret", cfg.Nested.Secret)
		assert.Equal(t, "prefixed_nested_simple_env", cfg.Nested.Simple)
		assert.Equal(t, "prefixed_outer_simple_env", cfg.OuterSimple)
	})
}

func TestStructLoaderBackwardCompatibility(t *testing.T) {
	resolver, srcEnv, srcFile, srcVault, _ := newTestResolver()

	srcEnv.data["VAR_A"] = "env_a"
	srcFile.data["VAR_B"] = "file_b"
	srcVault.data["VAR_C"] = "vault_c"

	type Config struct {
		VarA string `env:"VAR_A"`
		VarB string `env:"VAR_B,VAR_C"`
	}

	var cfg Config
	err := envx.Load(&cfg, envx.WithResolver(resolver))

	require.NoError(t, err)
	assert.Equal(t, "env_a", cfg.VarA)
	assert.Equal(t, "file_b", cfg.VarB)
}

func TestStructLoaderInvalidLabelSyntaxError(t *testing.T) {
	// Test direct parsing with invalid syntax
	parser := envx.NewTagParser()

	invalidTags := []struct {
		name    string
		tag     string
		errText string
	}{
		{
			name:    "Missing closing bracket",
			tag:     "VAR_NAME[invalid",
			errText: "missing closing bracket",
		},
		{
			name:    "Suffix after closing bracket",
			tag:     "VAR_NAME[valid]suffix",
			errText: "unexpected characters after closing bracket",
		},
		{
			name:    "Empty name before bracket",
			tag:     "[label]",
			errText: "empty name before labels",
		},
		{
			name:    "Invalid bracket order",
			tag:     "VAR_NAME]invalid[",
			errText: "missing closing bracket",
		},
	}

	for _, tt := range invalidTags {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse(tt.tag)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errText)
		})
	}
}

func TestStructLoaderEmptyPlanVsCoalesce(t *testing.T) {
	resolver, srcEnv, _, srcVault, _ := newTestResolver()
	prefix := "P_"
	fallbackPrefix := "F_"

	srcEnv.data[prefix+"VAR1"] = "p_var1"
	srcEnv.data[fallbackPrefix+"VAR1"] = "f_var1"
	srcEnv.data["VAR1"] = "var1"

	srcEnv.data[fallbackPrefix+"VAR2"] = "f_var2"
	srcEnv.data["VAR2"] = "var2"

	srcEnv.data["VAR3"] = "var3"

	srcVault.data["SECRET"] = "secret"

	opts := []envx.Option{
		envx.WithPrefix(prefix),
		envx.WithPrefixFallback(true),
		envx.WithFallbackPrefix(fallbackPrefix),
		envx.WithResolver(resolver),
	}

	t.Run("No Labels - Uses Coalesce", func(t *testing.T) {
		type ConfigNoLabels struct {
			Var1 string `env:"VAR1"`
			Var2 string `env:"VAR2"`
			Var3 string `env:"VAR3"`
		}
		var cfg ConfigNoLabels
		err := envx.Load(&cfg, opts...)
		require.NoError(t, err)
		assert.Equal(t, "p_var1", cfg.Var1)
		assert.Equal(t, "f_var2", cfg.Var2)
		assert.Equal(t, "var3", cfg.Var3)
	})

	t.Run("With Labels - Uses ResolvePlan", func(t *testing.T) {
		type ConfigWithLabels struct {
			Secret string `env:"SECRET[vault]"`
			Var1   string `env:"VAR1"`
			Var2   string `env:"VAR2"`
			Var3   string `env:"VAR3"`
		}
		var cfg ConfigWithLabels
		err := envx.Load(&cfg, opts...)
		require.NoError(t, err)
		assert.Equal(t, "secret", cfg.Secret)
		assert.Equal(t, "p_var1", cfg.Var1)
		assert.Equal(t, "f_var2", cfg.Var2)
		assert.Equal(t, "var3", cfg.Var3)
	})
}

func TestStructLoader_RequiredFieldsAdapt(t *testing.T) {
	resolver := envx.NewResolver(envx.EnvSource{})

	type Config struct {
		Required string `env:"REQUIRED_ADAPT;required"`
	}

	os.Unsetenv("REQUIRED_ADAPT")

	var cfg Config
	err := envx.Load(&cfg, envx.WithResolver(resolver))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not set")
	assert.Contains(t, err.Error(), "REQUIRED_ADAPT")
}

func TestDefaultResolverWithSources(t *testing.T) {
	srcOther := NewMockSource("other", map[string]string{"DEFAULT_INIT_VAR": "other_value"})

	testResolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
	testResolver.AddSource(envx.EnvSource{}, envx.WithLabels("env", "default"))
	testResolver.AddSource(srcOther, envx.WithLabels("other"), envx.IsExplicitOnly())

	originalResolver := envx.DefaultResolver
	envx.DefaultResolver = testResolver
	defer func() { envx.DefaultResolver = originalResolver }()

	cleanup := setupTestEnv(map[string]string{"DEFAULT_INIT_VAR": "actual_env_value"})
	defer cleanup()

	// Test Get uses the EnvSource (non-explicit)
	v := envx.Get("DEFAULT_INIT_VAR")
	assert.True(t, v.Exist)
	assert.Equal(t, "actual_env_value", v.Val)

	// Test Coalesce uses the EnvSource (non-explicit)
	v = envx.Coalesce("NON_EXISTENT", "DEFAULT_INIT_VAR")
	assert.True(t, v.Exist)
	assert.Equal(t, "actual_env_value", v.Val)

	// Add an explicit source and ensure Get/Coalesce ignore it
	envx.DefaultResolver.AddSource(srcOther, envx.WithLabels("other"), envx.IsExplicitOnly())
	v = envx.Get("DEFAULT_INIT_VAR")
	assert.Equal(t, "actual_env_value", v.Val)

	// Test that DefaultResolver still uses ContinueOnError
	envx.DefaultResolver.AddSource(ErrorSource{})
	v = envx.Get("DEFAULT_INIT_VAR")
	assert.True(t, v.Exist)
	assert.Equal(t, "actual_env_value", v.Val)
}

func TestDefaultResolverDirectUse(t *testing.T) {
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
