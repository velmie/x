//go:build integration

package mysql_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	connection "github.com/velmie/x/svc/sqlconnection/mysql"
)

const integrationServerName = "localhost"

// TestConnectionIntegration requires an explicitly configured disposable MySQL fixture.
func TestConnectionIntegration(t *testing.T) {
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("context constructor and pool ownership", func(t *testing.T) {
		initCtx, cancelInit := context.WithCancel(ctx)
		defer cancelInit()
		current := *cfg
		db, err := connection.NewConnectionContext(initCtx, &current, integrationLogger{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		cancelInit()
		require.NoError(t, db.PingContext(ctx))
		var actualName string
		var actualTime time.Time
		err = db.QueryRowContext(ctx, "SELECT DATABASE(), CAST('2026-01-02 03:04:05' AS DATETIME)").Scan(&actualName, &actualTime)
		require.NoError(t, err)
		require.Equal(t, cfg.Name, actualName)
		require.Equal(t, time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), actualTime)
		require.Positive(t, current.MaxIdleConnections)
		require.Positive(t, current.MaxOpenConnections)
	})

	t.Run("legacy constructor", func(t *testing.T) {
		current := *cfg
		//nolint:contextcheck // Exercise the original constructor without a context.
		db, err := connection.NewConnection(&current, integrationLogger{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		require.NoError(t, db.PingContext(ctx))
	})

	t.Run("independent pools and session settings", func(t *testing.T) {
		first := *cfg
		first.TLSConfig = nil
		first.TLSCertPath = os.Getenv("MYSQL_INTEGRATION_CA")
		first.TLSServerName = integrationServerName
		first.Params = "sql_mode=%27STRICT_ALL_TABLES%27&rejectReadOnly=true"
		second := *cfg
		second.MaxOpenConnections = 4
		second.MaxIdleConnections = 4
		pools := make([]*sql.DB, 0, 2)
		for _, current := range []*connection.Config{
			&first,
			&second,
		} {
			db, err := connection.NewConnectionContext(ctx, current, integrationLogger{})
			require.NoError(t, err)
			pools = append(pools, db)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
		}
		require.Equal(t, 10, pools[0].Stats().MaxOpenConnections)
		require.Equal(t, 10, first.MaxIdleConnections)
		require.Equal(t, 4, pools[1].Stats().MaxOpenConnections)
		require.Equal(t, 4, second.MaxIdleConnections)
		var mode string
		require.NoError(t, pools[0].QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&mode))
		require.Equal(t, "STRICT_ALL_TABLES", mode)
		firstConn, err := pools[1].Conn(ctx)
		require.NoError(t, err)
		secondConn, err := pools[1].Conn(ctx)
		require.NoError(t, err)
		require.NoError(t, firstConn.Close())
		require.NoError(t, secondConn.Close())
		require.Equal(t, 2, pools[1].Stats().Idle)
	})

	t.Run("before connect supplies credentials per connection", func(t *testing.T) {
		current := *cfg
		current.User = ""
		current.Password = ""
		var calls atomic.Int32
		current.DriverOptions = []driver.Option{
			driver.BeforeConnect(func(_ context.Context, dc *driver.Config) error {
				calls.Add(1)
				dc.User = cfg.User
				dc.Passwd = cfg.Password

				return nil
			}),
		}
		db, err := connection.NewConnectionContext(ctx, &current, integrationLogger{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		first, err := db.Conn(ctx)
		require.NoError(t, err)
		second, err := db.Conn(ctx)
		require.NoError(t, err)
		require.NoError(t, first.PingContext(ctx))
		require.NoError(t, second.PingContext(ctx))
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())
		require.EqualValues(t, 2, calls.Load())
	})

	t.Run("raw IPv6", func(t *testing.T) {
		host := os.Getenv("MYSQL_INTEGRATION_IPV6_HOST")
		if host == "" {
			t.Skip("set MYSQL_INTEGRATION_IPV6_HOST to exercise IPv6")
		}
		current := *cfg
		current.Host = host
		db, err := connection.NewConnectionContext(ctx, &current, integrationLogger{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		require.NoError(t, db.PingContext(ctx))
	})

	t.Run("independent TLS profiles", func(t *testing.T) {
		tls12 := *cfg
		tls12.TLSConfig = cfg.TLSConfig.Clone()
		tls12.TLSConfig.MinVersion = tls.VersionTLS12
		tls12.TLSConfig.MaxVersion = tls.VersionTLS12
		tls13 := *cfg
		tls13.TLSConfig = cfg.TLSConfig.Clone()
		tls13.TLSConfig.MinVersion = tls.VersionTLS13
		tls13.TLSConfig.MaxVersion = tls.VersionTLS13
		ready := make(chan struct{})
		var arrived atomic.Int32
		for _, profile := range []struct {
			name string
			cfg  *connection.Config
		}{
			{
				name: "TLSv1.2",
				cfg:  &tls12,
			},
			{
				name: "TLSv1.3",
				cfg:  &tls13,
			},
		} {
			t.Run(profile.name, func(t *testing.T) {
				t.Parallel()
				queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
				defer queryCancel()
				db, err := connection.NewConnectionContext(queryCtx, profile.cfg, integrationLogger{})
				// Arrival precedes the assertion so a failed constructor cannot strand its peer.
				if arrived.Add(1) == 2 {
					close(ready)
				}
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, db.Close()) })
				select {
				case <-ready:
				case <-queryCtx.Done():
					t.Fatal(queryCtx.Err())
				}
				// Reconnect only after both independently configured constructors have finished.
				db.SetMaxIdleConns(0)
				var key, version string
				err = db.QueryRowContext(queryCtx, "SHOW SESSION STATUS LIKE 'Ssl_version'").Scan(&key, &version)
				require.NoError(t, err)
				require.Equal(t, profile.name, version)
			})
		}
	})

	t.Run("untrusted certificate", func(t *testing.T) {
		current := *cfg
		current.TLSConfig = cfg.TLSConfig.Clone()
		current.TLSConfig.RootCAs = x509.NewCertPool()
		db, err := connection.NewConnectionContext(ctx, &current, integrationLogger{})
		require.Error(t, err)
		require.Nil(t, db)
		var unknownAuthority x509.UnknownAuthorityError
		require.ErrorAs(t, err, &unknownAuthority)
	})
}

type integrationLogger struct{}

func (integrationLogger) Info(string, ...any) {}

func integrationConfig(t *testing.T) *connection.Config {
	t.Helper()
	host := os.Getenv("MYSQL_INTEGRATION_HOST")
	if host == "" {
		t.Skip("set MYSQL_INTEGRATION_HOST to run against a disposable MySQL fixture")
	}
	port, err := strconv.Atoi(os.Getenv("MYSQL_INTEGRATION_PORT"))
	require.NoError(t, err)
	// #nosec G703 -- The CA path selects an explicit test fixture.
	pem, err := os.ReadFile(os.Getenv("MYSQL_INTEGRATION_CA"))
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(pem))

	// #nosec G101 -- Public, fictitious credentials for a disposable integration fixture.

	return &connection.Config{
		Host:     host,
		Port:     port,
		Name:     "fixture ?@+#",
		User:     "fixture:@+?",
		Password: "fixture:/?@%#&+pass",
		TLSConfig: &tls.Config{
			RootCAs:    roots,
			ServerName: integrationServerName,
			MinVersion: tls.VersionTLS12,
		},
	}
}
