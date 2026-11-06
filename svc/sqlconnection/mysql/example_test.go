//nolint:goconst // Independent test cases and examples repeat literal contract values.
package mysql_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"

	driver "github.com/go-sql-driver/mysql"

	"github.com/velmie/x/svc/sqlconnection/mysql"
)

func ExampleConfigFromEnv() {
	values := map[string]string{
		"EXAMPLE_DB_HOST":                     "mysql.example.test",
		"EXAMPLE_DB_PORT":                     "3306",
		"EXAMPLE_DB_NAME":                     "bookstore",
		"EXAMPLE_DB_USER":                     "bookstore",
		"EXAMPLE_DB_PASS":                     "example-only",
		"EXAMPLE_DB_MAX_OPEN_CONNECTIONS":     "",
		"EXAMPLE_DB_MAX_IDLE_CONNECTIONS":     "",
		"EXAMPLE_DB_CONNECTION_MAX_LIFETIME":  "",
		"EXAMPLE_DB_CONNECTION_MAX_IDLE_TIME": "",
		"EXAMPLE_DB_UNSAFE_DISABLE_TLS":       "false",
		"EXAMPLE_DB_TLS_CERT_PATH":            "",
		"EXAMPLE_DB_TLS_SERVER_NAME":          "",
		"EXAMPLE_DB_CONNECT_TIMEOUT":          "",
		"EXAMPLE_DB_READ_TIMEOUT":             "",
		"EXAMPLE_DB_WRITE_TIMEOUT":            "",
		"EXAMPLE_DB_PARAMS":                   "",
	}
	defer func() {
		for name := range values {
			_ = os.Unsetenv(name)
		}
	}()
	for name, value := range values {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
	cfg, err := mysql.ConfigFromEnv("EXAMPLE_")
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Host, cfg.TLSConfig.ServerName, cfg.TLSConfig.MinVersion == tls.VersionTLS13)
	// Output: mysql.example.test mysql.example.test true
}

func ExampleLoadTLSConfig() {
	// An empty CA file uses the operating system's trust roots.
	cfg, err := mysql.LoadTLSConfig("", "mysql.example.test")
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.ServerName, cfg.MinVersion == tls.VersionTLS13)
	// Output: mysql.example.test true
}

func ExampleConfig_DriverOptions() {
	// In a service, refreshCredentials obtains credentials from the chosen provider.
	refreshCredentials := func(_ context.Context, dc *driver.Config) error {
		dc.User = "example-user"
		dc.Passwd = "example-only"

		return nil
	}
	unavailable := errors.New("example database unavailable")
	cfg := mysql.Config{
		Host: "mysql.example.test",
		Port: 3306,
		DriverOptions: []driver.Option{
			driver.BeforeConnect(refreshCredentials),
			func(dc *driver.Config) error {
				dc.DialFunc = func(context.Context, string, string) (net.Conn, error) {
					return nil, unavailable
				}

				return nil
			},
		},
	}
	db, err := mysql.NewConnectionContext(context.Background(), &cfg, exampleLogger{})
	if err == nil {
		defer db.Close()
	}
	fmt.Println(errors.Is(err, unavailable))
	// Output: true
}

type exampleLogger struct{}

func (exampleLogger) Info(string, ...any) {}
