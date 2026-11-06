package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

const (
	defaultMaxOpenConnections = 10
	defaultConnMaxLifetime    = 3 * time.Minute
	defaultConnMaxIdleTime    = 3 * time.Minute
)

// Logger receives connection initialization information.
type Logger interface {
	Info(msg string, args ...any)
}

// NewConnection opens and initializes a connection pool using context.Background.
// Use NewConnectionContext to bound initialization with cancellation or a deadline.
func NewConnection(cfg *Config, log Logger) (*sql.DB, error) {
	return NewConnectionContext(context.Background(), cfg, log)
}

// NewConnectionContext applies settings and verifies connectivity to a caller-owned pool.
// Failed initialization closes the pool and preserves the original and cleanup causes.
// Later cancellation of ctx does not close the pool. Computed defaults are written back to cfg.
func NewConnectionContext(ctx context.Context, cfg *Config, log Logger) (*sql.DB, error) {
	driverConfig, err := prepareDriverConfig(cfg)
	if err != nil {
		return nil, err
	}
	connector, err := driver.NewConnector(driverConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot open mysql connection: %w", err)
	}

	return initializeConnection(ctx, sql.OpenDB(connector), cfg, log)
}

var (
	errParamsSyntax = errors.New("params must use DSN query syntax without a leading ? and with escaped values")
	errParamsStrict = errors.New("params: strict is no longer supported by the mysql driver")
	errParamsTLS    = errors.New("params TLS settings conflict with the configured TLS mode or enable plaintext fallback")
)

func prepareDriverConfig(cfg *Config) (*driver.Config, error) {
	tlsConfig, err := cfg.tlsConfig()
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(cfg.Params, "?") || strings.Contains(cfg.Params, "/") {
		return nil, errParamsSyntax
	}
	// ParseDSN panics on the removed strict option instead of returning an error.
	for _, item := range strings.Split(cfg.Params, "&") {
		key, _, hasValue := strings.Cut(item, "=")
		if key == "strict" && hasValue {
			return nil, errParamsStrict
		}
	}
	query := "parseTime=true&timeout=" + cfg.ConnectTimeout.String() +
		"&readTimeout=" + cfg.ReadTimeout.String() + "&writeTimeout=" + cfg.WriteTimeout.String()
	if cfg.Params != "" {
		query += "&" + cfg.Params
	}
	dc, err := driver.ParseDSN("/?" + query)
	if err != nil {
		return nil, fmt.Errorf("parse mysql params: %w", err)
	}
	expectedTLS := "true"
	if cfg.UnsafeDisableTLS {
		expectedTLS = "false"
	}
	if dc.AllowFallbackToPlaintext || (dc.TLSConfig != "" && dc.TLSConfig != expectedTLS) {
		return nil, errParamsTLS
	}
	dc.User = cfg.User
	dc.Passwd = cfg.Password
	dc.DBName = cfg.Name
	dc.Net = "tcp"
	dc.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dc.TLSConfig = ""
	dc.TLS = tlsConfig
	if err := dc.Apply(cfg.DriverOptions...); err != nil {
		return nil, fmt.Errorf("apply mysql driver options: %w", err)
	}

	return dc, nil
}

func initializeConnection(ctx context.Context, db *sql.DB, cfg *Config, log Logger) (_ *sql.DB, err error) {
	defer func() {
		if err != nil {
			if closeErr := db.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("cannot close mysql connection: %w", closeErr))
			}
		}
	}()

	if cfg.MaxOpenConnections == 0 {
		cfg.MaxOpenConnections = defaultMaxOpenConnections
	}
	if cfg.MaxIdleConnections == 0 {
		cfg.MaxIdleConnections = cfg.MaxOpenConnections
		if cfg.MaxIdleConnections < 0 {
			cfg.MaxIdleConnections = defaultMaxOpenConnections
		}
	}
	if cfg.ConnMaxIdleTime == 0 {
		cfg.ConnMaxIdleTime = defaultConnMaxIdleTime
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = defaultConnMaxLifetime
	}
	db.SetMaxOpenConns(cfg.MaxOpenConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err = db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("mysql connection is not established: %w", err)
	}
	open := max(cfg.MaxOpenConnections, 0)
	idle := max(cfg.MaxIdleConnections, 0)
	if open > 0 && idle > open {
		idle = open
	}
	log.Info("mysql connection pool established",
		"maxOpenConnections", open,
		"maxIdleConnections", idle,
		"connMaxLifetime", max(cfg.ConnMaxLifetime, 0),
		"connMaxIdleTime", max(cfg.ConnMaxIdleTime, 0),
	)

	return db, nil
}
