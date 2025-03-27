package envx_test

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/velmie/x/envx"
)

func TestStructLoader_BasicTypes(t *testing.T) {
	os.Setenv("TEST_STRING", "hello")
	os.Setenv("TEST_INT", "123")
	os.Setenv("TEST_INT8", "120")
	os.Setenv("TEST_INT16", "32000")
	os.Setenv("TEST_INT32", "2000000000")
	os.Setenv("TEST_INT64", "9000000000000000000")
	os.Setenv("TEST_UINT", "123")
	os.Setenv("TEST_UINT8", "250")
	os.Setenv("TEST_UINT16", "60000")
	os.Setenv("TEST_UINT32", "4000000000")
	os.Setenv("TEST_UINT64", "18000000000000000000")
	os.Setenv("TEST_BOOL", "true")
	os.Setenv("TEST_FLOAT32", "3.14")
	os.Setenv("TEST_FLOAT64", "3.141592653589793")
	os.Setenv("TEST_DURATION", "5s")
	defer func() {
		os.Unsetenv("TEST_STRING")
		os.Unsetenv("TEST_INT")
		os.Unsetenv("TEST_INT8")
		os.Unsetenv("TEST_INT16")
		os.Unsetenv("TEST_INT32")
		os.Unsetenv("TEST_INT64")
		os.Unsetenv("TEST_UINT")
		os.Unsetenv("TEST_UINT8")
		os.Unsetenv("TEST_UINT16")
		os.Unsetenv("TEST_UINT32")
		os.Unsetenv("TEST_UINT64")
		os.Unsetenv("TEST_BOOL")
		os.Unsetenv("TEST_FLOAT32")
		os.Unsetenv("TEST_FLOAT64")
		os.Unsetenv("TEST_DURATION")
	}()

	type Config struct {
		String   string        `env:"TEST_STRING"`
		Int      int           `env:"TEST_INT"`
		Int8     int8          `env:"TEST_INT8"`
		Int16    int16         `env:"TEST_INT16"`
		Int32    int32         `env:"TEST_INT32"`
		Int64    int64         `env:"TEST_INT64"`
		Uint     uint          `env:"TEST_UINT"`
		Uint8    uint8         `env:"TEST_UINT8"`
		Uint16   uint16        `env:"TEST_UINT16"`
		Uint32   uint32        `env:"TEST_UINT32"`
		Uint64   uint64        `env:"TEST_UINT64"`
		Bool     bool          `env:"TEST_BOOL"`
		Float32  float32       `env:"TEST_FLOAT32"`
		Float64  float64       `env:"TEST_FLOAT64"`
		Duration time.Duration `env:"TEST_DURATION"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.NoError(t, err)

	assert.Equal(t, "hello", cfg.String)
	assert.Equal(t, 123, cfg.Int)
	assert.Equal(t, int8(120), cfg.Int8)
	assert.Equal(t, int16(32000), cfg.Int16)
	assert.Equal(t, int32(2000000000), cfg.Int32)
	assert.Equal(t, int64(9000000000000000000), cfg.Int64)
	assert.Equal(t, uint(123), cfg.Uint)
	assert.Equal(t, uint8(250), cfg.Uint8)
	assert.Equal(t, uint16(60000), cfg.Uint16)
	assert.Equal(t, uint32(4000000000), cfg.Uint32)
	assert.Equal(t, uint64(18000000000000000000), cfg.Uint64)
	assert.Equal(t, true, cfg.Bool)
	assert.Equal(t, float32(3.14), cfg.Float32)
	assert.Equal(t, 3.141592653589793, cfg.Float64)
	assert.Equal(t, 5*time.Second, cfg.Duration)
}

func TestStructLoader_DefaultValues(t *testing.T) {
	type Config struct {
		String    string        `env:"NONEXISTENT;default(default value)"`
		Int       int           `env:"NONEXISTENT;default(42)"`
		Bool      bool          `env:"NONEXISTENT;default(true)"`
		Float     float64       `env:"NONEXISTENT;default(3.14)"`
		Duration  time.Duration `env:"NONEXISTENT;default(10s)"`
		EmptyList []string      `env:"NONEXISTENT;default()"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.NoError(t, err)

	assert.Equal(t, "default value", cfg.String)
	assert.Equal(t, 42, cfg.Int)
	assert.Equal(t, true, cfg.Bool)
	assert.Equal(t, 3.14, cfg.Float)
	assert.Equal(t, 10*time.Second, cfg.Duration)
	assert.Empty(t, cfg.EmptyList)
}

func TestStructLoader_RequiredFields(t *testing.T) {
	type Config struct {
		Required string `env:"REQUIRED;required"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not set")
}

func TestStructLoader_NotEmpty(t *testing.T) {
	os.Setenv("EMPTY_VAL", "")
	defer os.Unsetenv("EMPTY_VAL")

	type Config struct {
		NotEmpty string `env:"EMPTY_VAL;notEmpty"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has empty value")
}

func TestStructLoader_ValidationDirectives(t *testing.T) {
	os.Setenv("VALID_URL", "https://example.com")
	os.Setenv("INVALID_URL", "not-a-url")
	os.Setenv("VALID_IP", "192.168.1.1")
	os.Setenv("INVALID_IP", "300.168.1.1")
	os.Setenv("VALID_PORT", "8080")
	os.Setenv("INVALID_PORT", "70000")
	os.Setenv("VALID_DOMAIN", "example.com")
	os.Setenv("INVALID_DOMAIN", "exam@ple.com")
	os.Setenv("VALID_LISTEN_ADDR", "127.0.0.1:8080")
	os.Setenv("INVALID_LISTEN_ADDR", "127.0.0.1")
	os.Setenv("STRING_FOR_LEN", "12345")
	os.Setenv("INT_FOR_RANGE", "50")
	os.Setenv("UINT_FOR_RANGE", "50")
	os.Setenv("FLOAT_FOR_RANGE", "50.5")
	os.Setenv("ENUM_VAL", "option1")
	os.Setenv("INVALID_ENUM_VAL", "option4")
	os.Setenv("REGEX_MATCH", "abc123")
	os.Setenv("REGEX_NO_MATCH", "123")
	defer func() {
		os.Unsetenv("VALID_URL")
		os.Unsetenv("INVALID_URL")
		os.Unsetenv("VALID_IP")
		os.Unsetenv("INVALID_IP")
		os.Unsetenv("VALID_PORT")
		os.Unsetenv("INVALID_PORT")
		os.Unsetenv("VALID_DOMAIN")
		os.Unsetenv("INVALID_DOMAIN")
		os.Unsetenv("VALID_LISTEN_ADDR")
		os.Unsetenv("INVALID_LISTEN_ADDR")
		os.Unsetenv("STRING_FOR_LEN")
		os.Unsetenv("INT_FOR_RANGE")
		os.Unsetenv("UINT_FOR_RANGE")
		os.Unsetenv("FLOAT_FOR_RANGE")
		os.Unsetenv("ENUM_VAL")
		os.Unsetenv("INVALID_ENUM_VAL")
		os.Unsetenv("REGEX_MATCH")
		os.Unsetenv("REGEX_NO_MATCH")
	}()

	t.Run("Valid Values", func(t *testing.T) {
		type ValidConfig struct {
			URL        string  `env:"VALID_URL;validURL"`
			IP         string  `env:"VALID_IP;validIP"`
			Port       string  `env:"VALID_PORT;validPort"`
			Domain     string  `env:"VALID_DOMAIN;validDomain"`
			ListenAddr string  `env:"VALID_LISTEN_ADDR;validListenAddr"`
			ExactLen   string  `env:"STRING_FOR_LEN;exactLen(5)"`
			MinLen     string  `env:"STRING_FOR_LEN;min(3)"`
			MaxLen     string  `env:"STRING_FOR_LEN;max(10)"`
			MinInt     int     `env:"INT_FOR_RANGE;min(10)"`
			MaxInt     int     `env:"INT_FOR_RANGE;max(100)"`
			RangeInt   int     `env:"INT_FOR_RANGE;range(10,100)"`
			MinUint    uint    `env:"UINT_FOR_RANGE;min(10)"`
			MaxUint    uint    `env:"UINT_FOR_RANGE;max(100)"`
			RangeUint  uint    `env:"UINT_FOR_RANGE;range(10,100)"`
			MinFloat   float64 `env:"FLOAT_FOR_RANGE;min(10.0)"`
			MaxFloat   float64 `env:"FLOAT_FOR_RANGE;max(100.0)"`
			RangeFloat float64 `env:"FLOAT_FOR_RANGE;range(10.0,100.0)"`
			Enum       string  `env:"ENUM_VAL;oneOf(option1,option2,option3)"`
			Regex      string  `env:"REGEX_MATCH;regexp(^[a-z]+[0-9]+$)"`
		}

		var cfg ValidConfig
		err := envx.Load(&cfg)
		require.NoError(t, err)

		assert.Equal(t, "https://example.com", cfg.URL)
		assert.Equal(t, "192.168.1.1", cfg.IP)
		assert.Equal(t, "8080", cfg.Port)
		assert.Equal(t, "example.com", cfg.Domain)
		assert.Equal(t, "127.0.0.1:8080", cfg.ListenAddr)
		assert.Equal(t, "12345", cfg.ExactLen)
		assert.Equal(t, "12345", cfg.MinLen)
		assert.Equal(t, "12345", cfg.MaxLen)
		assert.Equal(t, 50, cfg.MinInt)
		assert.Equal(t, 50, cfg.MaxInt)
		assert.Equal(t, 50, cfg.RangeInt)
		assert.Equal(t, uint(50), cfg.MinUint)
		assert.Equal(t, uint(50), cfg.MaxUint)
		assert.Equal(t, uint(50), cfg.RangeUint)
		assert.Equal(t, 50.5, cfg.MinFloat)
		assert.Equal(t, 50.5, cfg.MaxFloat)
		assert.Equal(t, 50.5, cfg.RangeFloat)
		assert.Equal(t, "option1", cfg.Enum)
		assert.Equal(t, "abc123", cfg.Regex)
	})

	t.Run("Invalid Values", func(t *testing.T) {
		t.Run("URL Validation", func(t *testing.T) {
			// The URL validator actually accepts any string and only validates when used to get a URL object
			type Config struct {
				URL *url.URL `env:"INVALID_URL;validURL"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "valid URL")
		})

		t.Run("IP Validation", func(t *testing.T) {
			type Config struct {
				IP string `env:"INVALID_IP;validIP"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not valid IP")
		})

		t.Run("Port Validation", func(t *testing.T) {
			type Config struct {
				Port string `env:"INVALID_PORT;validPort"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "out of port range")
		})

		t.Run("Domain Validation", func(t *testing.T) {
			type Config struct {
				Domain string `env:"INVALID_DOMAIN;validDomain"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "label contains invalid characters")
		})

		t.Run("Listen Address Validation", func(t *testing.T) {
			type Config struct {
				Addr string `env:"INVALID_LISTEN_ADDR;validListenAddr"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing port")
		})

		t.Run("Exact Length Validation", func(t *testing.T) {
			type Config struct {
				Str string `env:"STRING_FOR_LEN;exactLen(6)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be 6 characters long")
		})

		t.Run("Min Length Validation", func(t *testing.T) {
			type Config struct {
				Str string `env:"STRING_FOR_LEN;min(10)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be at least 10 characters long")
		})

		t.Run("Max Length Validation", func(t *testing.T) {
			type Config struct {
				Str string `env:"STRING_FOR_LEN;max(3)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be no more than 3 characters long")
		})

		t.Run("Min Int Validation", func(t *testing.T) {
			type Config struct {
				Val int `env:"INT_FOR_RANGE;min(100)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be greater than or equal to")
		})

		t.Run("Max Int Validation", func(t *testing.T) {
			type Config struct {
				Val int `env:"INT_FOR_RANGE;max(10)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be less than or equal to")
		})

		t.Run("Range Int Validation", func(t *testing.T) {
			type Config struct {
				Val int `env:"INT_FOR_RANGE;range(100,200)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be greater than or equal to")
		})

		t.Run("One Of Validation", func(t *testing.T) {
			type Config struct {
				Val string `env:"INVALID_ENUM_VAL;oneOf(option1,option2,option3)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be one of the following values")
		})

		t.Run("Regexp Validation", func(t *testing.T) {
			type Config struct {
				Val string `env:"REGEX_NO_MATCH;regexp(^[a-z]+[0-9]+$)"`
			}

			var cfg Config
			err := envx.Load(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not match regular expression")
		})
	})
}

func TestStructLoader_ComplexTypes(t *testing.T) {
	os.Setenv("STRING_SLICE", "a,b,c")
	os.Setenv("CUSTOM_DELIM_SLICE", "a|b|c")
	os.Setenv("INT_SLICE", "1,2,3")
	os.Setenv("INT64_SLICE", "1000000000,2000000000,3000000000")
	os.Setenv("UINT_SLICE", "1,2,3")
	os.Setenv("UINT8_SLICE", "1,2,3")
	os.Setenv("UINT16_SLICE", "1,2,3")
	os.Setenv("UINT32_SLICE", "1,2,3")
	os.Setenv("UINT64_SLICE", "1,2,3")
	os.Setenv("FLOAT32_SLICE", "1.1,2.2,3.3")
	os.Setenv("FLOAT64_SLICE", "1.1,2.2,3.3")
	os.Setenv("BOOL_SLICE", "true,false,true")
	os.Setenv("DURATION_SLICE", "1s,2m,3h")
	os.Setenv("URL_VAL", "https://example.com")
	os.Setenv("TIME_VAL", "2025-01-01T12:00:00Z")
	os.Setenv("CUSTOM_TIME_VAL", "2025-01-01")
	os.Setenv("MAP_VAL", "key1=value1,key2=value2")
	defer func() {
		os.Unsetenv("STRING_SLICE")
		os.Unsetenv("CUSTOM_DELIM_SLICE")
		os.Unsetenv("INT_SLICE")
		os.Unsetenv("INT64_SLICE")
		os.Unsetenv("UINT_SLICE")
		os.Unsetenv("UINT8_SLICE")
		os.Unsetenv("UINT16_SLICE")
		os.Unsetenv("UINT32_SLICE")
		os.Unsetenv("UINT64_SLICE")
		os.Unsetenv("FLOAT32_SLICE")
		os.Unsetenv("FLOAT64_SLICE")
		os.Unsetenv("BOOL_SLICE")
		os.Unsetenv("DURATION_SLICE")
		os.Unsetenv("URL_VAL")
		os.Unsetenv("TIME_VAL")
		os.Unsetenv("CUSTOM_TIME_VAL")
		os.Unsetenv("MAP_VAL")
	}()

	type Config struct {
		StringSlice      []string          `env:"STRING_SLICE"`
		CustomDelimSlice []string          `env:"CUSTOM_DELIM_SLICE;delimiter(|)"`
		IntSlice         []int             `env:"INT_SLICE"`
		Int64Slice       []int64           `env:"INT64_SLICE"`
		UintSlice        []uint            `env:"UINT_SLICE"`
		Uint8Slice       []uint8           `env:"UINT8_SLICE"`
		Uint16Slice      []uint16          `env:"UINT16_SLICE"`
		Uint32Slice      []uint32          `env:"UINT32_SLICE"`
		Uint64Slice      []uint64          `env:"UINT64_SLICE"`
		Float32Slice     []float32         `env:"FLOAT32_SLICE"`
		Float64Slice     []float64         `env:"FLOAT64_SLICE"`
		BoolSlice        []bool            `env:"BOOL_SLICE"`
		DurationSlice    []time.Duration   `env:"DURATION_SLICE"`
		URLValue         *url.URL          `env:"URL_VAL"`
		TimeValue        time.Time         `env:"TIME_VAL"`
		CustomTimeValue  time.Time         `env:"CUSTOM_TIME_VAL;layout(2006-01-02)"`
		MapValue         map[string]string `env:"MAP_VAL"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.NoError(t, err)

	assert.Equal(t, []string{"a", "b", "c"}, cfg.StringSlice)
	assert.Equal(t, []string{"a", "b", "c"}, cfg.CustomDelimSlice)
	assert.Equal(t, []int{1, 2, 3}, cfg.IntSlice)
	assert.Equal(t, []int64{1000000000, 2000000000, 3000000000}, cfg.Int64Slice)
	assert.Equal(t, []uint{1, 2, 3}, cfg.UintSlice)
	assert.Equal(t, []uint8{1, 2, 3}, cfg.Uint8Slice)
	assert.Equal(t, []uint16{1, 2, 3}, cfg.Uint16Slice)
	assert.Equal(t, []uint32{1, 2, 3}, cfg.Uint32Slice)
	assert.Equal(t, []uint64{1, 2, 3}, cfg.Uint64Slice)
	assert.Equal(t, []float32{1.1, 2.2, 3.3}, cfg.Float32Slice)
	assert.Equal(t, []float64{1.1, 2.2, 3.3}, cfg.Float64Slice)
	assert.Equal(t, []bool{true, false, true}, cfg.BoolSlice)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Minute, 3 * time.Hour}, cfg.DurationSlice)
	assert.Equal(t, "https://example.com", cfg.URLValue.String())
	assert.Equal(t, "2025-01-01 12:00:00 +0000 UTC", cfg.TimeValue.String())
	assert.Equal(t, "2025-01-01 00:00:00 +0000 UTC", cfg.CustomTimeValue.String())
	assert.Equal(t, map[string]string{"key1": "value1", "key2": "value2"}, cfg.MapValue)
}

func TestStructLoader_Expand(t *testing.T) {
	os.Setenv("USER_NAME", "John")
	os.Setenv("GREETING", "Hello, ${USER_NAME}!")
	defer func() {
		os.Unsetenv("USER_NAME")
		os.Unsetenv("GREETING")
	}()

	type Config struct {
		Greeting string `env:"GREETING;expand"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.NoError(t, err)

	assert.Equal(t, "Hello, John!", cfg.Greeting)
}

func TestStructLoader_CustomValidator(t *testing.T) {
	os.Setenv("PASSWORD", "weakpass")
	os.Setenv("PASSWORD_STRONG", "StrongP@ss123")
	defer func() {
		os.Unsetenv("PASSWORD")
		os.Unsetenv("PASSWORD_STRONG")
	}()

	var ValidatePasswordCalled bool
	var ValidatePasswordValue string

	// Skip the test if validateMethod is still using reflection and can't find the method
	t.Run("Using direct validation", func(t *testing.T) {
		// Setup a custom validator
		emailValidator := func(ctx *envx.FieldContext, _ envx.Directive) error {
			value, err := ctx.Variable.String()
			if err != nil {
				return err
			}

			ValidatePasswordCalled = true
			ValidatePasswordValue = value

			if len(value) < 10 {
				return errors.New("password is too weak")
			}
			return nil
		}

		type Config struct {
			Password string `env:"PASSWORD;passwordCheck"`
		}

		var cfg Config
		err := envx.Load(&cfg, envx.WithCustomValidator("passwordCheck", emailValidator))

		require.Error(t, err)
		require.True(t, ValidatePasswordCalled)
		require.Equal(t, "weakpass", ValidatePasswordValue)
		assert.Contains(t, err.Error(), "password is too weak")

		// Reset and check strong password
		ValidatePasswordCalled = false
		ValidatePasswordValue = ""

		type StrongConfig struct {
			Password string `env:"PASSWORD_STRONG;passwordCheck"`
		}

		var strongCfg StrongConfig
		err = envx.Load(&strongCfg, envx.WithCustomValidator("passwordCheck", emailValidator))

		require.NoError(t, err)
		require.True(t, ValidatePasswordCalled)
		require.Equal(t, "StrongP@ss123", ValidatePasswordValue)
	})
}

func TestStructLoader_ConditionalRequired(t *testing.T) {
	t.Run("Custom Condition Required", func(t *testing.T) {
		var checkCalled bool
		var checkResult bool

		// Create a test implementation of the required directive
		customRequiredIfHandler := func(ctx *envx.FieldContext, dir envx.Directive) error {
			checkCalled = true

			if checkResult {
				ctx.Variable = ctx.Variable.Required()
			}

			return nil
		}

		// Test when condition is true (required)
		t.Run("When Required", func(t *testing.T) {
			checkCalled = false
			checkResult = true

			type Config struct {
				Field string `env:"NON_EXISTENT;customRequired"`
			}

			var cfg Config
			err := envx.Load(&cfg, envx.WithCustomValidator("customRequired", customRequiredIfHandler))

			require.Error(t, err)
			require.True(t, checkCalled, "Condition should have been checked")
			assert.Contains(t, err.Error(), "is not set")
		})

		// Test when condition is false (not required)
		t.Run("When Not Required", func(t *testing.T) {
			checkCalled = false
			checkResult = false

			type Config struct {
				Field string `env:"NON_EXISTENT;customRequired"`
			}

			var cfg Config
			err := envx.Load(&cfg, envx.WithCustomValidator("customRequired", customRequiredIfHandler))

			require.NoError(t, err)
			require.True(t, checkCalled, "Condition should have been checked")
		})
	})
}

func TestStructLoader_PrefixHandling(t *testing.T) {
	os.Setenv("APP_VARIABLE", "prefix_value")
	os.Setenv("VARIABLE", "regular_value")
	defer func() {
		os.Unsetenv("APP_VARIABLE")
		os.Unsetenv("VARIABLE")
	}()

	type Config struct {
		Variable string `env:"VARIABLE"`
	}

	t.Run("With Prefix Without Fallback", func(t *testing.T) {
		var cfg Config
		err := envx.Load(&cfg, envx.WithPrefix("APP_"))
		require.NoError(t, err)
		assert.Equal(t, "prefix_value", cfg.Variable)
	})

	t.Run("With Prefix With Fallback", func(t *testing.T) {
		os.Unsetenv("APP_VARIABLE")

		var cfg Config
		err := envx.Load(&cfg, envx.WithPrefix("APP_"), envx.WithPrefixFallback(true))
		require.NoError(t, err)
		assert.Equal(t, "regular_value", cfg.Variable)
	})
}

func TestStructLoader_NestedStructs(t *testing.T) {
	cleanup := func() {
		// Regular nested struct vars
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("DB_USERNAME")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("API_ENDPOINT")
		os.Unsetenv("API_TIMEOUT")

		// Pointer to struct vars
		os.Unsetenv("METRICS_PATH")
		os.Unsetenv("METRICS_INTERVAL")

		// Anonymous struct vars
		os.Unsetenv("CACHE_HOST")
		os.Unsetenv("CACHE_PORT")
		os.Unsetenv("CACHE_TTL")

		// Vars without env tag
		os.Unsetenv("LOGGER_LEVEL")
		os.Unsetenv("LOGGER_OUTPUT")

		// Vars with prefix
		os.Unsetenv("APP_DB_HOST")
		os.Unsetenv("APP_DB_PORT")
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")

		// Vars with deeply nested structs
		os.Unsetenv("SYSTEM_AUTH_PROVIDER_URL")
		os.Unsetenv("SYSTEM_AUTH_PROVIDER_TIMEOUT")
		os.Unsetenv("SYSTEM_AUTH_CREDENTIALS_USERNAME")
		os.Unsetenv("SYSTEM_AUTH_CREDENTIALS_PASSWORD")
	}

	setup := func() {
		// Regular nested struct vars
		os.Setenv("DB_HOST", "localhost")
		os.Setenv("DB_PORT", "5432")
		os.Setenv("DB_USERNAME", "user")
		os.Setenv("DB_PASSWORD", "password")
		os.Setenv("API_ENDPOINT", "https://api.example.com")
		os.Setenv("API_TIMEOUT", "30s")

		// Pointer to struct vars
		os.Setenv("METRICS_PATH", "/metrics")
		os.Setenv("METRICS_INTERVAL", "15s")

		// Anonymous struct vars
		os.Setenv("CACHE_HOST", "redis")
		os.Setenv("CACHE_PORT", "6379")
		os.Setenv("CACHE_TTL", "60s")

		// Vars without env tag
		os.Setenv("LOGGER_LEVEL", "info")
		os.Setenv("LOGGER_OUTPUT", "stdout")

		// Vars with prefix
		os.Setenv("APP_DB_HOST", "app-db")
		os.Setenv("APP_DB_PORT", "3306")

		// Vars with deeply nested structs
		os.Setenv("SYSTEM_AUTH_PROVIDER_URL", "https://auth.example.com")
		os.Setenv("SYSTEM_AUTH_PROVIDER_TIMEOUT", "5s")
		os.Setenv("SYSTEM_AUTH_CREDENTIALS_USERNAME", "admin")
		os.Setenv("SYSTEM_AUTH_CREDENTIALS_PASSWORD", "secret")
	}

	t.Run("Basic Nested Structs", func(t *testing.T) {
		setup()
		defer cleanup()

		type DatabaseConfig struct {
			Host     string `env:"HOST"`
			Port     int    `env:"PORT"`
			Username string `env:"USERNAME"`
			Password string `env:"PASSWORD"`
		}

		type APIConfig struct {
			Endpoint string        `env:"ENDPOINT"`
			Timeout  time.Duration `env:"TIMEOUT"`
		}

		type Config struct {
			Database DatabaseConfig `env:"DB"`
			API      APIConfig      `env:"API"`
		}

		var cfg Config
		err := envx.Load(&cfg)
		require.NoError(t, err)

		assert.Equal(t, "localhost", cfg.Database.Host)
		assert.Equal(t, 5432, cfg.Database.Port)
		assert.Equal(t, "user", cfg.Database.Username)
		assert.Equal(t, "password", cfg.Database.Password)
		assert.Equal(t, "https://api.example.com", cfg.API.Endpoint)
		assert.Equal(t, 30*time.Second, cfg.API.Timeout)
	})

	t.Run("Pointer to Struct", func(t *testing.T) {
		setup()
		defer cleanup()

		type MetricsConfig struct {
			Path     string        `env:"PATH"`
			Interval time.Duration `env:"INTERVAL"`
		}

		type Config struct {
			Metrics *MetricsConfig `env:"METRICS"`
		}

		var cfg Config
		err := envx.Load(&cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.Metrics)

		assert.Equal(t, "/metrics", cfg.Metrics.Path)
		assert.Equal(t, 15*time.Second, cfg.Metrics.Interval)
	})

	t.Run("Anonymous Struct", func(t *testing.T) {
		setup()
		defer cleanup()

		type Config struct {
			Cache struct {
				Host string        `env:"HOST"`
				Port int           `env:"PORT"`
				TTL  time.Duration `env:"TTL"`
			} `env:"CACHE"`
		}

		var cfg Config
		err := envx.Load(&cfg)
		require.NoError(t, err)

		assert.Equal(t, "redis", cfg.Cache.Host)
		assert.Equal(t, 6379, cfg.Cache.Port)
		assert.Equal(t, 60*time.Second, cfg.Cache.TTL)
	})

	t.Run("No Tag on Struct", func(t *testing.T) {
		setup()
		defer cleanup()

		type LoggerConfig struct {
			Level  string `env:"LOGGER_LEVEL"`
			Output string `env:"LOGGER_OUTPUT"`
		}

		type Config struct {
			Logger LoggerConfig // No env tag on this struct field
		}

		var cfg Config
		err := envx.Load(&cfg)
		require.NoError(t, err)

		assert.Equal(t, "info", cfg.Logger.Level)
		assert.Equal(t, "stdout", cfg.Logger.Output)
	})

	t.Run("Prefix with Fallback", func(t *testing.T) {
		// Create a standalone test for prefix fallback that doesn't interfere with other tests
		t.Run("Without Fallback", func(t *testing.T) {
			// Set test variables
			os.Setenv("APP_DB_HOST", "app-db")
			os.Setenv("APP_DB_PORT", "3306")
			defer func() {
				os.Unsetenv("APP_DB_HOST")
				os.Unsetenv("APP_DB_PORT")
			}()

			type DatabaseConfig struct {
				Host string `env:"HOST"`
				Port int    `env:"PORT"`
			}

			type Config struct {
				Database DatabaseConfig `env:"DB"`
			}

			var cfg Config
			err := envx.Load(&cfg, envx.WithPrefix("APP_"))
			require.NoError(t, err)

			assert.Equal(t, "app-db", cfg.Database.Host)
			assert.Equal(t, 3306, cfg.Database.Port)
		})

		t.Run("With Fallback", func(t *testing.T) {
			// Set test variables
			os.Setenv("DB_HOST", "fallback-db")
			os.Setenv("APP_DB_PORT", "3306")
			defer func() {
				os.Unsetenv("DB_HOST")
				os.Unsetenv("APP_DB_PORT")
			}()

			type DatabaseConfig struct {
				Host string `env:"HOST"`
				Port int    `env:"PORT"`
			}

			type Config struct {
				Database DatabaseConfig `env:"DB"`
			}

			var cfg Config
			err := envx.Load(&cfg, envx.WithPrefix("APP_"), envx.WithPrefixFallback(true))
			require.NoError(t, err)

			assert.Equal(t, "fallback-db", cfg.Database.Host) // Should fallback to non-prefixed
			assert.Equal(t, 3306, cfg.Database.Port)          // This one has the prefixed version
		})
	})

	t.Run("Deeply Nested Structs", func(t *testing.T) {
		// Set up clean test environment for deeply nested structs
		os.Setenv("SYSTEM_AUTH_URL", "https://auth.example.com")
		os.Setenv("SYSTEM_AUTH_TIMEOUT", "5s")
		os.Setenv("SYSTEM_AUTH_CREDENTIALS_USERNAME", "admin")
		os.Setenv("SYSTEM_AUTH_CREDENTIALS_PASSWORD", "secret")
		defer func() {
			os.Unsetenv("SYSTEM_AUTH_URL")
			os.Unsetenv("SYSTEM_AUTH_TIMEOUT")
			os.Unsetenv("SYSTEM_AUTH_CREDENTIALS_USERNAME")
			os.Unsetenv("SYSTEM_AUTH_CREDENTIALS_PASSWORD")
		}()

		type CredentialsConfig struct {
			Username string `env:"USERNAME"`
			Password string `env:"PASSWORD"`
		}

		type AuthProviderConfig struct {
			URL         string            `env:"URL"`
			Timeout     time.Duration     `env:"TIMEOUT"`
			Credentials CredentialsConfig `env:"CREDENTIALS"`
		}

		type SystemConfig struct {
			Auth AuthProviderConfig `env:"AUTH"`
		}

		type Config struct {
			System SystemConfig `env:"SYSTEM"`
		}

		var cfg Config
		err := envx.Load(&cfg)
		require.NoError(t, err)

		assert.Equal(t, "https://auth.example.com", cfg.System.Auth.URL)
		assert.Equal(t, 5*time.Second, cfg.System.Auth.Timeout)
		assert.Equal(t, "admin", cfg.System.Auth.Credentials.Username)
		assert.Equal(t, "secret", cfg.System.Auth.Credentials.Password)
	})
}

type CustomTagParser struct{}

func (p *CustomTagParser) Parse(tag string) (envx.Tag, error) {
	// Simple implementation that just adds a "CUSTOM_" prefix to all names
	var result envx.Tag
	parts := strings.Split(tag, ";")
	if len(parts) > 0 {
		namesList := strings.Split(parts[0], ",")
		for _, name := range namesList {
			name = strings.TrimSpace(name)
			if name != "" {
				// Remove "CUSTOM_" prefix if it exists
				if strings.HasPrefix(name, "CUSTOM_") {
					result.Names = append(result.Names, name)
				} else {
					result.Names = append(result.Names, "CUSTOM_"+name)
				}
			}
		}
	}
	return result, nil
}

func TestStructLoader_CustomTagParser(t *testing.T) {
	os.Setenv("CUSTOM_VAR", "custom_value")
	defer os.Unsetenv("CUSTOM_VAR")

	type Config struct {
		Variable string `env:"VAR"` // Custom parser will look for CUSTOM_VAR
	}

	var cfg Config
	err := envx.Load(&cfg, envx.WithTagParser(&CustomTagParser{}))
	require.NoError(t, err)
	assert.Equal(t, "custom_value", cfg.Variable)
}

func TestStructLoader_CustomDirectiveHandler(t *testing.T) {
	os.Setenv("TEST_EMAIL", "test@example.com")
	os.Setenv("INVALID_EMAIL", "not-an-email")
	defer func() {
		os.Unsetenv("TEST_EMAIL")
		os.Unsetenv("INVALID_EMAIL")
	}()

	// Define a custom email validator directive handler
	emailValidatorHandler := func(ctx *envx.FieldContext, _ envx.Directive) error {
		value, err := ctx.Variable.String()
		if err != nil {
			return err
		}

		if !strings.Contains(value, "@") || !strings.Contains(value, ".") {
			return fmt.Errorf("invalid email format: %s", value)
		}
		return nil
	}

	t.Run("Valid Email", func(t *testing.T) {
		type Config struct {
			Email string `env:"TEST_EMAIL;email"`
		}

		var cfg Config
		err := envx.Load(&cfg, envx.WithCustomValidator("email", emailValidatorHandler))
		require.NoError(t, err)
		assert.Equal(t, "test@example.com", cfg.Email)
	})

	t.Run("Invalid Email", func(t *testing.T) {
		type Config struct {
			Email string `env:"INVALID_EMAIL;email"`
		}

		var cfg Config
		err := envx.Load(&cfg, envx.WithCustomValidator("email", emailValidatorHandler))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid email format")
	})
}

func TestStructLoader_AutoSnakeCase(t *testing.T) {
	os.Setenv("AUTO_SNAKE_CASE", "regular_snake_case_value")
	os.Setenv("ID_OF_IP", "acronym_value")
	os.Setenv("USER_ID_TYPE", "acronym_end_value")
	os.Setenv("IP_ADDRESS", "acronym_start_value")
	os.Setenv("COMPLEX_URL_PARSER", "complex_acronyms_value")

	defer func() {
		os.Unsetenv("AUTO_SNAKE_CASE")
		os.Unsetenv("ID_OF_IP")
		os.Unsetenv("USER_ID_TYPE")
		os.Unsetenv("IP_ADDRESS")
		os.Unsetenv("COMPLEX_URL_PARSER")
	}()

	type Config struct {
		AutoSnakeCase    string // Should map to AUTO_SNAKE_CASE
		IDOfIP           string // Should map to ID_OF_IP (acronym in middle)
		UserIDType       string // Should map to USER_ID_TYPE (acronym at end)
		IPAddress        string // Should map to IP_ADDRESS (acronym at start)
		ComplexURLParser string // Should map to COMPLEX_URL_PARSER (multiple acronyms)
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.NoError(t, err)
	assert.Equal(t, "regular_snake_case_value", cfg.AutoSnakeCase)
	assert.Equal(t, "acronym_value", cfg.IDOfIP)
	assert.Equal(t, "acronym_end_value", cfg.UserIDType)
	assert.Equal(t, "acronym_start_value", cfg.IPAddress)
	assert.Equal(t, "complex_acronyms_value", cfg.ComplexURLParser)
}

func TestStructLoader_MultipleErrors(t *testing.T) {
	type Config struct {
		Required1 string `env:"REQUIRED1;required"`
		Required2 string `env:"REQUIRED2;required"`
	}

	var cfg Config
	err := envx.Load(&cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REQUIRED1")
	assert.Contains(t, err.Error(), "REQUIRED2")
	assert.Contains(t, err.Error(), "is not set")
}

// TestConfig example struct with methods for testing
type TestConfig struct {
	TLSEnabled     bool   `env:"TLS_ENABLED"`
	Password       string `env:"PASSWORD;validateMethod(ValidatePassword)"`
	StrongPassword string `env:"PASSWORD_STRONG;validateMethod(ValidatePassword)"`
}

// IsTLSEnabled returns true if TLS is enabled
func (c *TestConfig) IsTLSEnabled() bool {
	return c.TLSEnabled
}

// ValidatePassword validates a password
func (c *TestConfig) ValidatePassword(password string) error {
	if len(password) < 10 {
		return errors.New("password is too weak")
	}

	// Check for uppercase, lowercase, digit, and special char
	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(password)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(password)
	hasDigit := regexp.MustCompile(`[0-9]`).MatchString(password)
	hasSpecial := regexp.MustCompile(`[^a-zA-Z0-9]`).MatchString(password)

	if !(hasUpper && hasLower && hasDigit && hasSpecial) {
		return errors.New("password must contain uppercase, lowercase, digit, and special character")
	}

	return nil
}
