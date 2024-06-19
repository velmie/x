package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/velmie/x/envx"
)

const (
	defaultDBUnsafeDisableTLS = "false"
)

type Config struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string

	MaxOpenConnections int           // leave 0 to use default value
	MaxIdleConnections int           // leave 0 to use default value
	ConnMaxIdleTime    time.Duration // leave 0 to use default value
	ConnMaxLifetime    time.Duration // leave 0 to use default value

	TLSConfig *tls.Config
}

func ConfigFromEnv(envPrefix string) (*Config, error) {
	res := &Config{}
	p := envx.CreatePrototype().WithPrefix(envPrefix)

	var (
		disableTLS  bool
		tlsCertPath string
	)

	const (
		envNameUnsafeDisableTLS = "DB_UNSAFE_DISABLE_TLS"
		envNameTLSCertPath      = "DB_TLS_CERT_PATH"
	)

	err := envx.Supply(
		envx.Set(&res.Host, p.Get("DB_HOST").Required().NotEmpty().ValidURL().String),
		envx.Set(&res.Port, p.Get("DB_PORT").Required().NotEmpty().ValidPortNumber().Int),
		envx.Set(&res.Name, p.Get("DB_NAME").Required().NotEmpty().String),
		envx.Set(&res.User, p.Get("DB_USER").Required().NotEmpty().String),
		envx.Set(&res.Password, p.Get("DB_PASS").Required().NotEmpty().String),

		envx.Set(&res.MaxOpenConnections, p.Get("DB_MAX_OPEN_CONNECTIONS").Int),
		envx.Set(&res.MaxIdleConnections, p.Get("DB_MAX_IDLE_CONNECTIONS").Int),
		envx.Set(&res.ConnMaxLifetime, p.Get("DB_CONNECTION_MAX_LIFETIME").Duration),
		envx.Set(&res.ConnMaxIdleTime, p.Get("DB_CONNECTION_MAX_IDLE_TIME").Duration),

		envx.Set(&disableTLS, p.Get(envNameUnsafeDisableTLS).Default(defaultDBUnsafeDisableTLS).Boolean),
	)
	if err != nil {
		return nil, err
	}

	err = envx.Supply(
		envx.Set(&tlsCertPath, p.Get(envNameTLSCertPath).RequiredIf(!disableTLS).NotEmptyIf(!disableTLS).String),
	)
	if err != nil {
		err = fmt.Errorf(
			`%w. Set "%s=true" if you want to disable TLS`,
			err, withPrefix(envPrefix, envNameUnsafeDisableTLS),
		)
		return nil, err
	}

	if tlsCertPath != "" {
		pemFile, inErr := os.ReadFile(tlsCertPath)
		if inErr != nil {
			return nil, fmt.Errorf(
				"the environment variable '%s' has invalid value: %w",
				withPrefix(envPrefix, envNameTLSCertPath), inErr,
			)
		}

		rootCertPool := x509.NewCertPool()
		if ok := rootCertPool.AppendCertsFromPEM(pemFile); !ok {
			return nil, fmt.Errorf(
				"the environment variable '%s' has invalid value. Please make sure ypu use a PEM file",
				withPrefix(envPrefix, envNameTLSCertPath),
			)
		}
		res.TLSConfig = &tls.Config{
			RootCAs:    rootCertPool,
			ServerName: res.Host,
			MinVersion: tls.VersionTLS13,
		}
	}

	return res, err
}

func withPrefix(prefix, name string) string {
	return prefix + name
}
