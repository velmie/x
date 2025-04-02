# Envx

The envx package provides fluent API for retrieving and validating environment variables. It
allows for easy fetching, default setting, type conversion, and conditions checking of configuration values from multiple sources.

## Features

* Retrieve configuration values with fallbacks from multiple sources
* Configurable sources - environment variables, .env files, in-memory maps, etc.
* Set default values
* Enforce required variables
* Validate variables against a set of conditions including range validations
* Convert configuration values to common types (string, boolean, duration, int, uint, float types, time.Time, etc.)
* Load configuration values directly into struct fields using reflection and struct tags
* Support for nested structures with proper prefix handling

## Data Sources

Envx now supports retrieving configuration values from multiple sources. By default, it uses environment variables, but you can add additional sources:

```go
import "github.com/velmie/x/envx"

// Use environment variables (default)
value, err := envx.Get("MY_ENV_VAR").String()

// Add a custom source (in-memory map)
customSource := &envx.MapSource{
    Values: map[string]string{
        "CUSTOM_KEY": "custom_value",
    },
}
envx.DefaultResolver.AddSource(customSource)

// Now values will be first checked in customSource, then in environment variables
value, err := envx.Get("CUSTOM_KEY").String() // Returns "custom_value"

// Load from .env file
envFileSource, err := envx.NewEnvFileSource(".env")
if err != nil {
    // handle error
}
envx.DefaultResolver.AddSource(envFileSource)

// Create a custom resolver with specific sources
resolver := envx.NewResolver(
    envx.EnvSource{},                      // First check environment variables
    &envx.MapSource{Values: customValues}, // Then check in-memory map
)

// Use custom resolver
value, err := resolver.Get("CONFIG_KEY").String()
```

## Basic Usage

Basic Retrieval

```go
import "github.com/velmie/x/envx"

func main() {
chain := envx.Get("MY_ENV_VAR").Default("defaultValue")
value, err := chain.String()
if err != nil {
// handle error
}
fmt.Println(value)
}
```

Using Coalesce to Get the First Non-Empty Variable

```go
val, _ := envx.Coalesce("VAR_1", "VAR_2", "VAR_3").Default("defaultValue").String()
```

Validations

```go
// Ensure the variable is set
chain := envx.Get("MY_VAR").Required()

// Ensure the variable matches a regular expression
chain := envx.Get("MY_VAR").MatchRegexp(regexp.MustCompile("^value-\\d+$"))

// Ensure the variable is one of a set of values
chain := envx.Get("MY_VAR").OneOf("value1", "value2", "value3")

// String length validations
chain := envx.Get("MY_VAR").MinLength(3)
chain := envx.Get("MY_VAR").MaxLength(10)
chain := envx.Get("MY_VAR").ExactLength(8)

// Universal validation for any type
chain := envx.Get("ANY_TYPE_VAR").Min(5)  // Works for strings (length) and numbers (value)
chain := envx.Get("ANY_TYPE_VAR").Max(10) // Works for strings (length) and numbers (value)
chain := envx.Get("ANY_TYPE_VAR").Range(5, 10) // Works for all numeric types

// Numeric range validations (legacy, but still supported)
chain := envx.Get("MY_INT_VAR").MinInt(5)
chain := envx.Get("MY_INT_VAR").MaxInt(100)
chain := envx.Get("MY_INT_VAR").IntRange(5, 100)

// Unsigned integer range validations
chain := envx.Get("MY_UINT_VAR").MinUint(5)
chain := envx.Get("MY_UINT_VAR").MaxUint(100)
chain := envx.Get("MY_UINT_VAR").UintRange(5, 100)

// Float range validations
chain := envx.Get("MY_FLOAT_VAR").MinFloat(1.5)
chain := envx.Get("MY_FLOAT_VAR").MaxFloat(99.5)
chain := envx.Get("MY_FLOAT_VAR").FloatRange(1.5, 99.5)

// Chain multiple validations
chain := envx.Get("MY_VAR").Required().NotEmpty().MatchRegexp(regexp.MustCompile("^value-\\d+$")).MinLength(8)

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
    err := envx.Load(&cfg)
    if err != nil {
        // handle error
    }
    
    // Use the config
    fmt.Printf("Server will start at %s:%d\n", cfg.Host, cfg.Port)
}
```

### Struct Tag Syntax

The struct tag format is:

```
`env:"ENV_VAR_NAME;directive1;directive2(param);directive3(param1,param2)"`
```

- `ENV_VAR_NAME`: The name of the environment variable to load
- `directive1`, `directive2`, etc.: Directives that specify validation rules or other behaviors
- Directives can have parameters in parentheses: `directive(param)` or `directive(param1,param2)`
- Multiple directives are separated by semicolons
- Multiple environment variable names can be specified with comma separation: `env:"VAR1,VAR2,VAR3"`

When multiple environment variable names are specified, they are tried in the order listed, and the first one that is set will be used. This is similar to the `Coalesce` function:

```go
type Config struct {
    // Will try DATABASE_URL, then DB_URL, then DB_CONNECTION_STRING in order
    DatabaseURL string `env:"DATABASE_URL,DB_URL,DB_CONNECTION_STRING"`
    
    // Combines multiple variables with validation
    APIKey string `env:"API_KEY_PROD,API_KEY;required;minLen(10)"`
}
```

Special tag formats:

1. When using quoted variable names, the prefix is not applied: `env:"'EXACT_VAR_NAME'"` - will look for exactly `EXACT_VAR_NAME` without any prefixes.
2. When using a leading comma, the field name is automatically prepended: `env:",FALLBACK_NAME"` - will first try the field name (converted to UPPER_SNAKE_CASE), then `FALLBACK_NAME`.
3. When using only directives: `env:";required;default(value)"` - the field name (converted to UPPER_SNAKE_CASE) will be used.

This allows for flexible fallback strategies and migration paths when renaming environment variables.

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
    Username string `env:"USERNAME"`
    Password string `env:"PASSWORD"`
}

type APIConfig struct {
    Endpoint string        `env:"ENDPOINT"`
    Timeout  time.Duration `env:"TIMEOUT"`
}

type Config struct {
    // Tagged nested structs - tag is used as prefix
    Database DatabaseConfig `env:"DB"` // Will look for DB_HOST, DB_PORT, etc.
    API      APIConfig      `env:"API"` // Will look for API_ENDPOINT, API_TIMEOUT
    
    // Non-tagged nested struct - fields accessed directly
    Logger struct {
        Level  string `env:"LOGGER_LEVEL"`
        Output string `env:"LOGGER_OUTPUT"`
    } // Will look for LOGGER_LEVEL, LOGGER_OUTPUT directly
    
    // Pointer to struct works too
    Metrics *struct {
        Path     string        `env:"METRICS_PATH"`
        Interval time.Duration `env:"METRICS_INTERVAL"`
    } `env:"METRICS"` // Will look for METRICS_PATH, METRICS_INTERVAL
}
```

You can also nest structures multiple levels deep:

```go
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

// This will look for:
// - SYSTEM_AUTH_URL
// - SYSTEM_AUTH_TIMEOUT
// - SYSTEM_AUTH_CREDENTIALS_USERNAME
// - SYSTEM_AUTH_CREDENTIALS_PASSWORD
```

### Available Directives

#### Basic Directives

- `required`: Field is required and must be set in environment
- `notEmpty`: Value must not be empty
- `default(value)`: Default value if environment variable is not set
- `expand`: Expand environment variable references in the value (e.g., `${VAR_NAME}`)

#### Validation Directives

- `validURL`: Value must be a valid URL
- `validIP`: Value must be a valid IP address
- `validPort`: Value must be a valid port number (0-65535)
- `validDomain`: Value must be a valid domain name
- `validListenAddr`: Value must be a valid listen address (format: `host:port`)
- `min(n)`: Universal validator that checks:
  - String length for string types
  - Minimum value for numeric types
- `max(n)`: Universal validator that checks:
  - String length for string types
  - Maximum value for numeric types
- `range(min,max)`: Universal range validator that works with all numeric types
- `minLen(n)`: Value must have at least n characters
- `maxLen(n)`: Value must have no more than n characters
- `exactLen(n)`: Value must have exactly n characters
- `regexp(pattern)`: Value must match the regular expression pattern
- `oneOf(value1,value2,...)`: Value must be one of the specified values

#### Format Directives

- `delimiter(char)`: Delimiter for slice elements (default is comma)
- `layout(format)`: Time format layout for parsing time.Time fields

#### Custom Method Directives

- `validateMethod(methodName)`: Call a method on the struct to validate the field value
- `requiredIfMethod(methodName)`: Field is required if the specified method returns true
- `convertMethod(methodName)`: Call a method on the struct to convert the string value from environment variable to the field type

### Configuration Options

The loader can be configured with various options:

```go
err := envx.Load(&cfg, 
    envx.WithPrefix("APP_"),
    envx.WithPrefixFallback(true),
    envx.WithFallbackPrefix("DEFAULT_"),
    envx.WithCustomValidator("email", emailValidator))
```

Available options:

- `WithPrefix(prefix)`: Add a prefix to all environment variable names
  - The prefix is only automatically applied to the first name in the list of names
  - The prefix is not applied to names in single quotes: `env:"'EXACT_NAME'"` 
- `WithPrefixFallback(enable)`: If enabled, falls back to non-prefixed names when prefixed ones are not set
- `WithFallbackPrefix(prefix)`: Adds a secondary prefix for fallback when the primary prefix doesn't match
  - This is applied to all fallback names (names after the first one) when `WithPrefixFallback` is enabled
- `WithTagParser(parser)`: Use a custom tag parser
- `WithCustomValidator(name, validator)`: Add a custom validation directive
- `WithTypeHandler(type, handler)`: Register a handler for a specific type
- `WithKindHandler(kind, handler)`: Register a handler for a specific reflection kind
- `WithResolver(resolver)`: Use a custom resolver for retrieving values instead of the default one

### Custom Validation

You can create custom validators for your specific needs:

```go
// Using a custom validator directive
emailValidator := func(ctx *envx.FieldContext, _ envx.Directive) error {
    value, err := ctx.Variable.String()
    if err != nil {
        return err
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

If no `env` tag is specified, the field name is automatically converted to UPPER_SNAKE_CASE. The conversion properly handles acronyms in the field names:

```go
type Config struct {
    // Regular camelCase to UPPER_SNAKE_CASE conversions:
    DatabaseURL string     // Uses DATABASE_URL environment variable
    ServerPort int         // Uses SERVER_PORT environment variable
    
    // Properly handling acronyms:
    IDOfIP string          // Uses ID_OF_IP environment variable
    UserIDType string      // Uses USER_ID_TYPE environment variable
    IPAddress string       // Uses IP_ADDRESS environment variable
    ComplexURLParser string // Uses COMPLEX_URL_PARSER environment variable
}
```

This allows for a more intuitive mapping between struct field names and environment variable names, even when working with complex naming conventions and acronyms.

### Environment Variable Prefix Rules

When using the struct loader with prefixes, the following rules apply:

1. **Primary name with prefix**: The prefix is only applied to the first name in the comma-separated list in the tag. For example, with `WithPrefix("APP_")` and a tag `env:"VAR1,VAR2"`, the system will look for `APP_VAR1`, not for `APP_VAR2`.

2. **Quoted names**: If a name is enclosed in single quotes, the prefix is never applied to it. This allows for exact environment variable names. For example, with `WithPrefix("APP_")` and a tag `env:"'EXACT_VAR'"`, the system will look for exactly `EXACT_VAR`, not `APP_EXACT_VAR`.

3. **Leading comma**: If a tag starts with a comma, the field name is automatically prepended to the list of names to try. For example, with a field named `Port` and a tag `env:",FALLBACK_PORT"`, the system will first try the environment variable `PORT` (converted from the field name), and then `FALLBACK_PORT`.

4. **No names, only directives**: If a tag contains only directives (e.g., `env:";required;default(8080)"`), the field name is used as the environment variable name. For example, with a field named `Port` and a tag `env:";required"`, the system will look for the environment variable `PORT`.

5. **Fallback prefix**: When `WithPrefixFallback(true)` and `WithFallbackPrefix("FALLBACK_")` are used, the fallback prefix is applied to secondary names (after the first name) when they are not quoted. For example, with `WithPrefix("APP_")`, `WithPrefixFallback(true)`, `WithFallbackPrefix("DEFAULT_")` and a tag `env:"VAR1,VAR2"`, the system will look for `APP_VAR1`, then `DEFAULT_VAR2`, and then `VAR2`.