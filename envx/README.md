# Envx

The envx package provides fluent API for retrieving and validating environment variables. It
allows for easy fetching, default setting, type conversion, and conditions checking of environment variables.

## Features

* Retrieve environment variables with fallbacks.
* Set default values.
* Enforce required variables.
* Validate variables against a set of conditions including range validations.
* Convert environment variable values to common types (string, boolean, duration, int, uint, float types, time.Time, etc.)

## Usage

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

// Numeric range validations
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