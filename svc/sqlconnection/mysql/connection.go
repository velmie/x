package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	defaultConnMaxIdleTime  = 10 * time.Minute
	defaultConnMaxLifetime  = 1 * time.Hour
	maxOpenConnsCoefficient = .9
	maxIdleConnsCoefficient = .1
)

type Logger interface {
	Info(msg string, args ...any)
}

// NewConnection opens and initializes a connection pool using context.Background.
// Use NewConnectionContext to bound initialization with cancellation or a deadline.
func NewConnection(cfg *Config, log Logger) (*sql.DB, error) {
	return NewConnectionContext(context.Background(), cfg, log)
}

// NewConnectionContext opens a pool, checks connectivity, and applies pool
// settings. Both Ping and the server-settings query use ctx. Failed
// initialization closes the pool and preserves the original error cause.
// On success the caller owns the pool and must close it. Later cancellation of
// ctx does not close the pool. Computed defaults are written back to cfg.
func NewConnectionContext(ctx context.Context, cfg *Config, log Logger) (*sql.DB, error) {
	driverConfig := mysql.NewConfig()
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.DBName = cfg.Name
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverConfig.ParseTime = true
	driverConfig.TLS = cfg.TLSConfig

	connector, err := mysql.NewConnector(driverConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot open mysql connection: %w", err)
	}

	return initializeConnection(ctx, sql.OpenDB(connector), cfg, log)
}

func initializeConnection(ctx context.Context, db *sql.DB, cfg *Config, log Logger) (_ *sql.DB, err error) {
	defer func() {
		if err != nil {
			if closeErr := db.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("cannot close mysql connection: %w", closeErr))
			}
		}
	}()

	err = db.PingContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("mysql connection is not established: %w", err)
	}
	if cfg.TLSConfig != nil {
		log.Info("TLS DB connection is established")
	} else {
		log.Info("DB connection is established")
	}

	if cfg.MaxIdleConnections == 0 || cfg.MaxOpenConnections == 0 {
		var (
			maxConn int
			name    string
		)
		err = db.QueryRowContext(ctx, "SHOW VARIABLES LIKE 'max_connections'").Scan(&name, &maxConn)
		if err != nil {
			return nil, fmt.Errorf("cannot get maximum number of connections: %w", err)
		}

		if cfg.MaxIdleConnections == 0 {
			maxIdleConn := int(float64(maxConn) * maxIdleConnsCoefficient)
			if maxIdleConn < 1 {
				maxIdleConn = 1
			}
			cfg.MaxIdleConnections = maxIdleConn
		}
		if cfg.MaxOpenConnections == 0 {
			maxC := int(float64(maxConn) * maxOpenConnsCoefficient)
			if maxC < 1 {
				maxC = 1
			}
			cfg.MaxOpenConnections = maxC
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

	log.Info("maximum number of database connections is set", "maxConn", cfg.MaxOpenConnections)
	log.Info("maximum number of idle database connections is set", "maxIdleConn", cfg.MaxIdleConnections)
	log.Info("maximum life time of idle database connections is set", "minutes", cfg.ConnMaxIdleTime.Minutes())
	log.Info("maximum life time of database connections is set", "minutes", cfg.ConnMaxLifetime.Minutes())

	return db, nil
}
