//go:build integration

package mysql_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

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
		db, err := connection.NewConnection(&current, integrationLogger{}) //nolint:contextcheck // Exercises the legacy API without a context parameter.
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		require.NoError(t, db.PingContext(ctx))
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
			{name: "TLSv1.2", cfg: &tls12},
			{name: "TLSv1.3", cfg: &tls13},
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
	pem, err := os.ReadFile(os.Getenv("MYSQL_INTEGRATION_CA")) // #nosec G703 -- Operator-supplied certificate for an explicitly selected test fixture.
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
