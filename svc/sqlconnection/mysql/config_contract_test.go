//nolint:goconst // Independent test cases and examples repeat literal contract values.
package mysql

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigFromEnvBindsEveryTaggedField(t *testing.T) {
	caFile, _ := certificateFile(t)
	values := map[string]string{
		"Host":               "mysql.example.test",
		"Port":               "3307",
		"Name":               "example_db",
		"User":               "example_user",
		"Password":           "example_password",
		"MaxOpenConnections": "7",
		"MaxIdleConnections": "6",
		"ConnMaxLifetime":    "4m",
		"ConnMaxIdleTime":    "2m",
		"UnsafeDisableTLS":   "false",
		"TLSCertPath":        caFile,
		"TLSServerName":      "db.example.test",
		"ConnectTimeout":     "5s",
		"ReadTimeout":        "9s",
		"WriteTimeout":       "11s",
		"Params":             "rejectReadOnly=true",
	}
	want := Config{
		Host:               "mysql.example.test",
		Port:               3307,
		Name:               "example_db",
		User:               "example_user",
		Password:           "example_password",
		MaxOpenConnections: 7,
		MaxIdleConnections: 6,
		ConnMaxLifetime:    4 * time.Minute,
		ConnMaxIdleTime:    2 * time.Minute,
		UnsafeDisableTLS:   false,
		TLSCertPath:        caFile,
		TLSServerName:      "db.example.test",
		ConnectTimeout:     5 * time.Second,
		ReadTimeout:        9 * time.Second,
		WriteTimeout:       11 * time.Second,
		Params:             "rejectReadOnly=true",
	}
	const prefix = "MYSQL_BINDING_TEST_"
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Tag.Get("k")
		if name == "-" {
			continue
		}
		require.NotEmpty(t, name, field.Name)
		value, ok := values[field.Name]
		require.True(t, ok, "missing test value for %s", field.Name)
		t.Setenv(prefix+"DB_"+strings.ToUpper(name), value)
	}
	got, err := ConfigFromEnv(prefix)
	require.NoError(t, err)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get("k") == "-" {
			continue
		}
		require.Equal(t, reflect.ValueOf(want).Field(i).Interface(), reflect.ValueOf(*got).Field(i).Interface(), field.Name)
	}
	require.Len(t, values, typ.NumField()-2)
	require.NotNil(t, got.TLSConfig)
	require.Equal(t, "db.example.test", got.TLSConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS13), got.TLSConfig.MinVersion)
}

func TestConfigFromEnvDefaultsAndScalarErrors(t *testing.T) {
	const prefix = "MYSQL_DEFAULTS_TEST_"
	for key, value := range map[string]string{
		"HOST": "localhost",
		"PORT": "3306",
		"NAME": "example",
		"USER": "example",
		"PASS": "example-only",
	} {
		t.Setenv(prefix+"DB_"+key, value)
	}
	cfg, err := ConfigFromEnv(prefix)
	require.NoError(t, err)
	require.Zero(t, cfg.MaxOpenConnections)
	require.Zero(t, cfg.MaxIdleConnections)
	require.Zero(t, cfg.ConnMaxLifetime)
	require.Zero(t, cfg.ConnMaxIdleTime)
	require.Zero(t, cfg.ConnectTimeout)
	require.False(t, cfg.UnsafeDisableTLS)
	require.NotNil(t, cfg.TLSConfig)
	require.Nil(t, cfg.TLSConfig.RootCAs)
	require.Equal(t, "localhost", cfg.TLSConfig.ServerName)

	t.Setenv(prefix+"DB_CONNECT_TIMEOUT", "not-a-duration")
	_, err = ConfigFromEnv(prefix)
	require.ErrorContains(t, err, prefix+"DB_CONNECT_TIMEOUT")
	require.ErrorContains(t, err, "duration")

	t.Setenv(prefix+"DB_CONNECT_TIMEOUT", "0s")
	t.Setenv(prefix+"DB_UNSAFE_DISABLE_TLS", "true")
	cfg, err = ConfigFromEnv(prefix)
	require.NoError(t, err)
	require.True(t, cfg.UnsafeDisableTLS)
	require.Nil(t, cfg.TLSConfig)

	t.Setenv(prefix+"DB_TLS_SERVER_NAME", "other.example.test")
	_, err = ConfigFromEnv(prefix)
	require.ErrorIs(t, err, errConflictingTLS)
}

func TestConfigValidateReportsFieldsAndNeverReadsCA(t *testing.T) {
	base := Config{
		Host:        "mysql.example.test",
		Port:        3306,
		Name:        "example",
		User:        "example",
		Password:    "secret-not-for-diagnostics",
		Params:      "sensitive-session-setting-not-for-diagnostics",
		TLSCertPath: filepath.Join(t.TempDir(), "does-not-exist.pem"),
	}
	require.NoError(t, base.Validate())
	for _, test := range []struct {
		name   string
		change func(*Config)
		field  string
		rule   string
	}{
		{
			name:   "host",
			change: func(c *Config) { c.Host = "bad%host" },
			field:  "host",
			rule:   "hostname|ip",
		},
		{
			name:   "port low",
			change: func(c *Config) { c.Port = -1 },
			field:  "port",
			rule:   "gte=1",
		},
		{
			name:   "port high",
			change: func(c *Config) { c.Port = 65536 },
			field:  "port",
			rule:   "lte=65535",
		},
		{
			name:   "name",
			change: func(c *Config) { c.Name = "" },
			field:  "name",
			rule:   "required",
		},
		{
			name:   "user",
			change: func(c *Config) { c.User = "" },
			field:  "user",
			rule:   "required",
		},
		{
			name:   "password",
			change: func(c *Config) { c.Password = "" },
			field:  "pass",
			rule:   "required",
		},
		{
			name:   "connect timeout",
			change: func(c *Config) { c.ConnectTimeout = -1 },
			field:  "connect_timeout",
			rule:   "gte=0",
		},
		{
			name:   "read timeout",
			change: func(c *Config) { c.ReadTimeout = -1 },
			field:  "read_timeout",
			rule:   "gte=0",
		},
		{
			name:   "write timeout",
			change: func(c *Config) { c.WriteTimeout = -1 },
			field:  "write_timeout",
			rule:   "gte=0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.change(&cfg)
			err := cfg.Validate()
			require.ErrorContains(t, err, test.field)
			require.ErrorContains(t, err, test.rule)
			require.NotContains(t, err.Error(), base.Password)
			require.NotContains(t, err.Error(), base.Params)
		})
	}
	for _, host := range []string{
		"127.0.0.1",
		"::1",
		"example.test",
	} {
		cfg := base
		cfg.Host = host
		require.NoError(t, cfg.Validate(), host)
	}
}

func TestLoadTLSConfig(t *testing.T) {
	path, certificate := certificateFile(t)
	cfg, err := LoadTLSConfig(path, "db.example.test")
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	require.Equal(t, "db.example.test", cfg.ServerName)
	require.False(t, cfg.InsecureSkipVerify)
	_, err = certificate.Verify(x509.VerifyOptions{
		Roots:   cfg.RootCAs,
		DNSName: "db.example.test",
	})
	require.NoError(t, err)

	cfg, err = LoadTLSConfig("", "system.example.test")
	require.NoError(t, err)
	require.Nil(t, cfg.RootCAs)
	require.Equal(t, "system.example.test", cfg.ServerName)
	require.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)

	_, err = LoadTLSConfig(filepath.Join(t.TempDir(), "missing.pem"), "")
	require.ErrorIs(t, err, os.ErrNotExist)
	invalidPath := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(invalidPath, []byte("invalid PEM"), 0o600))
	_, err = LoadTLSConfig(invalidPath, "")
	require.ErrorIs(t, err, errInvalidPEM)
}

func TestTLSResolutionAndConflicts(t *testing.T) {
	custom := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "custom.example.test",
	}
	for _, test := range []struct {
		name    string
		cfg     Config
		want    *tls.Config
		wantErr bool
	}{
		{
			name: "plaintext",
			cfg: Config{
				UnsafeDisableTLS: true,
			},
		},
		{
			name: "CA conflicts",
			cfg: Config{
				UnsafeDisableTLS: true,
				TLSCertPath:      "no-file",
			},
			wantErr: true,
		},
		{
			name: "name conflicts",
			cfg: Config{
				UnsafeDisableTLS: true,
				TLSServerName:    "example.test",
			},
			wantErr: true,
		},
		{
			name: "config conflicts",
			cfg: Config{
				UnsafeDisableTLS: true,
				TLSConfig:        custom,
			},
			wantErr: true,
		},
		{
			name: "native overrides path and name",
			cfg: Config{
				TLSConfig:     custom,
				TLSCertPath:   "no-file",
				TLSServerName: "other",
			},
			want: custom,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.cfg.tlsConfig()
			if test.wantErr {
				require.ErrorIs(t, err, errConflictingTLS)
				_, err = NewConnectionContext(context.Background(), &test.cfg, connectionLogger{})
				require.ErrorIs(t, err, errConflictingTLS)

				return
			}
			require.NoError(t, err)
			require.Same(t, test.want, got)
		})
	}
	cfg := Config{
		Host: "db.example.test",
	}
	got, err := cfg.tlsConfig()
	require.NoError(t, err)
	require.Equal(t, "db.example.test", got.ServerName)
	cfg.TLSServerName = "override.example.test"
	got, err = cfg.tlsConfig()
	require.NoError(t, err)
	require.Equal(t, cfg.TLSServerName, got.ServerName)
}

func certificateFile(t *testing.T) (string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "db.example.test",
		},
		DNSNames: []string{
			"db.example.test",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	}), 0o600))

	return path, cert
}
