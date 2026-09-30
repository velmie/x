# mysql

The package provides functionality to read DB configuration from environment variables and open an SQL connection

## Read configuration from environment variables

```go
import (
    "github.com/velmie/x/svc/sqlconnection/mysql"
)

func main() {
    cfg, err := mysql.ConfigFromEnv("PFX_")
}
```

For the example above, it reads the following environment variables:

| Name                            | Meaning                          | Required | Default | Example   |
|---------------------------------|----------------------------------|----------|---------|-----------|
| PFX_DB_HOST                     | Database connection host name    | Yes      |         | db.example |
| PFX_DB_PORT                     | Database connection port         | Yes      |         | 3306      |
| PFX_DB_USER                     | Database connection user         | Yes      |         | root      |
| PFX_DB_PASS                     | Database connection password     | Yes      |         | secret    |
| PFX_DB_NAME                     | Database name                    | Yes      |         | db_name   |
| PFX_DB_MAX_OPEN_CONNECTIONS     | Max number of connections        | No       |         | 10        |
| PFX_DB_MAX_IDLE_CONNECTIONS     | Max number of idle connections   | No       |         | 2         |
| PFX_DB_CONNECTION_MAX_LIFETIME  | Max lifetime of connections      | No       |         | 10m       |
| PFX_DB_CONNECTION_MAX_IDLE_TIME | Max lifetime of idle connections | No       |         | 5m        |
| PFX_DB_UNSAFE_DISABLE_TLS       | Disable TLS connection           | No       | false   | true      |
| PFX_DB_TLS_CERT_PATH            | Path to a PEM certificate        | No       |         | /file.pem |

## Open an SQL connection

Use a context to bound both the connectivity check and the query used to
calculate default pool limits:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

db, err := mysql.NewConnectionContext(ctx, cfg, logger)
if err != nil {
    // errors.Is and errors.As can inspect the original cause.
    return err
}
defer db.Close()
```

`NewConnection(cfg, logger)` remains supported with its original signature. It
uses `context.Background()` and has no initialization deadline.

If initialization fails, the created pool is closed before returning. The
primary failure is preserved. A cleanup error, when present, is also available
through `errors.Is` and `errors.As`. The supplied `Logger` receives connection
and pool-settings messages without raw dependency errors or credentials. Treat
returned dependency errors as internal diagnostics, not as safe client-facing
messages.

On success, the caller owns the pool and must close it. Canceling the
initialization context later does not close the pool. Supply a separate context
for each database operation.

The constructor uses typed driver configuration, preserving user, password,
and database-name values without DSN escaping. It enables `parseTime` and uses
the supplied `TLSConfig` for that connection pool without a global TLS registry.
Use a separate mutable `Config` for each constructor call, and do not mutate TLS
configuration or certificate pools while they are in use.

Programmatic `Config.Host` accepts a DNS name or an unbracketed IP address,
including IPv6. `ConfigFromEnv` retains its existing domain-name validation for
`DB_HOST`. The new constructor does not change environment variable validation.

Explicit pool settings remain unchanged. If either `MaxOpenConnections` or
`MaxIdleConnections` is zero, the constructor queries `max_connections` and
fills only the zero values: 90% for open connections and 10% for idle connections,
with a minimum of one for each. When both limits are set, no server-settings
query is needed. Zero `ConnMaxLifetime` and `ConnMaxIdleTime` default to one hour
and ten minutes respectively. Computed defaults are written back to `cfg`.

## Integration tests

Unit tests run with `go test ./...`. The optional `integration` build tag adds
real MySQL checks for credentials, database names, time parsing, pool ownership,
TLS configuration isolation, certificate verification, and IPv6.

Use a disposable MySQL server with TLS 1.2 and 1.3 enabled and a certificate valid
for `localhost`. Prepare the fictitious account and schema declared in
`connection_integration_test.go`. Set `MYSQL_INTEGRATION_HOST`,
`MYSQL_INTEGRATION_PORT`, and `MYSQL_INTEGRATION_CA` to its host, port, and PEM
certificate path, then run `go test -race -tags integration ./...`.
Set `MYSQL_INTEGRATION_IPV6_HOST` to an unbracketed IPv6 address to also test a
real IPv6 connection. The tests skip when the fixture is not configured. They
must never target a production server.
