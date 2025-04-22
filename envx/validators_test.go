package envx_test

import (
	"os"
	"testing"

	. "github.com/velmie/x/envx"
)

func init() {
	// Initialize DefaultResolver for tests
	resolver := NewResolver()
	resolver.WithErrorHandler(ContinueOnError)
	resolver.AddSource(EnvSource{})
	DefaultResolver = resolver
}

func TestRangeValidators(t *testing.T) {
	// Clear environment variables before each run
	os.Clearenv()

	// Setup DefaultResolver for each test
	resolver := NewResolver()
	resolver.WithErrorHandler(ContinueOnError)
	resolver.AddSource(EnvSource{})
	DefaultResolver = resolver

	t.Run("String length validators", func(t *testing.T) {
		// Setup environment variables
		os.Setenv("MIN_LENGTH", "abc")
		os.Setenv("MAX_LENGTH", "abcdefghij")
		os.Setenv("EXACT_LENGTH", "abcde")
		os.Setenv("INVALID_MIN_LENGTH", "a")
		os.Setenv("INVALID_MAX_LENGTH", "abcdefghijklmno")
		os.Setenv("INVALID_EXACT_LENGTH", "abc")
		defer func() {
			os.Unsetenv("MIN_LENGTH")
			os.Unsetenv("MAX_LENGTH")
			os.Unsetenv("EXACT_LENGTH")
			os.Unsetenv("INVALID_MIN_LENGTH")
			os.Unsetenv("INVALID_MAX_LENGTH")
			os.Unsetenv("INVALID_EXACT_LENGTH")
		}()

		_, err := Get("MIN_LENGTH").MinLength(3).String()
		if err != nil {
			t.Errorf("MinLength validation failed: %v", err)
		}

		_, err = Get("MAX_LENGTH").MaxLength(10).String()
		if err != nil {
			t.Errorf("MaxLength validation failed: %v", err)
		}

		_, err = Get("EXACT_LENGTH").ExactLength(5).String()
		if err != nil {
			t.Errorf("ExactLength validation failed: %v", err)
		}

		_, err = Get("INVALID_MIN_LENGTH").MinLength(3).String()
		if err == nil {
			t.Error("MinLength should fail for strings shorter than minimum")
		}

		_, err = Get("INVALID_MAX_LENGTH").MaxLength(10).String()
		if err == nil {
			t.Error("MaxLength should fail for strings longer than maximum")
		}

		_, err = Get("INVALID_EXACT_LENGTH").ExactLength(5).String()
		if err == nil {
			t.Error("ExactLength should fail for strings not exactly the specified length")
		}
	})

	t.Run("Integer range validators", func(t *testing.T) {
		os.Setenv("VALID_INT", "50")
		os.Setenv("TOO_SMALL_INT", "5")
		os.Setenv("TOO_LARGE_INT", "200")
		defer func() {
			os.Unsetenv("VALID_INT")
			os.Unsetenv("TOO_SMALL_INT")
			os.Unsetenv("TOO_LARGE_INT")
		}()

		_, err := Get("VALID_INT").IntRange(10, 100).Int()
		if err != nil {
			t.Errorf("IntRange validation failed: %v", err)
		}

		_, err = Get("VALID_INT").MinInt(10).Int()
		if err != nil {
			t.Errorf("MinInt validation failed: %v", err)
		}

		_, err = Get("VALID_INT").MaxInt(100).Int()
		if err != nil {
			t.Errorf("MaxInt validation failed: %v", err)
		}

		_, err = Get("TOO_SMALL_INT").MinInt(10).Int()
		if err == nil {
			t.Error("MinInt should fail for values below minimum")
		}

		_, err = Get("TOO_LARGE_INT").MaxInt(100).Int()
		if err == nil {
			t.Error("MaxInt should fail for values above maximum")
		}

		_, err = Get("TOO_SMALL_INT").IntRange(10, 100).Int()
		if err == nil {
			t.Error("IntRange should fail for values below range")
		}

		_, err = Get("TOO_LARGE_INT").IntRange(10, 100).Int()
		if err == nil {
			t.Error("IntRange should fail for values above range")
		}
	})

	t.Run("Unsigned integer range validators", func(t *testing.T) {
		os.Setenv("VALID_UINT", "50")
		os.Setenv("TOO_SMALL_UINT", "5")
		os.Setenv("TOO_LARGE_UINT", "200")
		defer func() {
			os.Unsetenv("VALID_UINT")
			os.Unsetenv("TOO_SMALL_UINT")
			os.Unsetenv("TOO_LARGE_UINT")
		}()

		_, err := Get("VALID_UINT").UintRange(10, 100).Uint()
		if err != nil {
			t.Errorf("UintRange validation failed: %v", err)
		}

		_, err = Get("VALID_UINT").MinUint(10).Uint()
		if err != nil {
			t.Errorf("MinUint validation failed: %v", err)
		}

		_, err = Get("VALID_UINT").MaxUint(100).Uint()
		if err != nil {
			t.Errorf("MaxUint validation failed: %v", err)
		}

		_, err = Get("TOO_SMALL_UINT").MinUint(10).Uint()
		if err == nil {
			t.Error("MinUint should fail for values below minimum")
		}

		_, err = Get("TOO_LARGE_UINT").MaxUint(100).Uint()
		if err == nil {
			t.Error("MaxUint should fail for values above maximum")
		}

		_, err = Get("TOO_SMALL_UINT").UintRange(10, 100).Uint()
		if err == nil {
			t.Error("UintRange should fail for values below range")
		}

		_, err = Get("TOO_LARGE_UINT").UintRange(10, 100).Uint()
		if err == nil {
			t.Error("UintRange should fail for values above range")
		}
	})

	t.Run("Float range validators", func(t *testing.T) {
		os.Setenv("VALID_FLOAT", "50.5")
		os.Setenv("TOO_SMALL_FLOAT", "5.5")
		os.Setenv("TOO_LARGE_FLOAT", "200.5")
		defer func() {
			os.Unsetenv("VALID_FLOAT")
			os.Unsetenv("TOO_SMALL_FLOAT")
			os.Unsetenv("TOO_LARGE_FLOAT")
		}()

		_, err := Get("VALID_FLOAT").FloatRange(10.0, 100.0).Float64()
		if err != nil {
			t.Errorf("FloatRange validation failed: %v", err)
		}

		_, err = Get("VALID_FLOAT").MinFloat(10.0).Float64()
		if err != nil {
			t.Errorf("MinFloat validation failed: %v", err)
		}

		_, err = Get("VALID_FLOAT").MaxFloat(100.0).Float64()
		if err != nil {
			t.Errorf("MaxFloat validation failed: %v", err)
		}

		_, err = Get("TOO_SMALL_FLOAT").MinFloat(10.0).Float64()
		if err == nil {
			t.Error("MinFloat should fail for values below minimum")
		}

		_, err = Get("TOO_LARGE_FLOAT").MaxFloat(100.0).Float64()
		if err == nil {
			t.Error("MaxFloat should fail for values above maximum")
		}

		_, err = Get("TOO_SMALL_FLOAT").FloatRange(10.0, 100.0).Float64()
		if err == nil {
			t.Error("FloatRange should fail for values below range")
		}

		_, err = Get("TOO_LARGE_FLOAT").FloatRange(10.0, 100.0).Float64()
		if err == nil {
			t.Error("FloatRange should fail for values above range")
		}
	})
}
