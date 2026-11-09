# MySQL connection pool

This package loads MySQL settings and creates a `database/sql` pool using
[go-sql-driver/mysql](https://github.com/go-sql-driver/mysql). The caller owns the pool and closes it.

## Changes to defaults

- Zero pool limits now select 10 open connections and the same number of idle connections. The constructor no longer
  queries the server's `max_connections` value. Zero connection lifetime and idle age both select three minutes.
- A programmatic `Config` with a nil `TLSConfig` now uses verified TLS 1.3 and system roots. A plaintext connection
  requires `UnsafeDisableTLS: true`. Environment configurations that already set `DB_UNSAFE_DISABLE_TLS=true` continue
  to use plaintext.

| Setting | Zero or omitted | Explicit value |
| --- | --- | --- |
| `MaxOpenConnections` | 10 | Positive per-pool limit; negative means unlimited |
| `MaxIdleConnections` | Effective open limit, or 10 when open is unlimited | Clamped to open limit; negative retains no idle connections |
| `ConnMaxLifetime` | 3 minutes | Positive maximum age; negative disables expiry |
| `ConnMaxIdleTime` | 3 minutes | Positive maximum idle age; negative disables expiry |
| `ConnectTimeout`, `ReadTimeout`, `WriteTimeout` | 0, using the driver's timeout behavior | Positive duration |
| TLS | Verified TLS 1.3 with system roots | Custom roots, server name, native TLS configuration, or explicit plaintext |

Connections are opened on demand. An idle limit of 10 does not preallocate connections. Lifetime and idle age retire
connections; they are not query deadlines. Choose limits for the entire fleet, including jobs and overlapping deployments.
For example, 12 pools with 10 open connections can use 120 of a server's 151 connections, leaving 31 for other clients.

## Environment configuration

`ConfigFromEnv(prefix)` reads `prefix + "DB_" +` the names below, validates the result, and prepares TLS. The five
identity fields are required. Set the password using the environment or an appropriate secret provider.

| Name after the prefix | Meaning | Default |
| --- | --- | --- |
| `DB_HOST` | DNS hostname or IP address | Required |
| `DB_PORT` | TCP port, 1 through 65535 | Required |
| `DB_NAME` | Database name | Required |
| `DB_USER` | Database user | Required |
| `DB_PASS` | Database password | Required |
| `DB_MAX_OPEN_CONNECTIONS` | Open connection limit | 0 |
| `DB_MAX_IDLE_CONNECTIONS` | Idle connection limit | 0 |
| `DB_CONNECTION_MAX_LIFETIME` | Maximum connection age | `0s` |
| `DB_CONNECTION_MAX_IDLE_TIME` | Maximum idle age | `0s` |
| `DB_UNSAFE_DISABLE_TLS` | Disable TLS | `false` |
| `DB_TLS_CERT_PATH` | PEM file containing trusted CA certificates | System roots |
| `DB_TLS_SERVER_NAME` | Server name used for certificate verification | `DB_HOST` |
| `DB_CONNECT_TIMEOUT` | Driver connection timeout | `0s` |
| `DB_READ_TIMEOUT` | Driver read timeout | `0s` |
| `DB_WRITE_TIMEOUT` | Driver write timeout | `0s` |
| `DB_PARAMS` | Native DSN query parameters, without `?` or credentials | Empty |

```go
cfg, err := mysql.ConfigFromEnv("BOOKSTORE_")
if err != nil {
    return err
}
```

A configuration loader can populate `Config` directly from its `k` tags. For example:

```yaml
db:
  host: mysql.example.test
  port: 3306
  user: bookstore
  name: bookstore
  max_open_connections: 4
  connect_timeout: 5s
  read_timeout: 5m
  write_timeout: 30s
  params: "rejectReadOnly=true&maxAllowedPacket=0&sql_mode=%27STRICT_ALL_TABLES%27"
```

Supply `BOOKSTORE_DB_PASS` separately. The same parameter string can be supplied through `BOOKSTORE_DB_PARAMS`.
Call `cfg.Database.Validate()` after loading operator settings. Validation checks the tagged fields without opening the
CA file or connecting to the database. For programmatically assembled settings, constructors do not require a user,
password, or database name because native options can provide them.

## TLS

By default, the server certificate and hostname are verified with a minimum of TLS 1.3. An empty CA path uses system
roots. A nonempty path replaces those roots with the PEM certificates in that file. `LoadTLSConfig(caFile, serverName)`
provides the same configuration to Go callers and preserves filesystem error causes.

TLS is resolved in this order:

1. `UnsafeDisableTLS: true` selects plaintext and cannot be combined with a CA path, server name, or `TLSConfig`.
2. A non-nil `TLSConfig` takes precedence over the CA path and server name.
3. Otherwise, the CA path is loaded with `TLSServerName`, or `Host` when the server name is empty.

Use a separate mutable `Config` for each constructor call. Do not mutate a TLS configuration or certificate pool while
it is in use. Native driver options are trusted Go code and can override the resulting TLS configuration.

## Opening a pool

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

db, err := mysql.NewConnectionContext(ctx, cfg, logger)
if err != nil {
    return err
}
defer db.Close()
```

`NewConnection(cfg, logger)` remains available and uses `context.Background()`. Pool settings are applied before the
connectivity check. One success record reports the effective pool settings, while computed defaults are written back to
`cfg`. On failure the constructor closes the pool and preserves the original and any cleanup causes for `errors.Is` and
`errors.As`. On success, canceling the initialization context does not close the pool. Use a context for each operation.

## Native driver settings

For file and environment configuration, use `Params` with the driver's
[DSN query syntax](https://github.com/go-sql-driver/mysql#parameters). URL-escape parameter values where required,
including slashes. Do not include the leading `?` or credentials. The removed driver parameter `strict` is rejected.
Parameters cannot change the selected TLS mode or enable plaintext fallback.

Go callers can use `DriverOptions` for native driver features:

```go
cfg.DriverOptions = []driver.Option{
    driver.BeforeConnect(refreshCredentials),
    func(dc *driver.Config) error {
        dc.Logger = driverLogger
        dc.DialFunc = dial
        return nil
    },
}
db, err := mysql.NewConnectionContext(ctx, &cfg, poolLogger)
```

The package applies structured driver settings first, then `Params`, then the native options in order. Repeated query
parameters follow the driver's last-value behavior. Options run once during assembly; `BeforeConnect` runs for each
physical connection. Option errors preserve their causes. Native options can deliberately override any driver setting,
including TLS and credentials.

## Integration tests

Unit tests run with `go test ./...`. The `integration` build tag adds real MySQL checks for pool limits, session
settings, per-connection credentials, TLS isolation and verification, and IPv6. Use a disposable MySQL server with
TLS 1.2 and 1.3 enabled and a certificate valid for `localhost`. Prepare the fictitious account and schema declared in
`connection_integration_test.go`. Set `MYSQL_INTEGRATION_HOST`, `MYSQL_INTEGRATION_PORT`, and `MYSQL_INTEGRATION_CA`,
then run `go test -race -tags integration ./...`. Set `MYSQL_INTEGRATION_IPV6_HOST` to an unbracketed IPv6 address to
also exercise IPv6. Tests skip when the fixture is not configured. Never target a production server.

### Configuration validation errors

`Config.Validate` returns `*validation.Error` for invalid settings. Violations
name fields by their `k` configuration keys and contain the failed rule and
parameter. The error unwraps to the original validator or TLS conflict cause.
`ConfigFromEnv` also preserves errors from environment parsing and validation.
The module requires Go 1.26 or later through its validation dependency.

### Driver logging and UTC

Use `WithDriverLogger` to route driver diagnostics through `slog` and
`RequireUTC` to require UTC parsing and the UTC session time zone. Apply the
UTC option after any options that modify time settings. The connection
constructor enables `parseTime` and uses the driver's UTC location by default.
See `ExampleWithDriverLogger` and `ExampleRequireUTC`.
