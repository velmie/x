package mysql

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	defaultTLSConfigName = "mysqlTLSConfig"

	defaultConnMaxIdleTime = 10 * time.Minute
	defaultConnMaxLifetime = 1 * time.Hour
)

type Logger interface {
	Info(msg string, args ...any)
}

// NewConnection creates new database connection
func NewConnection(cfg *Config, log Logger) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Name,
	)

	if cfg.TLSConfig != nil {
		if err := mysql.RegisterTLSConfig(defaultTLSConfigName, cfg.TLSConfig); err != nil {
			return nil, fmt.Errorf("cannot register mysql tls config: %w", err)
		}

		dsn += "&tls=" + defaultTLSConfigName
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot open mysql connection: %w", err)
	}

	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("mysql connection is not established: %w", err)
	}

	if cfg.MaxIdleConnections == 0 || cfg.MaxOpenConnections == 0 {
		var (
			maxConn int
			name    string
		)
		err = db.QueryRow("SHOW VARIABLES LIKE 'max_connections'").Scan(&name, &maxConn)
		if err != nil {
			return nil, fmt.Errorf("cannot get maximum number of connections: %w", err)
		}

		const (
			maxOpenConnsCoefficient = .9
			maxIdleConnsCoefficient = .1
		)

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
