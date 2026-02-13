package confload

import (
	"errors"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadInto_StructTagDefaults(t *testing.T) {
	type Limits struct {
		RequestSize int `k:"request_size" default:"512"`
	}

	type HTTP struct {
		Addr    *url.URL      `k:"addr" default:"https://example.org"`
		Timeout time.Duration `k:"timeout" default:"5s"`
		Retries int           `k:"retries" default:"3"`
		Debug   bool          `k:"debug" default:"false"`
		Headers []string      `k:"headers" default:"X-Req-Id;User-Agent" default_sep:";"`
		Limits  Limits        `k:"limits"`
	}

	cfg, err := LoadInto[HTTP](New())
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Equal(t, "https://example.org", cfg.Addr.String())
	require.Equal(t, 5*time.Second, cfg.Timeout)
	require.Equal(t, 3, cfg.Retries)
	require.False(t, cfg.Debug)
	require.Equal(t, []string{"X-Req-Id", "User-Agent"}, cfg.Headers)
	require.Equal(t, 512, cfg.Limits.RequestSize)
}

func TestLoadInto_TagDefaultsLowestPriority(t *testing.T) {
	type Config struct {
		Value string `k:"value" default:"tag"`
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	writeFile(t, configPath, "value: file\n")

	t.Run("tag value", func(t *testing.T) {
		cfg, err := LoadInto[Config](New())
		require.NoError(t, err)
		require.Equal(t, "tag", cfg.Value)
	})

	t.Run("defaults override tag", func(t *testing.T) {
		cfg, err := LoadInto[Config](New(WithDefaults(map[string]any{"value": "defaults"})))
		require.NoError(t, err)
		require.Equal(t, "defaults", cfg.Value)
	})

	t.Run("file overrides defaults", func(t *testing.T) {
		cfg, err := LoadInto[Config](New(
			WithDefaults(map[string]any{"value": "defaults"}),
			WithFiles(configPath),
		))
		require.NoError(t, err)
		require.Equal(t, "file", cfg.Value)
	})

	t.Run("env overrides file", func(t *testing.T) {
		t.Setenv("VALUE", "env")

		cfg, err := LoadInto[Config](New(
			WithDefaults(map[string]any{"value": "defaults"}),
			WithFiles(configPath),
		))
		require.NoError(t, err)
		require.Equal(t, "env", cfg.Value)
	})

	t.Run("flag overrides env", func(t *testing.T) {
		t.Setenv("VALUE", "env")
		fs := newValueFlagSet(t, "flag")

		cfg, err := LoadInto[Config](New(
			WithDefaults(map[string]any{"value": "defaults"}),
			WithFiles(configPath),
			WithFlagSet(fs),
		))
		require.NoError(t, err)
		require.Equal(t, "flag", cfg.Value)
	})

	t.Run("overrides have highest precedence", func(t *testing.T) {
		t.Setenv("VALUE", "env")
		fs := newValueFlagSet(t, "flag")

		cfg, err := LoadInto[Config](New(
			WithDefaults(map[string]any{"value": "defaults"}),
			WithFiles(configPath),
			WithFlagSet(fs),
			WithOverrides(map[string]any{"value": "override"}),
		))
		require.NoError(t, err)
		require.Equal(t, "override", cfg.Value)
	})
}

func TestLoader_WithTagName(t *testing.T) {
	type Config struct {
		Value string `cfg:"value"`
	}

	cfg, err := LoadInto[Config](New(
		WithTagName("cfg"),
		WithOverrides(map[string]any{"value": "override"}),
	))
	require.NoError(t, err)
	require.Equal(t, "override", cfg.Value)
}

func TestLoader_WithEnvPrefix(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	t.Setenv("CORE_VALUE", "from_env")

	loader := New(WithEnvPrefix("  CORE "))
	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "from_env", cfg.Value)
}

func TestLoader_WithConfigFileEnv(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "base.yaml")
	envPath := filepath.Join(tempDir, "env.yaml")

	writeFile(t, basePath, "value: base\n")
	writeFile(t, envPath, "value: env-file\n")

	t.Setenv("CONFIG_PATH", envPath)

	loader := New(
		WithFiles(basePath),
		WithConfigFileEnv(" CONFIG_PATH "),
	)

	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "env-file", cfg.Value)
}

func TestLoadSubset(t *testing.T) {
	type Logging struct {
		Level   string `k:"level" default:"info"`
		Enabled bool   `k:"enabled" default:"true"`
	}

	loader := New(
		WithOverrides(map[string]any{
			"services.logging.level": "debug",
		}),
	)

	cfg, err := LoadSubset[Logging](loader, "services.logging")
	require.NoError(t, err)
	require.Equal(t, "debug", cfg.Level)
	require.True(t, cfg.Enabled)
}

func TestLoadIntoWithDefaultsMergesOptions(t *testing.T) {
	type Config struct {
		A string `k:"a"`
		B string `k:"b"`
	}

	loader := New(
		WithDefaults(map[string]any{"a": "first", "b": "original"}),
		WithDefaults(map[string]any{"b": "overridden"}),
	)

	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "first", cfg.A)
	require.Equal(t, "overridden", cfg.B)
}

func TestLoadIntoWithDefaultsEmptyOverrideDoesNotClear(t *testing.T) {
	type Config struct {
		A string `k:"a"`
	}

	loader := New(
		WithDefaults(map[string]any{"a": "value"}),
		WithDefaults(nil),
	)

	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "value", cfg.A)
}

func TestLoadInto_NilLoader(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	_, err := LoadInto[Config](nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errNilLoader)
}

func TestLoadIntoWithSnapshot_NilLoader(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	_, _, err := LoadIntoWithSnapshot[Config](nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errNilLoader)
}

func TestLoadSubset_NilLoader(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	_, err := LoadSubset[Config](nil, "value")
	require.Error(t, err)
	require.ErrorIs(t, err, errNilLoader)
}

func TestLoadInto_FileErrors(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		_, err := LoadInto[Config](New(WithFiles(path)))
		require.Error(t, err)
		require.ErrorContains(t, err, "stat config file")
	})

	t.Run("directory path", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadInto[Config](New(WithFiles(dir)))
		require.Error(t, err)
		require.ErrorContains(t, err, errConfigFileIsDir.Error())
	})

	t.Run("unsupported extension", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "config.txt")
		writeFile(t, path, "value: 42\n")

		_, err := LoadInto[Config](New(WithFiles(path)))
		require.Error(t, err)
		require.ErrorContains(t, err, errUnsupportedConfigFormat.Error())
	})

	t.Run("parse error", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "config.yaml")
		writeFile(t, path, "value: [1\n")

		_, err := LoadInto[Config](New(WithFiles(path)))
		require.Error(t, err)
		require.ErrorContains(t, err, "load config file")
	})
}

func TestLoadInto_FlagsNotVisited(t *testing.T) {
	type Config struct {
		Value string `k:"value" default:"tag"`
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("value", "", "usage")

	cfg, err := LoadInto[Config](New(WithFlagSet(fs)))
	require.NoError(t, err)
	require.Equal(t, "tag", cfg.Value)
}

func TestLoadInto_EnvList(t *testing.T) {
	type Config struct {
		List []string `k:"list"`
	}

	t.Run("sequential indexes collected", func(t *testing.T) {
		t.Setenv("APP_LIST_0", "one")
		t.Setenv("APP_LIST_1", "two")

		cfg, err := LoadInto[Config](New(WithEnvPrefix("APP_")))
		require.NoError(t, err)
		require.Equal(t, []string{"one", "two"}, cfg.List)
	})

	t.Run("missing zero index ignored", func(t *testing.T) {
		t.Setenv("APP2_LIST_1", "one")

		cfg, err := LoadInto[Config](New(WithEnvPrefix("APP2_")))
		require.NoError(t, err)
		require.Nil(t, cfg.List)
	})
}

func TestLoadInto_NumericTypes(t *testing.T) {
	type Numerics struct {
		Int   int   `k:"int"`
		Int8  int8  `k:"int8"`
		Int16 int16 `k:"int16"`
		Int32 int32 `k:"int32"`
		Int64 int64 `k:"int64"`

		Uint   uint   `k:"uint"`
		Uint8  uint8  `k:"uint8"`
		Uint16 uint16 `k:"uint16"`
		Uint32 uint32 `k:"uint32"`
		Uint64 uint64 `k:"uint64"`

		Float32 float32 `k:"float32"`
		Float64 float64 `k:"float64"`
	}

	loader := New(
		WithOverrides(map[string]any{
			"int":   "1",
			"int8":  "2",
			"int16": "3",
			"int32": "4",
			"int64": "5",

			"uint":   "11",
			"uint8":  "12",
			"uint16": "13",
			"uint32": "14",
			"uint64": "15",

			"float32": "1.23",
			"float64": "4.56",
		}),
	)

	cfg, err := LoadInto[Numerics](loader)
	require.NoError(t, err)

	require.Equal(t, 1, cfg.Int)
	require.Equal(t, int8(2), cfg.Int8)
	require.Equal(t, int16(3), cfg.Int16)
	require.Equal(t, int32(4), cfg.Int32)
	require.Equal(t, int64(5), cfg.Int64)

	require.Equal(t, uint(11), cfg.Uint)
	require.Equal(t, uint8(12), cfg.Uint8)
	require.Equal(t, uint16(13), cfg.Uint16)
	require.Equal(t, uint32(14), cfg.Uint32)
	require.Equal(t, uint64(15), cfg.Uint64)

	require.Equal(t, float32(1.23), cfg.Float32)
	require.Equal(t, 4.56, cfg.Float64)
}

func TestDecodeHooks_InvalidInputs(t *testing.T) {
	t.Run("stringToURLHook reject relative url", func(t *testing.T) {
		_, err := stringToURLHook(reflect.TypeOf(""), urlType, "/relative")
		require.Error(t, err)
	})

	t.Run("stringToURLHook empty string", func(t *testing.T) {
		res, err := stringToURLHook(reflect.TypeOf(""), urlType, "")
		require.NoError(t, err)
		require.Nil(t, res)
	})

	t.Run("stringToBoolHook invalid", func(t *testing.T) {
		_, err := stringToBoolHook(reflect.TypeOf(""), reflect.TypeOf(true), "maybe")
		require.Error(t, err)
	})

	t.Run("stringToNumberHook invalid", func(t *testing.T) {
		_, err := stringToNumberHook(reflect.TypeOf(""), reflect.TypeOf(int(0)), "abc")
		require.Error(t, err)
	})

	t.Run("stringToFloatHook invalid", func(t *testing.T) {
		_, err := stringToFloatHook(reflect.TypeOf(""), reflect.TypeOf(float64(0)), "abc")
		require.Error(t, err)
	})
}

func TestDecodeHooks_Successful(t *testing.T) {
	t.Run("stringToNumberHook", func(t *testing.T) {
		val, err := stringToNumberHook(reflect.TypeOf(""), reflect.TypeOf(int(0)), "123")
		require.NoError(t, err)
		require.Equal(t, int64(123), val)
	})

	t.Run("stringToFloatHook", func(t *testing.T) {
		val, err := stringToFloatHook(reflect.TypeOf(""), reflect.TypeOf(float64(0)), "1.23")
		require.NoError(t, err)
		require.Equal(t, 1.23, val)
	})
}

type describeSample struct {
	Environment string `k:"environment" validate:"required" default:"development" hint:"Runtime env" docs:"env"`
	Log         struct {
		Level string `k:"level" validate:"required" default:"info" hint:"Log level"`
	} `k:"log"`
	Database struct {
		Host  string   `k:"host" validate:"required" hint:"Database host" docs:"db_host"`
		Ports []string `k:"ports" default:"3306,3307" default_sep:"," hint:"Allowed ports"`
	} `k:"database"`
	Extra *struct {
		Enabled bool `k:"enabled" default:"true" hint:"Toggle extra feature"`
	} `k:"extra"`
}

func TestDescribeStruct(t *testing.T) {
	desc, err := DescribeStruct[describeSample]()
	require.NoError(t, err)
	require.Equal(t, 5, desc.Len())

	fields := desc.Fields()
	require.Len(t, fields, 5)
	require.Equal(t, []string{
		"database.host",
		"database.ports",
		"environment",
		"extra.enabled",
		"log.level",
	}, collectPaths(fields))

	env := desc.EnvMap("CORE_SVCV4_")
	require.Equal(t, "CORE_SVCV4_DATABASE_HOST", env["database.host"])
	require.Equal(t, "CORE_SVCV4_ENVIRONMENT", env["environment"])

	field, ok := desc.Lookup("database.host")
	require.True(t, ok)
	require.True(t, field.Required)
	require.Equal(t, "Database host", field.Hint)
	require.Equal(t, "required", field.Validation)
	require.Nil(t, field.Default)
	require.Equal(t, "db_host", field.Attributes["docs"])

	ports, ok := desc.Lookup("database.ports")
	require.True(t, ok)
	require.Equal(t, []string{"3306", "3307"}, ports.Default)
	require.Equal(t, "", ports.Validation)

	logLevel, ok := desc.Lookup("log.level")
	require.True(t, ok)
	require.Equal(t, "info", logLevel.Default)
	require.Equal(t, "Log level", logLevel.Hint)
	require.Equal(t, "required", logLevel.Validation)

	enabled, ok := desc.Lookup("extra.enabled")
	require.True(t, ok)
	require.Equal(t, "true", enabled.Default)
	require.Equal(t, "Toggle extra feature", enabled.Hint)
	require.Empty(t, enabled.Validation)

	envField, ok := desc.Lookup("environment")
	require.True(t, ok)
	require.Equal(t, "env", envField.Attributes["docs"])
}

func TestDescribeStructWithTag(t *testing.T) {
	type Config struct {
		Value string `cfg:"value"`
	}

	desc, err := DescribeStructWithTag[Config]("cfg")
	require.NoError(t, err)
	require.Equal(t, 1, desc.Len())

	field, ok := desc.Lookup("value")
	require.True(t, ok)
	require.Equal(t, "value", field.Path)
}

func TestLoader_SnapshotSources(t *testing.T) {
	type cfg struct {
		Default  string `k:"default" default:"tag"`
		Fallback string `k:"fallback"`
		File     string `k:"file"`
		Env      string `k:"env"`
		Flag     string `k:"flag"`
		Override string `k:"override"`
	}

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "config.yaml")
	writeFile(t, filePath, "file: from_file\nflag: from_file\n")

	t.Setenv("APP_ENV", "from_env")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("flag", "", "usage")
	require.NoError(t, fs.Parse([]string{"-flag=from_flag"}))

	loader := New(
		WithEnvPrefix("APP_"),
		WithDefaults(map[string]any{"fallback": "from_defaults"}),
		WithFiles(filePath),
		WithFlagSet(fs),
		WithOverrides(map[string]any{"override": "from_override"}),
	)

	result, err := LoadInto[cfg](loader)
	require.NoError(t, err)
	require.Equal(t, "tag", result.Default)
	require.Equal(t, "from_defaults", result.Fallback)
	require.Equal(t, "from_file", result.File)
	require.Equal(t, "from_env", result.Env)
	require.Equal(t, "from_flag", result.Flag)
	require.Equal(t, "from_override", result.Override)

	snap := loader.Snapshot()
	require.Equal(t, "tag", snap.Values["default"])
	require.Equal(t, SourceStructDefault, snap.Origins["default"].Source)

	require.Equal(t, "from_defaults", snap.Values["fallback"])
	require.Equal(t, SourceDefaults, snap.Origins["fallback"].Source)

	require.Equal(t, "from_file", snap.Values["file"])
	require.Equal(t, SourceFile, snap.Origins["file"].Source)
	require.Equal(t, filepath.Clean(filePath), snap.Origins["file"].Identifier)

	require.Equal(t, "from_env", snap.Values["env"])
	require.Equal(t, SourceEnv, snap.Origins["env"].Source)
	require.Equal(t, "APP_ENV", snap.Origins["env"].Identifier)

	require.Equal(t, "from_flag", snap.Values["flag"])
	require.Equal(t, SourceFlag, snap.Origins["flag"].Source)
	require.Equal(t, "flag", snap.Origins["flag"].Identifier)

	require.Equal(t, "from_override", snap.Values["override"])
	require.Equal(t, SourceOverride, snap.Origins["override"].Source)

	require.Len(t, snap.Files, 1)
	require.Equal(t, filepath.Clean(filePath), snap.Files[0])
}

func TestLoader_SnapshotSources_MapParent(t *testing.T) {
	type mapCfg struct {
		Resource struct {
			Attributes map[string]string `k:"attributes"`
		} `k:"resource"`
	}

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "config.yaml")
	writeFile(t, filePath, `resource:
  attributes:
    service.version: dev
    some.custom.attr: hello
`)

	loader := New(WithFiles(filePath))

	result, err := LoadInto[mapCfg](loader)
	require.NoError(t, err)
	require.Len(t, result.Resource.Attributes, 2)
	require.Equal(t, "dev", result.Resource.Attributes["service.version"])

	snap := loader.Snapshot()
	require.Equal(t, SourceFile, snap.Origins["resource.attributes"].Source)
	require.Equal(t, filepath.Clean(filePath), snap.Origins["resource.attributes"].Identifier)
	require.Equal(t, SourceFile, snap.Origins["resource.attributes.service.version"].Source)
}

func TestLoadIntoWithSnapshot(t *testing.T) {
	type Config struct {
		Value string `k:"value" default:"tag"`
	}

	loader := New(WithOverrides(map[string]any{"value": "override"}))
	cfg, snap, err := LoadIntoWithSnapshot[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "override", cfg.Value)
	require.Equal(t, "override", snap.Values["value"])
	require.Equal(t, SourceOverride, snap.Origins["value"].Source)
}

func TestLoader_WithDecodeHooks(t *testing.T) {
	type Level string

	type Config struct {
		Level Level `k:"level"`
	}

	hook := func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf(Level("")) {
			return data, nil
		}

		str, ok := data.(string)
		if !ok {
			return data, nil
		}

		return Level(strings.ToUpper(str)), nil
	}

	loader := New(
		WithOverrides(map[string]any{"level": "info"}),
		WithDecodeHooks(hook),
	)
	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, Level("INFO"), cfg.Level)
}

func TestLoader_WithOptionalFiles(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	t.Run("missing optional ignored", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		cfg, err := LoadInto[Config](New(WithOptionalFiles(path)))
		require.NoError(t, err)
		require.Equal(t, "", cfg.Value)
	})

	t.Run("optional loaded when present", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "config.yaml")
		writeFile(t, path, "value: optional\n")

		cfg, err := LoadInto[Config](New(WithOptionalFiles(path)))
		require.NoError(t, err)
		require.Equal(t, "optional", cfg.Value)
	})
}

func TestLoader_WithReader(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	loader := New(WithReader("config.yaml", strings.NewReader("value: from_reader\n")))
	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "from_reader", cfg.Value)

	snap := loader.Snapshot()
	require.Equal(t, SourceReader, snap.Origins["value"].Source)
	require.Equal(t, "config.yaml", snap.Origins["value"].Identifier)
	require.Equal(t, []string{"config.yaml"}, snap.Files)
}

func TestLoader_WithEnvAliases(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	t.Setenv("CUSTOM_VALUE", "from_alias")

	loader := New(
		WithEnvPrefix("APP_"),
		WithEnvKeyFunc(func(prefix, dotted string) string {
			return "SHOULD_NOT_BE_USED"
		}),
		WithEnvAliases(map[string]string{"value": "CUSTOM_VALUE"}),
	)
	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "from_alias", cfg.Value)
}

func TestLoader_WithEnvKeyFunc(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	t.Setenv("APP__VALUE", "from_env")
	loader := New(
		WithEnvPrefix("APP_"),
		WithEnvKeyFunc(func(prefix, dotted string) string {
			return prefix + "_" + strings.ToUpper(dotted)
		}),
	)
	cfg, err := LoadInto[Config](loader)
	require.NoError(t, err)
	require.Equal(t, "from_env", cfg.Value)
}

func TestLoader_StrictUnknownOverride(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	_, err := LoadInto[Config](New(
		WithOverrides(map[string]any{"value": "ok", "extra": "nope"}),
		WithStrict(),
	))
	require.Error(t, err)

	var unknownErr *UnknownKeysError
	require.True(t, errors.As(err, &unknownErr))
	require.Contains(t, unknownErr.Keys, "extra")
	require.Equal(t, SourceOverride, unknownErr.Origins["extra"].Source)
}

func TestLoader_StrictUnknownFileKey(t *testing.T) {
	type Config struct {
		Value string `k:"value"`
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.yaml")
	writeFile(t, path, "value: ok\nextra: nope\n")

	_, err := LoadInto[Config](New(WithFiles(path), WithStrict()))
	require.Error(t, err)

	var unknownErr *UnknownKeysError
	require.True(t, errors.As(err, &unknownErr))
	require.Contains(t, unknownErr.Keys, "extra")
	require.Equal(t, SourceFile, unknownErr.Origins["extra"].Source)
	require.Equal(t, filepath.Clean(path), unknownErr.Origins["extra"].Identifier)
}

func TestLoader_StrictAllowsMapSubkeys(t *testing.T) {
	type Config struct {
		Resource struct {
			Attributes map[string]string `k:"attributes"`
		} `k:"resource"`
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.yaml")
	writeFile(t, path, "resource:\n  attributes:\n    foo: bar\n")

	cfg, err := LoadInto[Config](New(WithFiles(path), WithStrict()))
	require.NoError(t, err)
	require.Equal(t, "bar", cfg.Resource.Attributes["foo"])
}

func TestLoader_StrictLoadSubsetIgnoresOtherKeys(t *testing.T) {
	type App struct {
		Name string `k:"name"`
	}

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.yaml")
	writeFile(t, path, "app:\n  name: demo\ndb:\n  host: localhost\n")

	cfg, err := LoadSubset[App](New(WithFiles(path), WithStrict()), "app")
	require.NoError(t, err)
	require.Equal(t, "demo", cfg.Name)
}

func collectPaths(fields []FieldDescriptor) []string {
	paths := make([]string, len(fields))
	for i := range fields {
		paths[i] = fields[i].Path
	}

	return paths
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func newValueFlagSet(t *testing.T, value string) *flag.FlagSet {
	t.Helper()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("value", "", "usage")
	require.NoError(t, fs.Parse([]string{"-value=" + value}))

	return fs
}
