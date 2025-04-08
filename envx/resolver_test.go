package envx_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velmie/x/envx"
)

type MockSource struct {
	name   string
	data   map[string]string
	lookup func(key string) (string, bool, error)
	err    error
}

func NewMockSource(name string, data map[string]string) *MockSource {
	return &MockSource{
		name: name,
		data: data,
	}
}

func (m *MockSource) Lookup(key string) (string, bool, error) {
	if m.err != nil {
		return "", false, m.err
	}
	if m.lookup != nil {
		return m.lookup(key)
	}
	val, ok := m.data[key]
	return val, ok, nil
}

func (m *MockSource) Name() string {
	return m.name
}

func (m *MockSource) SetError(err error) {
	m.err = err
}

type MockLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *MockLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, fmt.Sprintf("WARN: %s %v", msg, args))
}

func (l *MockLogger) Logs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	logsCopy := make([]string, len(l.logs))
	copy(logsCopy, l.logs)
	return logsCopy
}

func (l *MockLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = nil
}

func TestAddSourceWithOptions(t *testing.T) {
	resolver := envx.NewResolver()

	src1 := NewMockSource("s1", map[string]string{"VAR1": "val1"})
	src2 := NewMockSource("s2", map[string]string{"VAR2": "val2"})
	src3 := NewMockSource("s3", map[string]string{"VAR3": "val3"})
	src4 := NewMockSource("s4", map[string]string{"VAR4": "val4"})

	resolver.AddSource(src1) // No options
	resolver.AddSource(src2, envx.WithLabels("env", "local"))
	resolver.AddSource(src3, envx.IsExplicitOnly(), envx.WithLabels("vault"))
	resolver.AddSource(src4, envx.WithLabels("file", "local"), envx.IsExplicitOnly())

	plan1 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR1"}}} // Should use src1, src2 (non-explicit)
	v, err := resolver.ResolvePlan(plan1)
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "val1", v.Val)

	plan2 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR2"}}} // Should use src1, src2
	v, err = resolver.ResolvePlan(plan2)
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "val2", v.Val)

	plan3 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR3"}}} // Should use src1, src2 (won't find)
	v, err = resolver.ResolvePlan(plan3)
	require.NoError(t, err)
	assert.False(t, v.Exist)

	plan4 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR3", Labels: []string{"vault"}}}} // Should use src3
	v, err = resolver.ResolvePlan(plan4)
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "val3", v.Val)

	plan5 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR4", Labels: []string{"local"}}}} // Should use src2, src4
	v, err = resolver.ResolvePlan(plan5)
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "val4", v.Val) // Found in src4

	plan6 := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR2", Labels: []string{"local"}}}} // Should use src2, src4
	v, err = resolver.ResolvePlan(plan6)
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "val2", v.Val) // Found in src2
}

func TestResolvePlan(t *testing.T) {
	srcEnv := NewMockSource("env", map[string]string{"VAR_A": "env_a", "VAR_B": "env_b", "COMMON": "env_common"})
	srcFile := NewMockSource("file", map[string]string{"VAR_B": "file_b", "VAR_C": "file_c", "COMMON": "file_common"})
	srcVault := NewMockSource("vault", map[string]string{"VAR_C": "vault_c", "VAR_D": "vault_d", "COMMON": "vault_common"})
	srcAPI := NewMockSource("api", map[string]string{"VAR_D": "api_d", "VAR_E": "api_e", "COMMON": "api_common"})
	mockLogger := &MockLogger{}

	resolver := envx.NewResolver().WithLogger(mockLogger)
	resolver.AddSource(srcEnv, envx.WithLabels("env", "local"))                             // Non-explicit
	resolver.AddSource(srcFile, envx.WithLabels("file", "local"))                           // Non-explicit
	resolver.AddSource(srcVault, envx.WithLabels("vault", "secure"), envx.IsExplicitOnly()) // Explicit
	resolver.AddSource(srcAPI, envx.WithLabels("api", "remote"), envx.IsExplicitOnly())     // Explicit

	tests := []struct {
		name           string
		plan           envx.SearchPlan
		expectedVal    string
		expectedExist  bool
		expectedLog    bool
		expectedLogMsg string
	}{
		{
			name:          "Step 1: No labels - Find in first non-explicit (env)",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_A"}}},
			expectedVal:   "env_a",
			expectedExist: true,
		},
		{
			name:          "Step 1: No labels - Find in second non-explicit (file)",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_C"}}}, // VAR_C only in file (non-explicit), vault (explicit)
			expectedVal:   "file_c",
			expectedExist: true,
		},
		{
			name:          "Step 1: No labels - Not found (only in explicit)",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_D"}}},
			expectedExist: false,
		},
		{
			name:          "Step 1: Specific label 'vault' - Find in explicit source",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_D", Labels: []string{"vault"}}}},
			expectedVal:   "vault_d",
			expectedExist: true,
		},
		{
			name:          "Step 1: Specific label 'api' - Find in explicit source",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_E", Labels: []string{"api"}}}},
			expectedVal:   "api_e",
			expectedExist: true,
		},
		{
			name:          "Step 1: Specific label 'local' - Find in first matching (env)",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_B", Labels: []string{"local"}}}},
			expectedVal:   "env_b",
			expectedExist: true,
		},
		{
			name:          "Step 1: Specific label 'local' - Find in second matching (file)",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_C", Labels: []string{"local"}}}}, // VAR_C is in file (local) and vault (not local)
			expectedVal:   "file_c",
			expectedExist: true,
		},
		{
			name:          "Step 1: Multiple labels - Find in vault",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_C", Labels: []string{"secure", "file"}}}}, // Check vault (secure) and file
			expectedVal:   "file_c",                                                                                       // Should find in file first based on AddSource order
			expectedExist: true,
		},
		{
			name:          "Step 1: Multiple labels - Find in api",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_D", Labels: []string{"remote", "nonexistent"}}}}, // Check api (remote)
			expectedVal:   "api_d",
			expectedExist: true,
		},
		{
			name:           "Step 1: Label not matching any source - Log warning",
			plan:           envx.SearchPlan{Steps: []envx.SearchStep{{Name: "VAR_A", Labels: []string{"nonexistent"}}}},
			expectedExist:  false,
			expectedLog:    true,
			expectedLogMsg: `WARN: no sources matched labels [[nonexistent]]`,
		},
		{
			name: "Multi-step: Find in step 1 (explicit)",
			plan: envx.SearchPlan{Steps: []envx.SearchStep{
				{Name: "VAR_D", Labels: []string{"vault"}},
				{Name: "VAR_A"}, // Should not be reached
			}},
			expectedVal:   "vault_d",
			expectedExist: true,
		},
		{
			name: "Multi-step: Find in step 2 (no label)",
			plan: envx.SearchPlan{Steps: []envx.SearchStep{
				{Name: "NON_EXISTENT", Labels: []string{"vault"}},
				{Name: "VAR_A"}, // Found here in non-explicit env
			}},
			expectedVal:   "env_a",
			expectedExist: true,
		},
		{
			name: "Multi-step: Find in step 3 (mixed labels)",
			plan: envx.SearchPlan{Steps: []envx.SearchStep{
				{Name: "VAR_E", Labels: []string{"vault"}},       // Not found
				{Name: "VAR_D", Labels: []string{"nonexistent"}}, // Not found
				{Name: "VAR_C", Labels: []string{"local"}},       // Found here in file
				{Name: "VAR_A"}, // Should not be reached
			}},
			expectedVal:   "file_c",
			expectedExist: true,
		},
		{
			name: "Multi-step: Prioritize first found value based on step order",
			plan: envx.SearchPlan{Steps: []envx.SearchStep{
				{Name: "COMMON", Labels: []string{"api"}}, // api_common
				{Name: "COMMON", Labels: []string{"vault"}},
				{Name: "COMMON", Labels: []string{"local"}},
				{Name: "COMMON"},
			}},
			expectedVal:   "api_common",
			expectedExist: true,
		},
		{
			name: "Multi-step: Prioritize first found value based on step order (fallback)",
			plan: envx.SearchPlan{Steps: []envx.SearchStep{
				{Name: "NON_EXISTENT", Labels: []string{"api"}},
				{Name: "NON_EXISTENT", Labels: []string{"vault"}},
				{Name: "COMMON", Labels: []string{"local"}}, // env_common (found first in env)
				{Name: "COMMON"},
			}},
			expectedVal:   "env_common",
			expectedExist: true,
		},
		{
			name:          "Empty plan",
			plan:          envx.SearchPlan{},
			expectedExist: false,
		},
		{
			name:          "Plan with empty steps",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{}},
			expectedExist: false,
		},
		{
			name:          "Variable with empty value",
			plan:          envx.SearchPlan{Steps: []envx.SearchStep{{Name: "EMPTY_VAR"}}},
			expectedExist: false, // ResolvePlan only returns exist=true for NON-EMPTY values
		},
	}

	srcEnv.data["EMPTY_VAR"] = ""

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLogger.Reset()
			v, err := resolver.ResolvePlan(tt.plan)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedExist, v.Exist, "Exist flag mismatch")
			if tt.expectedExist {
				assert.Equal(t, tt.expectedVal, v.Val, "Value mismatch")
				if len(tt.plan.Steps) > 0 {
					assert.Equal(t, tt.plan.Steps[0].Name, v.Name, "Primary name mismatch")
				}
			} else {
				assert.Equal(t, "", v.Val, "Value should be empty when not found")
			}

			logs := mockLogger.Logs()
			if tt.expectedLog {
				assert.NotEmpty(t, logs, "Expected log message but none found")
				assert.Contains(t, logs[0], tt.expectedLogMsg, "Log message mismatch")
			} else {
				assert.Empty(t, logs, "Expected no log messages but found some")
			}

			expectedAllNames := make([]string, 0, len(tt.plan.Steps))
			for _, step := range tt.plan.Steps {
				expectedAllNames = append(expectedAllNames, step.Name)
			}
			assert.Equal(t, expectedAllNames, v.AllNames, "AllNames mismatch")
		})
	}
}

func TestGetUsesNonExplicitOnly(t *testing.T) {
	srcEnv := NewMockSource("env", map[string]string{"VAR_A": "env_a"})
	srcFile := NewMockSource("file", map[string]string{"VAR_B": "file_b"})
	srcVault := NewMockSource("vault", map[string]string{"VAR_C": "vault_c"}) // Explicit

	resolver := envx.NewResolver()
	resolver.AddSource(srcEnv)                                                // Non-explicit
	resolver.AddSource(srcFile)                                               // Non-explicit
	resolver.AddSource(srcVault, envx.WithLabels("v"), envx.IsExplicitOnly()) // Explicit

	v, err := resolver.Get("VAR_A")
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "env_a", v.Val)

	v, err = resolver.Get("VAR_B")
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "file_b", v.Val)

	v, err = resolver.Get("VAR_C")
	require.NoError(t, err)
	assert.False(t, v.Exist)
}

func TestCoalesceUsesNonExplicitOnly(t *testing.T) {
	srcEnv := NewMockSource("env", map[string]string{"VAR_A": "env_a"})
	srcFile := NewMockSource("file", map[string]string{"VAR_B": "file_b"})
	srcVault := NewMockSource("vault", map[string]string{"VAR_C": "vault_c"}) // Explicit

	resolver := envx.NewResolver()
	resolver.AddSource(srcEnv)                                                // Non-explicit
	resolver.AddSource(srcFile)                                               // Non-explicit
	resolver.AddSource(srcVault, envx.WithLabels("v"), envx.IsExplicitOnly()) // Explicit

	v, err := resolver.Coalesce("NON_EXISTENT", "VAR_C", "VAR_B", "VAR_A")
	require.NoError(t, err)
	assert.True(t, v.Exist)
	assert.Equal(t, "file_b", v.Val)
	assert.Equal(t, "NON_EXISTENT", v.Name)
	assert.Equal(t, []string{"NON_EXISTENT", "VAR_C", "VAR_B", "VAR_A"}, v.AllNames)

	v, err = resolver.Coalesce("VAR_C", "ANOTHER_NON_EXISTENT")
	require.NoError(t, err)
	assert.False(t, v.Exist)
	assert.Equal(t, "VAR_C", v.Name)
	assert.Equal(t, []string{"VAR_C", "ANOTHER_NON_EXISTENT"}, v.AllNames)
}

func TestResolverErrorHandlingWithPlan(t *testing.T) {
	srcOK := NewMockSource("ok", map[string]string{"VAR_A": "val_a"})
	srcErr := NewMockSource("err", nil)
	srcErr.SetError(errors.New("source unavailable"))
	srcFallback := NewMockSource("fallback", map[string]string{"VAR_A": "fallback_a", "VAR_B": "fallback_b"})

	plan := envx.SearchPlan{Steps: []envx.SearchStep{
		{Name: "VAR_A"},
		{Name: "VAR_B"},
	}}

	t.Run("BreakOnError (Default)", func(t *testing.T) {
		resolver := envx.NewResolver()
		resolver.AddSource(srcErr)
		resolver.AddSource(srcOK) // Should not be reached

		_, err := resolver.ResolvePlan(plan) // Error occurs looking for VAR_A in srcErr
		require.Error(t, err)
		assert.Equal(t, "source unavailable", err.Error())
	})

	t.Run("ContinueOnError", func(t *testing.T) {
		resolver := envx.NewResolver().WithErrorHandler(envx.ContinueOnError)
		resolver.AddSource(srcErr)
		resolver.AddSource(srcOK)
		resolver.AddSource(srcFallback)

		v, err := resolver.ResolvePlan(plan)
		require.NoError(t, err)
		assert.True(t, v.Exist)
		assert.Equal(t, "val_a", v.Val)

		planNotFoundFirst := envx.SearchPlan{Steps: []envx.SearchStep{
			{Name: "NON_EXISTENT"},
			{Name: "VAR_B"},
		}}
		v, err = resolver.ResolvePlan(planNotFoundFirst)
		require.NoError(t, err)
		assert.True(t, v.Exist)
		assert.Equal(t, "fallback_b", v.Val)
		assert.Equal(t, "NON_EXISTENT", v.Name)
	})
}

func TestResolverWithLogger(t *testing.T) {
	mockLogger := &MockLogger{}
	resolver := envx.NewResolver().WithLogger(mockLogger)
	src1 := NewMockSource("s1", nil)
	resolver.AddSource(src1, envx.WithLabels("present"))

	plan := envx.SearchPlan{Steps: []envx.SearchStep{{Name: "ANY", Labels: []string{"missing"}}}}
	_, err := resolver.ResolvePlan(plan)
	require.NoError(t, err)

	logs := mockLogger.Logs()
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0], "WARN: no sources matched labels")
	assert.Contains(t, logs[0], "[missing]")

	mockLogger.Reset()
	plan = envx.SearchPlan{Steps: []envx.SearchStep{{Name: "ANY", Labels: []string{"present"}}}}
	_, err = resolver.ResolvePlan(plan)
	require.NoError(t, err)
	assert.Empty(t, mockLogger.Logs())
}

type TestErrorSource struct{}

func (TestErrorSource) Lookup(key string) (string, bool, error) {
	return "", false, errors.New("simulated error")
}

func (TestErrorSource) Name() string {
	return "Error Source"
}

func TestStandardResolverWithSingleSource(t *testing.T) {
	mapSource := envx.NewMapSource(map[string]string{
		"EXISTING_VAR": "test_value",
		"EMPTY_VAR":    "",
	}, "Test Source")

	resolver := envx.NewResolver(mapSource)

	result, err := resolver.Get("EXISTING_VAR")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "test_value", result.Val)

	result, err = resolver.Get("NON_EXISTENT_VAR")
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)

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

	resolver := envx.NewResolver(mapSource1, mapSource2)

	result, err := resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "source1_value", result.Val)

	result, err = resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "source2_value", result.Val)

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

	result, err := resolver.Coalesce("VAR1", "VAR2", "VAR3")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "value_for_var2", result.Val)
	assert.Equal(t, "VAR1", result.Name)
	assert.Equal(t, []string{"VAR1", "VAR2", "VAR3"}, result.AllNames)

	result, err = resolver.Coalesce("NON_EXISTENT1", "NON_EXISTENT2")
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)
	assert.Equal(t, "NON_EXISTENT1", result.Name)
	assert.Equal(t, []string{"NON_EXISTENT1", "NON_EXISTENT2"}, result.AllNames)

	result, err = resolver.Coalesce()
	assert.NoError(t, err)
	assert.False(t, result.Exist)
	assert.Equal(t, "", result.Val)
	assert.Nil(t, result.AllNames)
}

func TestStandardResolverWithErrorSourceAndBreakOnError(t *testing.T) {
	errorSource := TestErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

	resolver := envx.NewResolver(errorSource, mapSource)

	result, err := resolver.Get("VAR1")
	assert.Error(t, err)
	assert.Equal(t, "simulated error", err.Error())
	assert.Nil(t, result)
}

func TestStandardResolverWithErrorSourceAndContinueOnError(t *testing.T) {
	errorSource := TestErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

	resolver := envx.NewResolver(errorSource, mapSource).WithErrorHandler(envx.ContinueOnError)

	result, err := resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "fallback_value", result.Val)
}

func TestStandardResolverWithCustomErrorHandler(t *testing.T) {
	errorSource := TestErrorSource{}
	mapSource := envx.NewMapSource(map[string]string{
		"VAR1": "fallback_value",
	}, "Fallback Source")

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

	result, err := resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.False(t, result.Exist)

	mapSource2 := envx.NewMapSource(map[string]string{
		"VAR2": "added_value",
	}, "Added Source")

	resolver.AddSource(mapSource2)

	result, err = resolver.Get("VAR2")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "added_value", result.Val)

	result, err = resolver.Get("VAR1")
	assert.NoError(t, err)
	assert.True(t, result.Exist)
	assert.Equal(t, "original_value", result.Val)
}
