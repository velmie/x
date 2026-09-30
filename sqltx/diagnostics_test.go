package sqltx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/velmie/x/sqltx"
)

func TestWithTransaction_RollbackFailureRetainsCause(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback", true: "panic"}[panics], func(t *testing.T) {
			db, mock := testDBWithMock(t)
			primary := &databaseError{message: "operation rejected"}
			rollback := &databaseError{message: "rollback connection failed"}
			logger := new(rollbackLogger)
			wrapper := sqltx.NewDefaultWrapper(db, logger)
			mock.ExpectBegin()
			mock.ExpectRollback().WillReturnError(rollback)
			err := wrapper.WithTransaction(context.Background(), func(context.Context) error {
				if panics {
					panic(primary)
				}
				return primary
			})
			if panics {
				require.ErrorIs(t, err, primary)
				var typed *databaseError
				require.ErrorAs(t, err, &typed)
				require.Same(t, primary, typed)
			} else {
				require.Same(t, primary, err)
			}
			require.Len(t, logger.records, 1)
			require.Same(t, rollback, logger.records[0]["error"])
			require.Equal(t, "transaction", logger.records[0]["operation"])
			require.Equal(t, "rollback", logger.records[0]["stage"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWithTransaction_RollbackFailureAfterCancellation(t *testing.T) {
	db, mock := testDBWithMock(t)
	logger := new(rollbackLogger)
	wrapper := sqltx.NewDefaultWrapper(db, logger)
	rollback := errors.New("rollback failed")
	mock.ExpectBegin()
	mock.ExpectRollback().WillReturnError(rollback)
	err := wrapper.WithTransaction(context.Background(), func(context.Context) error { return context.Canceled })
	require.Same(t, context.Canceled, err)
	require.Len(t, logger.records, 1)
	require.Same(t, rollback, logger.records[0]["error"])
	require.NoError(t, mock.ExpectationsWereMet())
}

type rollbackLogger struct{ records []map[string]any }

func (l *rollbackLogger) Warn(_ string, args ...any) {
	fields := make(map[string]any)
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			fields[name] = args[i+1]
		}
	}
	l.records = append(l.records, fields)
}
