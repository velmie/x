# mysql

The provides functionality to read DB configuration from environment variables and open an SQL connection

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
| PFX_DB_HOST                     | Database connection host         | Yes      |         | 127.0.0.1 |
| PFX_DB_PORT                     | Database connection port         | Yes      |         | 3306      |
| PFX_DB_USER                     | Database connection user         | Yes      |         | root      |
| PFX_DB_PASS                     | Database connection password     | Yes      |         | secret    |
| PFX_DB_NAME                     | Database connection host         | Yes      |         | db_name   |
| PFX_DB_MAX_OPEN_CONNECTIONS     | Max number of connections        | No       |         | 10        |
| PFX_DB_MAX_IDLE_CONNECTIONS     | Max number of idle connections   | No       |         | 2         |
| PFX_DB_CONNECTION_MAX_LIFETIME  | Max lifetime of connections      | No       |         | 10m       |
| PFX_DB_CONNECTION_MAX_IDLE_TIME | Max lifetime of idle connections | No       |         | 5m        |
| PFX_DB_UNSAFE_DISABLE_TLS       | Disable TLS connection           | No       | false   | true      |
| PFX_DB_TLS_CERT_PATH            | Path to a PEM certificate        | No       |         | /file.pem |

## Open an SQL connection

```go
import (
    "github.com/velmie/x/svc/sqlconnection/mysql"
)

func main() {
    db, err := mysql.NewConnection(cfg, logger)
}
```

Default `Max lifetime of connections` is `1h`. Default `Max lifetime of idle connections` is `10m`.
