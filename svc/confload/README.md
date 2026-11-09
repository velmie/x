# confload

confload loads configuration for Go services. It merges defaults, files,
readers, environment variables, flags, and overrides into a struct. It can
report where each value came from and reject unknown keys.

## Inspect declared configuration

After loading, describe the fields and render their decoded values:

```go
fields, err := confload.DescribeStruct[Config]()
if err != nil {
    return err
}
rows, err := loader.Inspect(fields, cfg, snapshot, confload.InspectOptions{
    OverrideLabel: "cli",
})
if err != nil {
    return err
}
return confload.WriteInspection(out, format, rows)
```

Use `loader.Inspect(fields, nil, confload.Snapshot{}, opts)` for schema-only output.
The loader method includes defaults supplied through `WithDefaults` and uses
the loader environment names unless overridden. Package-level `Inspect` keeps
its tag-default-only behavior.
Inspection performs no source loading and never reads `Snapshot.Values`. It
reads the decoded struct, so zero values are retained and nil nested pointers
are shown as null.

Mark sensitive fields or their containing structs with `sensitive:"true"`.
Inspection masks both defaults and values as `[REDACTED]` before formatting or
marshaling. A map, slice, or array is masked in its entirety when its element
type contains a sensitive field at any depth, even when that field is nested
through pointers or collections. This also hides non-sensitive members of the
collection. `DescribeStruct` itself retains the declared metadata, including
raw defaults. Treat it accordingly. File origins are displayed as `file[n]`,
reader and flag identifiers are reduced to source labels, and environment
origins use the declared environment name. Paths and reader names are omitted.
An override label defaults to `override`; applications may use another label
such as `cli`. Origins are matched by the descriptor's exact
path. When inspecting a separately described `LoadSubset` value, project the
snapshot's prefixed origin keys to the subset's relative paths and retain
`Snapshot.Files` for file numbering.

`WriteInspection` accepts `text` and `json`; an empty format selects text. JSON
uses flattened field metadata and includes `value` and `origin` only when values
were inspected. Text values use JSON encoding, including escaped aggregate
strings. Writer errors are returned unchanged.

Parse CLI override entries before constructing the loader:

```go
overrides, err := confload.ParseOverrides(fields, entries)
if err != nil {
    return err
}
loader := confload.New(confload.WithOverrides(overrides))
```

Entries are `path=value`; values may be empty or contain `=`. Paths must exactly
match a declared field and later entries win. Syntax errors wrap
`ErrInvalidOverride`; unknown paths return `*UnknownKeysError`. Override values
are not included in these errors.

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

An exported embedded struct value can place its fields in the containing
namespace with `k:",squash"`. The flattened paths apply to sources, environment
variables, defaults and inspection. Duplicate paths and leaf/container conflicts
are rejected before sources are read. Squash is explicit and does not support
embedded pointers or maps.

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

Files and readers are loaded in the order they are added to the loader. An explicitly
blank required or optional file path is an error. Supplying no paths adds no files.

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

For example, `db.host` becomes `APP_DB_HOST`. A present empty value overrides
lower-priority sources. Blank numeric strings, including whitespace-only values,
are invalid; use `0` for zero. A later override can replace a blank value before
decoding. Empty strings remain available to string fields.

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

Add custom decode hooks that run after the built in ones. Built-in conversion
errors describe the expected type or format without including the rejected
value. Numeric errors wrap `strconv.ErrSyntax` or `strconv.ErrRange`; URL
errors wrap the inner parsing cause. Invalid duration errors describe the
expected format. Custom hooks own their error messages.

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

To decode JSON objects in strings into `map[string]string`, explicitly install
`confload.JSONStringMapHook()` with `WithDecodeHooks`.

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
