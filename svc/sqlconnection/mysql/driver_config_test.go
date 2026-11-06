//nolint:goconst // Independent test cases and examples repeat literal contract values.
package mysql

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestDriverParametersAndOptionsPrecedence(t *testing.T) {
	var applied int
	cfg := Config{
		Host:           "mysql.example.test",
		Port:           3306,
		Name:           "db/?@",
		User:           "user:@",
		Password:       "pass:/?@",
		ConnectTimeout: time.Second,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   3 * time.Second,
		Params:         "timeout=4s&readTimeout=5s&rejectReadOnly=false&rejectReadOnly=true&sql_mode=%27STRICT_ALL_TABLES%27",
		DriverOptions: []driver.Option{
			func(dc *driver.Config) error {
				applied++
				require.Equal(t, 4*time.Second, dc.Timeout)
				require.Equal(t, 5*time.Second, dc.ReadTimeout)
				require.True(t, dc.RejectReadOnly)
				dc.ReadTimeout = 7 * time.Second

				return nil
			},
			func(dc *driver.Config) error {
				applied++
				require.Equal(t, 7*time.Second, dc.ReadTimeout)
				dc.WriteTimeout = 8 * time.Second

				return nil
			},
		},
	}
	dc, err := prepareDriverConfig(&cfg)
	require.NoError(t, err)
	require.Equal(t, 2, applied)
	require.Equal(t, "user:@", dc.User)
	require.Equal(t, "pass:/?@", dc.Passwd)
	require.Equal(t, "db/?@", dc.DBName)
	require.Equal(t, "mysql.example.test:3306", dc.Addr)
	require.True(t, dc.ParseTime)
	require.True(t, dc.RejectReadOnly)
	require.Equal(t, "'STRICT_ALL_TABLES'", dc.Params["sql_mode"])
	require.Equal(t, 4*time.Second, dc.Timeout)
	require.Equal(t, 7*time.Second, dc.ReadTimeout)
	require.Equal(t, 8*time.Second, dc.WriteTimeout)
	require.Equal(t, "mysql.example.test", dc.TLS.ServerName)
}

func TestDriverParameterTLSRules(t *testing.T) {
	for _, test := range []struct {
		name    string
		params  string
		plain   bool
		wantErr bool
	}{
		{
			name: "verified default",
		},
		{
			name:   "verified explicit",
			params: "tls=true",
		},
		{
			name:    "verified false conflicts",
			params:  "tls=false",
			wantErr: true,
		},
		{
			name:    "skip verify conflicts",
			params:  "tls=skip-verify",
			wantErr: true,
		},
		{
			name:    "preferred conflicts",
			params:  "tls=preferred",
			wantErr: true,
		},
		{
			name:    "fallback conflicts",
			params:  "allowFallbackToPlaintext=true",
			wantErr: true,
		},
		{
			name:  "plaintext default",
			plain: true,
		},
		{
			name:   "plaintext explicit",
			plain:  true,
			params: "tls=false",
		},
		{
			name:    "plaintext true conflicts",
			plain:   true,
			params:  "tls=true",
			wantErr: true,
		},
		{
			name:   "last TLS value wins",
			params: "tls=false&tls=true",
		},
		{
			name:   "last fallback value wins",
			params: "allowFallbackToPlaintext=true&allowFallbackToPlaintext=false",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{
				Host:             "mysql.example.test",
				Port:             3306,
				UnsafeDisableTLS: test.plain,
				Params:           test.params,
			}
			dc, err := prepareDriverConfig(&cfg)
			if test.wantErr {
				require.ErrorIs(t, err, errParamsTLS)

				return
			}
			require.NoError(t, err)
			require.Zero(t, dc.Timeout)
			require.Zero(t, dc.ReadTimeout)
			require.Zero(t, dc.WriteTimeout)
			if test.plain {
				require.Nil(t, dc.TLS)
			} else {
				require.NotNil(t, dc.TLS)
				require.Equal(t, uint16(tls.VersionTLS13), dc.TLS.MinVersion)
			}
			require.False(t, dc.AllowFallbackToPlaintext)
		})
	}
}

func TestDriverOptionMayOverrideTLS(t *testing.T) {
	cfg := Config{
		Host: "mysql.example.test",
		Port: 3306,
		DriverOptions: []driver.Option{
			func(dc *driver.Config) error {
				dc.TLS = nil
				dc.TLSConfig = "false"
				dc.AllowFallbackToPlaintext = true

				return nil
			},
		},
	}
	dc, err := prepareDriverConfig(&cfg)
	require.NoError(t, err)
	require.Nil(t, dc.TLS)
	require.Equal(t, "false", dc.TLSConfig)
	require.True(t, dc.AllowFallbackToPlaintext)
}

func TestDriverParameterAndOptionErrorsRetainCauses(t *testing.T) {
	cfg := Config{
		Params: "maxAllowedPacket=invalid",
	}
	_, err := prepareDriverConfig(&cfg)
	var numberError *strconv.NumError
	require.ErrorAs(t, err, &numberError)
	require.ErrorContains(t, err, numberError.Error())
	for _, params := range []string{
		"strict=true",
		"?parseTime=true",
		"loc=Europe/Vilnius",
	} {
		cfg.Params = params
		require.NotPanics(t, func() {
			_, err = prepareDriverConfig(&cfg)
		})
		require.Error(t, err)
	}
	cause := errors.New("native option failed")
	cfg.Params = ""
	cfg.DriverOptions = []driver.Option{
		func(*driver.Config) error { return cause },
	}
	_, err = prepareDriverConfig(&cfg)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
}

func TestBeforeConnectRunsForEveryConnection(t *testing.T) {
	var hooks atomic.Int32
	var optionCalls int
	dialError := errors.New("fixture dial failure")
	cfg := Config{
		Host: "mysql.example.test",
		Port: 3306,
		DriverOptions: []driver.Option{
			driver.BeforeConnect(func(_ context.Context, dc *driver.Config) error {
				hooks.Add(1)
				dc.User = "refreshed-user"
				dc.Passwd = "refreshed-password"

				return nil
			}),
			func(dc *driver.Config) error {
				optionCalls++
				dc.DialFunc = func(context.Context, string, string) (net.Conn, error) { return nil, dialError }

				return nil
			},
		},
	}
	dc, err := prepareDriverConfig(&cfg)
	require.NoError(t, err)
	connector, err := driver.NewConnector(dc)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = connector.Connect(context.Background())
		require.ErrorIs(t, err, dialError)
	}
	require.EqualValues(t, 2, hooks.Load())
	require.Equal(t, 1, optionCalls)
}
