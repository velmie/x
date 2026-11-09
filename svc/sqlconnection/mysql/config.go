package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	driver "github.com/go-sql-driver/mysql"
	"github.com/velmie/x/envx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/validation"
)

// Config holds database settings and optional native driver customization.
// The constructor writes computed pool defaults back to this value.
type Config struct {
	Host     string `k:"host" validate:"required,hostname|ip"`
	Port     int    `k:"port" validate:"required,gte=1,lte=65535"`
	Name     string `k:"name" validate:"required"`
	User     string `k:"user" validate:"required"`
	Password string `k:"pass" validate:"required" sensitive:"true"`

	MaxOpenConnections int           `k:"max_open_connections" default:"0" hint:"0 uses 10"`
	MaxIdleConnections int           `k:"max_idle_connections" default:"0" hint:"0 follows the effective open limit"`
	ConnMaxLifetime    time.Duration `k:"connection_max_lifetime" default:"0s" hint:"0 uses 3m"`
	ConnMaxIdleTime    time.Duration `k:"connection_max_idle_time" default:"0s" hint:"0 uses 3m"`

	UnsafeDisableTLS bool   `k:"unsafe_disable_tls" default:"false"`
	TLSCertPath      string `k:"tls_cert_path"`
	TLSServerName    string `k:"tls_server_name"`

	ConnectTimeout time.Duration `k:"connect_timeout" default:"0s" validate:"gte=0"`
	ReadTimeout    time.Duration `k:"read_timeout" default:"0s" validate:"gte=0"`
	WriteTimeout   time.Duration `k:"write_timeout" default:"0s" validate:"gte=0"`
	Params         string        `k:"params" hint:"Native DSN query syntax without ?; no credentials"`

	TLSConfig     *tls.Config     `k:"-" validate:"-"`
	DriverOptions []driver.Option `k:"-" validate:"-"`
}

// ConfigFromEnv loads and validates prefixed DB settings and prepares TLS.
func ConfigFromEnv(prefix string) (*Config, error) {
	cfg := &Config{}
	p := envx.CreatePrototype().WithPrefix(prefix)
	err := envx.Supply(
		envx.Set(&cfg.Host, configEnv(p, "Host").String),
		envx.Set(&cfg.Port, configEnv(p, "Port").Int),
		envx.Set(&cfg.Name, configEnv(p, "Name").String),
		envx.Set(&cfg.User, configEnv(p, "User").String),
		envx.Set(&cfg.Password, configEnv(p, "Password").String),
		envx.Set(&cfg.MaxOpenConnections, configEnv(p, "MaxOpenConnections").Int),
		envx.Set(&cfg.MaxIdleConnections, configEnv(p, "MaxIdleConnections").Int),
		envx.Set(&cfg.ConnMaxLifetime, configEnv(p, "ConnMaxLifetime").Duration),
		envx.Set(&cfg.ConnMaxIdleTime, configEnv(p, "ConnMaxIdleTime").Duration),
		envx.Set(&cfg.UnsafeDisableTLS, configEnv(p, "UnsafeDisableTLS").Boolean),
		envx.Set(&cfg.TLSCertPath, configEnv(p, "TLSCertPath").String),
		envx.Set(&cfg.TLSServerName, configEnv(p, "TLSServerName").String),
		envx.Set(&cfg.ConnectTimeout, configEnv(p, "ConnectTimeout").Duration),
		envx.Set(&cfg.ReadTimeout, configEnv(p, "ReadTimeout").Duration),
		envx.Set(&cfg.WriteTimeout, configEnv(p, "WriteTimeout").Duration),
		envx.Set(&cfg.Params, configEnv(p, "Params").String),
	)
	if err != nil {
		return nil, fmt.Errorf("load mysql environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.TLSConfig, err = cfg.tlsConfig()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks operator configuration without reading files or opening connections.
// Native driver options and parameters are applied and validated when opening a pool.
func (c *Config) Validate() error {
	if err := c.validateTLS(); err != nil {
		return err
	}

	return validation.FromStruct(c, configValidator.Struct(c))
}

// LoadTLSConfig loads optional PEM roots and returns verified TLS 1.3 configuration.
// An empty caFile uses the system root certificate pool.
func LoadTLSConfig(caFile, serverName string) (*tls.Config, error) {
	var roots *x509.CertPool
	if caFile != "" {
		pem, err := os.ReadFile(caFile) // #nosec G304 -- The CA file is explicitly selected by the operator.
		if err != nil {
			return nil, fmt.Errorf("read mysql TLS CA file: %w", err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("load mysql TLS CA file: %w", errInvalidPEM)
		}
	}

	return &tls.Config{
		RootCAs:    roots,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}, nil
}

var (
	errConflictingTLS = errors.New("unsafe_disable_tls cannot be combined with TLS certificate, server name, or TLSConfig")
	errInvalidPEM     = errors.New("file must contain at least one PEM certificate")
	configValidator   = newConfigValidator()
)

func newConfigValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))

	return v
}

func configEnv(p *envx.Prototype, fieldName string) *envx.Variable {
	field, _ := reflect.TypeOf(Config{}).FieldByName(fieldName)
	variable := p.Get("DB_" + strings.ToUpper(field.Tag.Get("k")))
	if value, ok := field.Tag.Lookup("default"); ok {
		variable.Default(value)
	}

	return variable
}

func (c *Config) validateTLS() error {
	if c.UnsafeDisableTLS && (c.TLSCertPath != "" || c.TLSServerName != "" || c.TLSConfig != nil) {
		var failures validation.Error
		failures.Add("unsafe_disable_tls", apperror.RuleEQ, "", errConflictingTLS)
		if c.TLSCertPath != "" {
			failures.Add("tls_cert_path", apperror.RuleEQ, "", nil)
		}
		if c.TLSServerName != "" {
			failures.Add("tls_server_name", apperror.RuleEQ, "", nil)
		}

		return failures.Err()
	}

	return nil
}

func (c *Config) tlsConfig() (*tls.Config, error) {
	if err := c.validateTLS(); err != nil {
		return nil, err
	}
	if c.UnsafeDisableTLS {
		return nil, nil //nolint:nilnil // A nil TLS config explicitly selects plaintext.
	}
	if c.TLSConfig != nil {
		return c.TLSConfig, nil
	}
	serverName := c.TLSServerName
	if serverName == "" {
		serverName = c.Host
	}

	return LoadTLSConfig(c.TLSCertPath, serverName)
}
