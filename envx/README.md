# Envx

The envx package provides fluent API for retrieving and validating environment variables. It
allows for easy fetching, default setting, type conversion, and conditions checking of configuration values from multiple sources.

## Features

* Retrieve configuration values with fallbacks from multiple sources
* Configurable sources - environment variables, .env files, in-memory maps, etc.
* **Source Labeling:** Assign labels to sources for targeted lookups.
* **Explicit-Only Sources:** Mark sources to be used only when explicitly requested.
* Set default values
* Enforce required variables
* Validate variables against a set of conditions including range validations
* Convert configuration values to common types (string, boolean, duration, int, uint, float types, time.Time, etc.)
* Load configuration values directly into struct fields using reflection and struct tags
* Support for nested structures with proper prefix handling

## Data Sources

Envx now supports retrieving configuration values from multiple sources. By default, it uses environment variables (`envx.EnvSource{}` with labels `env` and `default`), but you can add and configure additional sources using a `Resolver`.

**Source Configuration:**

When adding sources to a resolver, you can provide options:

*   `envx.WithLabels(labels ...string)`: Assigns one or more string labels to a source. These labels can be used in struct tags to target specific sources.
*   `envx.IsExplicitOnly()`: Marks a source so that it's *only* queried when explicitly referenced by one of its labels in a struct tag (e.g., `env:"VAR[label]"`). Such sources are ignored by default lookups (like `envx.Get`, `envx.Coalesce`, or struct fields without specific labels).

```go
import "github.com/velmie/x/envx"

// --- Resolver Setup ---

// Create a custom resolver
resolver := envx.NewResolver() 

// Add sources with labels and options
resolver.AddSource(envx.EnvSource{}, envx.WithLabels("env", "local")) // Regular source, searchable by default and via labels 'env' or 'local'

// Load from .env file
envFileSource, err := envx.NewEnvFileSource(".env")
if err == nil {
    resolver.AddSource(envFileSource, envx.WithLabels("file", "local")) // Also searchable by default and via labels 'file' or 'local'
}

// Example: Add a source (e.g., Vault) that should only be used when explicitly requested
vaultSource := NewVaultSource(...) // Assume this is your custom Vault source implementation
resolver.AddSource(vaultSource, envx.WithLabels("vault", "secure"), envx.IsExplicitOnly()) 

// Example: Add another explicit-only source
externalSource := NewExternalSource(...)
resolver.AddSource(externalSource, envx.WithLabels("api"), envx.IsExplicitOnly())

// --- Using the Resolver ---

// 1. Direct Lookup (using the resolver directly)
//    Searches only non-explicit sources ('env', 'file' in this example)
value, err := resolver.Get("SOME_VAR").String() 

// 2. Loading into a Struct (using the resolver)
type MyConfig struct {
    // See Struct Loader section for tag syntax
    DatabaseURL string `env:"DB_URL[file]"` // Only look for DB_URL in sources labeled 'file'
    APIKey      string `env:"API_KEY[vault],LEGACY_KEY"` // Try API_KEY only in 'vault', then try LEGACY_KEY in 'env' and 'file'
    DefaultVar  string `env:"DEFAULT_VAR"` // Search in 'env' and 'file' (non-explicit sources)
}

var cfg MyConfig
// Pass the custom resolver to Load
err = envx.Load(&cfg, envx.WithResolver(resolver)) 
if err != nil {
    // handle error
}

// --- Using the Default Resolver ---
// The DefaultResolver is pre-configured with EnvSource (labeled 'env', 'default')
// You can add more sources to it:
envx.DefaultResolver.AddSource(envFileSource, envx.WithLabels("file")) 

// Get uses the DefaultResolver's non-explicit sources
valueFromEnvOrFile := envx.Get("SOME_VAR") 
```

## Basic Usage

Basic Retrieval

```go
import "github.com/velmie/x/envx"

func main() {
    // Uses envx.DefaultResolver (initially contains only EnvSource)
    chain := envx.Get("MY_ENV_VAR").Default("defaultValue") 
    value, err := chain.String()
    if err != nil {
        // handle error
    }
    fmt.Println(value)
}
```
*Note: By default, `Get` and `Coalesce` (and the `DefaultResolver`) only search sources that are **not** marked as `ExplicitOnly`.*

Using Coalesce to Get the First Non-Empty Variable

```go
// Searches for VAR_1, VAR_2, VAR_3 in non-explicit sources of DefaultResolver
val, _ := envx.Coalesce("VAR_1", "VAR_2", "VAR_3").Default("defaultValue").String()
```

Validations

```go
// Ensure the variable is set (checks non-explicit sources)
chain := envx.Get("MY_VAR").Required() 

// Ensure the variable matches a regular expression
chain = envx.Get("MY_VAR").MatchRegexp(regexp.MustCompile("^value-\\d+$"))

// Ensure the variable is one of a set of values
chain = envx.Get("MY_VAR").OneOf("value1", "value2", "value3")

// String length validations
chain = envx.Get("MY_VAR").MinLength(3)
chain = envx.Get("MY_VAR").MaxLength(10)
chain = envx.Get("MY_VAR").ExactLength(8)

// Universal validation for any type
chain = envx.Get("ANY_TYPE_VAR").Min(5)  // Works for strings (length) and numbers (value)
chain = envx.Get("ANY_TYPE_VAR").Max(10) // Works for strings (length) and numbers (value)
chain = envx.Get("ANY_TYPE_VAR").Range(5, 10) // Works for all numeric types

// Numeric range validations (legacy, but still supported)
chain = envx.Get("MY_INT_VAR").MinInt(5)
chain = envx.Get("MY_INT_VAR").MaxInt(100)
chain = envx.Get("MY_INT_VAR").IntRange(5, 100)

// Unsigned integer range validations
chain = envx.Get("MY_UINT_VAR").MinUint(5)
chain = envx.Get("MY_UINT_VAR").MaxUint(100)
chain = envx.Get("MY_UINT_VAR").UintRange(5, 100)

// Float range validations
chain = envx.Get("MY_FLOAT_VAR").MinFloat(1.5)
chain = envx.Get("MY_FLOAT_VAR").MaxFloat(99.5)
chain = envx.Get("MY_FLOAT_VAR").FloatRange(1.5, 99.5)

// Chain multiple validations
chain = envx.Get("MY_VAR").Required().NotEmpty().MatchRegexp(regexp.MustCompile("^value-\\d+$")).MinLength(8)

// .... 

value, err := chain.String()
// ....
```

Conversions

```go
// Basic types
valueStr, err := envx.Get("MY_STRING_VAR").String()
valueBool, err := envx.Get("MY_BOOL_VAR").Boolean()
valueDuration, err := envx.Get("MY_DURATION_VAR").Duration()

// Integer types
valueInt, err := envx.Get("MY_INT_VAR").Int()
valueInt64, err := envx.Get("MY_INT64_VAR").Int64()

// Unsigned integer types
valueUint, err := envx.Get("MY_UINT_VAR").Uint()
valueUint8, err := envx.Get("MY_UINT8_VAR").Uint8()
valueUint16, err := envx.Get("MY_PORT_VAR").Uint16()
valueUint32, err := envx.Get("MY_UINT32_VAR").Uint32()
valueUint64, err := envx.Get("MY_UINT64_VAR").Uint64()

// Float types
valueFloat32, err := envx.Get("MY_FLOAT32_VAR").Float32()
valueFloat64, err := envx.Get("MY_FLOAT64_VAR").Float64()

// Time type
valueTime, err := envx.Get("MY_TIME_VAR").Time("2006-01-02T15:04:05Z07:00")

// URL
valueURL, err := envx.Get("MY_URL_VAR").URL()
```

## Prototype

There are often cases when checks for multiple variables are the same.
In order to avoid duplicating code, the package provides functionality for creating prototypes.

```go
p := envx.CreatePrototype().WithRunners(envx.Required, envx.NotEmpty).WithPrefix("MY_PREFIX_")

v1 := p.Get("VAR1").String()
v2 := p.Get("VAR2").String()
```

## Supply function

A common case is obtaining values in order to fill a structure. The following example demonstrates how to simplify the
handling of such scenarios.

```go
type DatabaseCredentials struct {
    Host     string
    Port     int
    Name     string
    User     string
    Password string
}

func DatabaseCredentialsFromEnv() (*DatabaseCredentials, error) {
    cfg := new(DatabaseCredentials)
    p := envx.CreatePrototype().WithRunners(envx.Required, envx.NotEmpty)

    err := envx.Supply(
        envx.Set(&cfg.Host, p.Get("DB_HOST").ValidURL().String),
        envx.Set(&cfg.Port, p.Get("DB_PORT").ValidPortNumber().Int),
        envx.Set(&cfg.Name, p.Get("DB_NAME").String),
        envx.Set(&cfg.User, p.Get("DB_USER").String),
        envx.Set(&cfg.Password, p.Get("DB_PASS").String),
    )

    if err != nil {
        return nil, err
    }

    return cfg, nil
}
```

This approach allows to "collapse" multiple calls and error checks into one compact structure that groups these calls
and errors.

You can build more complex structures using nested structures, like this:

```go
type Service struct {
    LogLevel            string
    DatabaseCredentials *DatabaseCredentials
}

func ServiceFromEnv() (*Service, error) {
    cfg := new(Service)

    err := envx.Supply(
        envx.Set(&cfg.LogLevel, envx.Prefixed("MY_APP_").Get("LOG_LEVEL").Default("info").OneOf("warn", "error", "info").String),
        envx.Set(&cfg.DatabaseCredentials, DatabaseCredentialsFromEnv),
    )
    if err != nil {
        return nil, err
    }

    return cfg, nil
}
```

## Lists

Sometimes there is a need to retrieve values in the form of a list. If there's also a need to check each item of the
list, use the 'Each' method, which presents the current value as a list of variables and allows applying checks to each
item of the list.

For example:

```go
addresses, err := envx.Get("MY_LISTEN_ADDRESSES").Each().ValidListenAddress().StringSlice()
if err != nil {
//...
}
// ...
```

By default, the delimiter is a comma ",", but it accepts any string as a delimiter.

```go
addresses, err := envx.Get("MY_LISTEN_ADDRESSES").Each("|").ValidListenAddress().StringSlice()
if err != nil {
//...
}
// ...
```

The library supports various slice types:

```go
// String slices
strSlice, err := envx.Get("MY_STR_LIST").StringSlice() // default delimiter: ","
strSlice, err := envx.Get("MY_STR_LIST").StringSlice("|") // custom delimiter
uniqueStrSlice, err := envx.Get("MY_UNIQUE_LIST").UniqueStringSlice()

// Number type slices
intSlice, err := envx.Get("MY_INT_LIST").IntSlice()
int64Slice, err := envx.Get("MY_INT64_LIST").Int64Slice()
uintSlice, err := envx.Get("MY_UINT_LIST").UintSlice() 
uint8Slice, err := envx.Get("MY_UINT8_LIST").Uint8Slice()
uint16Slice, err := envx.Get("MY_UINT16_LIST").Uint16Slice()
uint32Slice, err := envx.Get("MY_UINT32_LIST").Uint32Slice()
uint64Slice, err := envx.Get("MY_UINT64_LIST").Uint64Slice()
float32Slice, err := envx.Get("MY_FLOAT32_LIST").Float32Slice()
float64Slice, err := envx.Get("MY_FLOAT64_LIST").Float64Slice()

// Other types
boolSlice, err := envx.Get("MY_BOOL_LIST").BooleanSlice()
durationSlice, err := envx.Get("MY_DURATION_LIST").DurationSlice()
urlSlice, err := envx.Get("MY_URL_LIST").URLSlice()
timeSlice, err := envx.Get("MY_TIME_LIST").TimeSlice("2006-01-02")
```

## Struct Loader

The envx package provides functionality to load configuration values directly into struct fields using reflection and struct tags. This approach simplifies the process of loading configuration from environment variables.

### Basic Usage

```go
import "github.com/velmie/x/envx"

type Config struct {
    Host     string        `env:"HOST;required"`
    Port     int           `env:"PORT;default(8080)"`
    LogLevel string        `env:"LOG_LEVEL;default(info);oneOf(debug,info,warn,error)"`
    Timeout  time.Duration `env:"TIMEOUT;default(10s)"`
    Debug    bool          `env:"DEBUG;default(false)"`
}

func main() {
    var cfg Config
    // Uses envx.DefaultResolver unless overridden with envx.WithResolver(...)
    err := envx.Load(&cfg) 
    if err != nil {
        // handle error
    }
    
    // Use the config
    fmt.Printf("Server will start at %s:%d\n", cfg.Host, cfg.Port)
}
```

### Struct Tag Syntax

The struct tag format supports specifying environment variable names, source labels, and directives:

```
`env:"VAR_NAME1[labelA,labelB], VAR_NAME2, VAR_NAME3[labelC]; directive1; directive2(param)"`
```

-   **Variable Names and Fallbacks:**
  -   `VAR_NAME1, VAR_NAME2, ...`: Comma-separated list of environment variable names to try in order. The first non-empty value found is used.
-   **Source Labels (Optional):**
  -   `[labelA,labelB]`: Immediately after a variable name, you can specify a list of source labels in square brackets.
  -   **Behavior:** If labels are specified for a variable name (`VAR_NAME1[labelA,labelB]`), that specific name (`VAR_NAME1`) will **only** be searched for in sources that have **at least one** of the specified labels (`labelA` or `labelB`). This includes sources marked as `ExplicitOnly`.
  -   If no labels are specified for a variable name (`VAR_NAME2`), that name will be searched for in all sources that are **not** marked as `ExplicitOnly` (the default search behavior).
-   **Directives:**
  -   `;directive1;directive2(param)`: Semicolon-separated list of directives for validation, default values, etc. (See "Available Directives" below).

**Lookup Order:**

The loader processes the tag from left to right:

1.  It attempts to resolve the first item (`VAR_NAME1[labelA,labelB]`).
  *   It looks for `VAR_NAME1` only in sources labeled `labelA` or `labelB`.
2.  If not found, it attempts to resolve the second item (`VAR_NAME2`).
  *   It looks for `VAR_NAME2` in all *non-explicit-only* sources.
3.  If not found, it attempts to resolve the third item (`VAR_NAME3[labelC]`).
  *   It looks for `VAR_NAME3` only in sources labeled `labelC`.
4.  This continues until a value is found or all names are exhausted.
5.  Finally, directives (`directive1`, `directive2`) are applied to the found value (or the default value if none was found but a default is specified).

**Backward Compatibility:**

-   The old format `env:"VAR1,VAR2"` is parsed as trying `VAR1` in all non-explicit sources, then trying `VAR2` in all non-explicit sources.
-   This preserves compatibility, but note that sources marked `ExplicitOnly` will now be ignored by this old syntax.

Special tag formats:

1.  **Quoted Names:** `env:"'EXACT_VAR_NAME'"` - Looks for `EXACT_VAR_NAME` directly, ignoring any `WithPrefix` option. Source labels can still be used: `env:"'EXACT_VAR_NAME'[label]"`.
2.  **Leading Comma:** `env:",FALLBACK_NAME"` - Automatically prepends the field name (converted to UPPER_SNAKE_CASE) to the list. Searches `FIELD_NAME` first (in non-explicit sources), then `FALLBACK_NAME` (in non-explicit sources). Source labels can be used on any name: `env:",FALLBACK_NAME[label]"`.
3.  **Only Directives:** `env:";required;default(value)"` - Uses the field name (converted to UPPER_SNAKE_CASE) and searches in non-explicit sources.

### Example with Labeled Sources

```go
// Assume resolver is configured as in the Data Sources example:
// - envSource: labels=["env", "local"], explicitOnly=false
// - fileSource: labels=["file", "local"], explicitOnly=false
// - vaultSource: labels=["vault", "secure"], explicitOnly=true
// - externalSource: labels=["api"], explicitOnly=true

type Config struct {
    // 1. Try SECRET_KEY only in 'vault' or 'secure' sources.
    // 2. If not found, try API_TOKEN only in 'file' source.
    // 3. If not found, try TOKEN in 'env' and 'file' sources (non-explicit).
    APIKey string `env:"SECRET_KEY[vault,secure], API_TOKEN[file], TOKEN"`

    // Search DB_HOST in 'env' and 'file' sources (non-explicit).
    DBHost string `env:"DB_HOST"` 

    // Search REMOTE_CFG only in 'api' source. Apply 'required' directive.
    RemoteConfig string `env:"REMOTE_CFG[api];required"`

    // Field without tag: Search 'USERNAME' in 'env' and 'file' (non-explicit).
    Username string 
}

var cfg Config
err := envx.Load(&cfg, envx.WithResolver(resolver)) 
// Handle error...
```

### Field Type Support

The struct loader supports the following field types:

- Basic types: `string`, `bool`, `int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `float32`, `float64`, `time.Duration`
- Complex types: `time.Time`, `*url.URL`
- Collections: `[]string`, `[]int`, `[]int64`, `[]uint`, `[]uint8`, `[]uint16`, `[]uint32`, `[]uint64`, `[]float32`, `[]float64`, `[]bool`, `[]time.Duration`
- Maps: `map[string]string`
- Nested structures: Both embedded and explicitly tagged

### Nested Structs Support

The envx package now supports nested structures with proper prefix handling:

```go
type DatabaseConfig struct {
    Host     string `env:"HOST"`
    Port     int    `env:"PORT"`
    Username string `env:"USERNAME"` // Searched in non-explicit sources by default
    Password string `env:"PASSWORD[vault];required"` // Searched only in 'vault' source
}

type APIConfig struct {
    Endpoint string        `env:"ENDPOINT"`
    Timeout  time.Duration `env:"TIMEOUT"`
}

type Config struct {
    // Tagged nested structs - tag is used as prefix
    Database DatabaseConfig `env:"DB"` // Will look for DB_HOST, DB_PORT, DB_USERNAME, DB_PASSWORD[vault] etc.
    API      APIConfig      `env:"API"` // Will look for API_ENDPOINT, API_TIMEOUT
    
    // Non-tagged nested struct - fields accessed directly (respecting labels inside)
    Logger struct {
        Level  string `env:"LOGGER_LEVEL"`
        Output string `env:"LOGGER_OUTPUT[file]"` // Only look in 'file' source
    } 
    
    // Pointer to struct works too
    Metrics *struct {
        Path     string        `env:"METRICS_PATH"`
        Interval time.Duration `env:"METRICS_INTERVAL"`
    } `env:"METRICS"` // Will look for METRICS_PATH, METRICS_INTERVAL
}
```
*Note:* Prefixes from nested structs (`DB_`, `API_`, `METRICS_`) are combined correctly with variable names *before* source label filtering (`[label]`) is applied.

You can also nest structures multiple levels deep:

```go
type CredentialsConfig struct {
    Username string `env:"USERNAME"`
    Password string `env:"PASSWORD[vault]"` // Look only in vault
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

// This will look for:
// - SYSTEM_AUTH_URL (non-explicit sources)
// - SYSTEM_AUTH_TIMEOUT (non-explicit sources)
// - SYSTEM_AUTH_CREDENTIALS_USERNAME (non-explicit sources)
// - SYSTEM_AUTH_CREDENTIALS_PASSWORD[vault] (only in 'vault' source)
```

### Available Directives

#### Basic Directives

- `required`: Field is required and must have a value found (or a default). Applied *after* searching.
- `notEmpty`: Found value (or default) must not be empty.
- `default(value)`: Default value if no environment variable is found/set.
- `expand`: Expand environment variable references in the value (e.g., `${VAR_NAME}`).

#### Validation Directives

- `validURL`: Value must be a valid URL.
- `validIP`: Value must be a valid IP address.
- `validPort`: Value must be a valid port number (0-65535).
- `validDomain`: Value must be a valid domain name.
- `validListenAddr`: Value must be a valid listen address (format: `host:port`).
- `min(n)`: Universal validator that checks:
  - String length for string types.
  - Minimum value for numeric types.
- `max(n)`: Universal validator that checks:
  - String length for string types.
  - Maximum value for numeric types.
- `range(min,max)`: Universal range validator that works with all numeric types.
- `minLen(n)`: Value must have at least n characters.
- `maxLen(n)`: Value must have no more than n characters.
- `exactLen(n)`: Value must have exactly n characters.
- `regexp(pattern)`: Value must match the regular expression pattern.
- `oneOf(value1,value2,...)`: Value must be one of the specified values.

#### Format Directives

- `delimiter(char)`: Delimiter for slice elements (default is comma).
- `layout(format)`: Time format layout for parsing time.Time fields.

#### Custom Method Directives

- `validateMethod(methodName)`: Call a method on the struct to validate the field value.
- `requiredIfMethod(methodName)`: Field is required if the specified method returns true.
- `convertMethod(methodName)`: Call a method on the struct to convert the string value from environment variable to the field type.

### Configuration Options

The loader can be configured with various options:

```go
resolver := envx.NewResolver()
// ... add sources to resolver ...

err := envx.Load(&cfg, 
    envx.WithResolver(resolver), // Use the configured resolver
    envx.WithPrefix("APP_"),
    envx.WithPrefixFallback(true),
    envx.WithFallbackPrefix("DEFAULT_"),
    envx.WithCustomValidator("email", emailValidator))
```

Available options:

- `WithResolver(resolver)`: Use a specific `Resolver` instance for lookups. If not provided, `envx.DefaultResolver` is used.
- `WithPrefix(prefix)`: Add a prefix to environment variable names looked up *via the default mechanism* (i.e., when no source labels `[]` are specified for a name, or when using fallback names without labels). Prefixes are *not* applied to names explicitly targeting sources via labels (`VAR[label]`) or names in single quotes (`'EXACT_NAME'`).
- `WithPrefixFallback(enable)`: If enabled, falls back to non-prefixed names when prefixed ones are not set (only applies to names looked up via the default mechanism).
- `WithFallbackPrefix(prefix)`: Adds a secondary prefix for fallback when the primary prefix doesn't match (only applies to names looked up via the default mechanism).
- `WithTagParser(parser)`: Use a custom tag parser.
- `WithCustomValidator(name, validator)`: Add a custom validation directive.
- `WithTypeHandler(type, handler)`: Register a handler for a specific type.
- `WithKindHandler(kind, handler)`: Register a handler for a specific reflection kind.

### Custom Validation

You can create custom validators for your specific needs:

```go
// Using a custom validator directive
emailValidator := func(ctx *envx.FieldContext, _ envx.Directive) error {
    // ctx.Variable contains the resolved variable (if found)
    value, err := ctx.Variable.String() 
    if err != nil || !ctx.Variable.Exist {
        return nil // Don't validate if not found or error occurred during lookup
    }
    
    if !strings.Contains(value, "@") || !strings.Contains(value, ".") {
        return fmt.Errorf("invalid email format: %s", value)
    }
    return nil
}

err := envx.Load(&cfg, envx.WithCustomValidator("email", emailValidator))

// Or using a struct method
type Config struct {
    Password string `env:"PASSWORD;validateMethod(ValidatePassword)"`
    
    // Using custom conversion method
    CustomField MyType `env:"CUSTOM_ENV;convertMethod(ConvertToMyType)"`
}

func (c *Config) ValidatePassword(password string) error {
    // Note: This method receives the string value *after* it has been successfully retrieved.
    if len(password) < 10 {
        return errors.New("password is too weak")
    }
    return nil
}

// Custom conversion method takes a string and returns the desired type plus an error
func (c *Config) ConvertToMyType(value string) (MyType, error) {
    // Custom parsing logic here
    return MyType{Value: value}, nil
}
```

### Automatic Snake Case

If no `env` tag is specified for a field, its name is automatically converted to UPPER_SNAKE_CASE to derive the environment variable name. This conversion correctly handles common acronyms:

```go
type Config struct {
    // Regular camelCase to UPPER_SNAKE_CASE conversions:
    DatabaseURL string     // Uses DATABASE_URL environment variable (searched in non-explicit sources)
    ServerPort int         // Uses SERVER_PORT environment variable (searched in non-explicit sources)
    
    // Properly handling acronyms:
    IDOfIP string          // Uses ID_OF_IP environment variable
    UserIDType string      // Uses USER_ID_TYPE environment variable
    IPAddress string       // Uses IP_ADDRESS environment variable
    ComplexURLParser string // Uses COMPLEX_URL_PARSER environment variable
}
```

### Environment Variable Prefix Rules (Interaction with Labels)

When using `WithPrefix` in conjunction with source labels `[]`:

1.  **Names with Labels (`VAR[label]`)**: Prefixes (`WithPrefix`, `WithFallbackPrefix`) are **never** applied to variable names that have explicit source labels specified (e.g., `env:"VAR1[label]"`). The lookup uses the exact name (`VAR1`) within the specified sources (`label`).
2.  **Names without Labels (`VAR2`)**: Prefixes *are* applied as usual to names *without* explicit labels, according to the `WithPrefix`, `WithPrefixFallback`, and `WithFallbackPrefix` options. The search for these (potentially prefixed) names occurs only in *non-explicit-only* sources.
3.  **Quoted Names (`'EXACT_VAR'`)**: Prefixes are never applied, regardless of labels.
4.  **Order Matters**: In a tag like `env:"VAR1[label], VAR2"`, `VAR1` is looked up first (without prefix) in sources with `label`. If not found, `VAR2` is looked up (potentially with prefix) in non-explicit sources.

This ensures that label-based lookups are precise and independent of global prefix settings, while names relying on the default search mechanism still benefit from prefixes.
