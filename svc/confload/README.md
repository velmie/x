# confload

confload is a small configuration loader for Go services. It merges defaults, files, readers, environment variables, flags, and overrides into a struct, can report where each value came from, and can reject unknown keys.

## Goals

- Provide one loader that handles multiple configuration sources with clear precedence.
- Keep config structs simple and explicit via tags.
- Offer a snapshot of values and their provenance for diagnostics and CLI help.

## Installation

```bash
go get github.com/velmie/x/svc/confload
```

## Quick start

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/velmie/x/svc/confload"
)

type Config struct {
	App struct {
		Name    string        `k:"name" default:"demo"`
		Timeout time.Duration `k:"timeout" default:"5s"`
	} `k:"app"`
}

func main() {
	loader := confload.New(
		confload.WithEnvPrefix("APP_"),
		confload.WithFiles("config.yaml"),
	)

	cfg, err := confload.LoadInto[Config](loader)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(cfg.App.Name, cfg.App.Timeout)
}
```

## How loading works

confload builds a combined configuration tree by applying sources in this order:

1. Struct tag defaults
2. Defaults provided by code
3. Files and readers
4. Environment variables
5. Flags
6. Overrides provided by code

Later sources override earlier ones. This makes it easy to set safe defaults and then customize per deployment.

## Core API

### Loader

Create a loader and apply options.

```go
loader := confload.New(
	confload.WithEnvPrefix("CORE_"),
	confload.WithFiles("config.yaml", "config.local.yaml"),
	confload.WithConfigFileEnv("CORE_CONFIG_FILE"),
)
```

### LoadInto

Load the entire configuration into a struct type.

```go
cfg, err := confload.LoadInto[Config](loader)
```

### LoadIntoWithSnapshot

Load the configuration and return the snapshot captured during the load.

```go
cfg, snap, err := confload.LoadIntoWithSnapshot[Config](loader)
```

### LoadSubset

Load only a subtree by prefix.

```go
logCfg, err := confload.LoadSubset[LogConfig](loader, "log")
```

### Unmarshal and UnmarshalPrefix

Use these methods when you already have a pointer and want to avoid generics.

```go
var cfg Config
if err := loader.Unmarshal(&cfg); err != nil {
	return err
}
```

## Sources and options

### Struct tag defaults

Add a `default` tag. For slices, use `default_sep` to change the separator.

```go
type HTTP struct {
	Addr    string   `k:"addr" default:"127.0.0.1:8080"`
	Headers []string `k:"headers" default:"X-Req-Id,User-Agent" default_sep:","`
}
```

### Defaults from code

```go
loader := confload.New(
	confload.WithDefaults(map[string]any{
		"log.level": "info",
	}),
)
```

### Files

Supported formats are YAML, JSON, and TOML based on file extension.

```go
loader := confload.New(
	confload.WithFiles("config.yaml"),
)
```

Files and readers are loaded in the order they are added to the loader.

### Optional files

Optional files are ignored when they do not exist.

```go
loader := confload.New(
	confload.WithOptionalFiles("config.local.yaml"),
)
```

### Readers

Readers are parsed based on the extension of the provided name.

```go
loader := confload.New(
	confload.WithReader("config.yaml", strings.NewReader("value: 42")),
)
```

### Environment variables

Set a prefix, then use dotted keys that map to upper snake case.

```go
loader := confload.New(confload.WithEnvPrefix("APP_"))
```

For example, `db.host` becomes `APP_DB_HOST`.

Lists can be supplied as indexed environment variables. Indexes must start at 0.

```
APP_ALLOWED_ORIGINS_0=https://a.example.com
APP_ALLOWED_ORIGINS_1=https://b.example.com
```

### Custom environment mapping

Override how dotted keys map to environment variable names or provide explicit aliases.

```go
loader := confload.New(
	confload.WithEnvPrefix("APP_"),
	confload.WithEnvKeyFunc(func(prefix, dotted string) string {
		return prefix + strings.ToUpper(strings.ReplaceAll(dotted, ".", "__"))
	}),
	confload.WithEnvAliases(map[string]string{
		"db.host": "DATABASE_HOST",
	}),
)
```

Aliases take precedence over the custom function. For help output, you can use `loader.EnvName` to compute the final name.

### Flags

Flags are read from a provided `flag.FlagSet` using their visited values.

```go
fs := flag.NewFlagSet("svc", flag.ContinueOnError)
fs.String("log.level", "", "")
_ = fs.Parse(os.Args[1:])

loader := confload.New(confload.WithFlagSet(fs))
```

If a flag value implements the method below, its return value is used as the dotted key.

```go
type DottedKeyProvider interface {
	DottedKey() string
}
```

### Overrides

Overrides have the highest priority and are useful for programmatic changes.

```go
loader := confload.New(
	confload.WithOverrides(map[string]any{
		"feature.enabled": true,
	}),
)
```

### Decode hooks

Add custom decode hooks that run after the built in ones.

```go
type Level string

hook := func(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String || to != reflect.TypeOf(Level("")) {
		return data, nil
	}
	return Level(strings.ToUpper(data.(string))), nil
}

loader := confload.New(confload.WithDecodeHooks(hook))
```

### Strict mode

Strict mode rejects keys that are not present in the target struct.

```go
loader := confload.New(
	confload.WithFiles("config.yaml"),
	confload.WithStrict(),
)
```

When strict mode fails, the error is of type `*confload.UnknownKeysError` and includes the offending keys and their origins.

## Tag reference

confload reads standard tags on your config structs:

- `k` sets the dotted key for a field.
- `default` sets a default value.
- `default_sep` sets the list separator for slice defaults.
- `validate` and `hint` are collected by `DescribeStruct` for help output.

Example:

```go
type Config struct {
	Environment string `k:"environment" default:"development" validate:"required" hint:"Runtime environment"`
}
```

If you want a custom tag name, set it explicitly:

```go
loader := confload.New(confload.WithTagName("cfg"))
```

For describing structs that use a custom tag name, use:

```go
desc, err := confload.DescribeStructWithTag[Config]("cfg")
```

## Snapshot and provenance

After loading, you can inspect the final values and where they came from.

```go
snap := loader.Snapshot()
for key, origin := range snap.Origins {
	fmt.Printf("%s from %s (%s)\n", key, origin.Source, origin.Identifier)
}
```

`Snapshot.Files` lists loaded file paths and reader names in load order.

Origins include:

- `struct_default`
- `defaults`
- `file`
- `reader`
- `env`
- `flag`
- `override`

## DescribeStruct

`DescribeStruct` builds a catalog of config fields with metadata. This is useful for CLI help or docs.

```go
desc, err := confload.DescribeStruct[Config]()
if err != nil {
	return err
}

for _, field := range desc.Fields() {
	fmt.Println(field.Path, field.Required, field.Default, field.Hint)
}
```

You can also get a map of dotted paths to environment variable names:

```go
envMap := desc.EnvMap("APP_")
fmt.Println(envMap["log.level"]) // APP_LOG_LEVEL
```

## Type decoding

confload uses decode hooks so string values can fill common types:

- `time.Duration`
- `bool` with common spellings like true, false, 1, 0, yes, no
- numeric types like int, uint, float
- `*url.URL` with validation for absolute URLs

Example:

```go
type Services struct {
	Endpoint *url.URL     `k:"endpoint"`
	Timeout  time.Duration `k:"timeout"`
}
```

## Error behavior

- Unsupported file extensions return a clear error.
- Invalid types during decode return a descriptive error.
- `DescribeStruct` returns an error for non struct types.

## Testing

```bash
go test ./...
```
