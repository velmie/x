//nolint:goconst // Independent test cases and examples repeat literal contract values.
package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolDefaultsAreAppliedBeforePingWithoutCapacityQuery(t *testing.T) {
	cfg := Config{}
	var db *sql.DB
	conn := &initializationConn{
		ping: func(context.Context) error {
			require.Equal(t, 10, db.Stats().MaxOpenConnections)

			return nil
		},
		query: func(context.Context) (driver.Rows, error) {
			return nil, errors.New("unexpected capacity query")
		},
	}
	db = initializationDB(t, conn)
	log := &recordingLogger{}
	got, err := initializeConnection(context.Background(), db, &cfg, log)
	require.NoError(t, err)
	require.Same(t, db, got)
	require.Equal(t, 10, cfg.MaxOpenConnections)
	require.Equal(t, 10, cfg.MaxIdleConnections)
	require.Equal(t, 3*time.Minute, cfg.ConnMaxLifetime)
	require.Equal(t, 3*time.Minute, cfg.ConnMaxIdleTime)
	require.Len(t, log.records, 1)
	require.Equal(t, []any{
		"maxOpenConnections",
		10,
		"maxIdleConnections",
		10,
		"connMaxLifetime",
		3 * time.Minute,
		"connMaxIdleTime",
		3 * time.Minute,
	}, log.records[0])
}

type recordingLogger struct {
	records [][]any
}

func (l *recordingLogger) Info(_ string, args ...any) {
	l.records = append(l.records, args)
}
