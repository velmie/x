package sqltx_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	. "github.com/velmie/x/sqltx"
)

func TestWithTransaction_Success(t *testing.T) {
	db, mock := testDBWithMock(t)
	wrapper := NewDefaultWrapper(db, &noopLogger{})

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE test").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := wrapper.WithTransaction(context.Background(), func(ctx context.Context) error {
		_, err := wrapper.Connection(ctx).ExecContext(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY)")
		return err
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithTransaction_NestedTransaction(t *testing.T) {
	db, mock := testDBWithMock(t)
	wrapper := NewDefaultWrapper(db, &noopLogger{})

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE test").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := wrapper.WithTransaction(context.Background(), func(ctx context.Context) error {
		return wrapper.WithTransaction(ctx, func(ctx2 context.Context) error {
			_, err := wrapper.Connection(ctx2).ExecContext(ctx2, "CREATE TABLE test (id INTEGER PRIMARY KEY)")
			return err
		})
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithTransaction_Panic(t *testing.T) {
	db, mock := testDBWithMock(t)
	wrapper := NewDefaultWrapper(db, &noopLogger{})

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := wrapper.WithTransaction(context.Background(), func(ctx context.Context) error {
		panic("test panic")
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "panic recovered")
	require.Contains(t, err.Error(), "test panic")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithTransaction_ErrorPhase(t *testing.T) {
	for _, stage := range []string{"begin", "commit", "callback", "nested callback"} {
		t.Run(stage, func(t *testing.T) {
			db, mock := testDBWithMock(t)
			var wrapper Wrapper = NewDefaultWrapper(db, &noopLogger{})
			cause := &databaseError{message: "database operation failed"}
			if stage == "begin" {
				mock.ExpectBegin().WillReturnError(cause)
			} else {
				mock.ExpectBegin()
				if stage == "commit" {
					mock.ExpectCommit().WillReturnError(cause)
				} else {
					mock.ExpectRollback()
				}
			}

			called := false
			err := wrapper.WithTransaction(context.Background(), func(ctx context.Context) error {
				called = true
				switch stage {
				case "callback":
					return cause
				case "nested callback":
					return wrapper.WithTransaction(ctx, func(nested context.Context) error {
						require.Same(t, wrapper.Connection(ctx), wrapper.Connection(nested))
						return cause
					})
				default:
					return nil
				}
			})

			require.Equal(t, stage != "begin", called)
			require.ErrorIs(t, err, cause)
			var typed *databaseError
			require.ErrorAs(t, err, &typed)
			require.Same(t, cause, typed)
			switch stage {
			case "begin":
				require.ErrorIs(t, err, ErrBegin)
				require.NotErrorIs(t, err, ErrCommit)
				require.Same(t, cause, errors.Unwrap(err))
			case "commit":
				require.ErrorIs(t, err, ErrCommit)
				require.NotErrorIs(t, err, ErrBegin)
				require.Same(t, cause, errors.Unwrap(err))
			default:
				require.Same(t, cause, err)
				require.NotErrorIs(t, err, ErrBegin)
				require.NotErrorIs(t, err, ErrCommit)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWithTransaction_CanceledBeforeBegin(t *testing.T) {
	db, mock := testDBWithMock(t)
	wrapper := NewDefaultWrapper(db, &noopLogger{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := wrapper.WithTransaction(ctx, func(context.Context) error {
		t.Fatal("callback must not run after BeginTx fails")
		return nil
	})
	require.ErrorIs(t, err, ErrBegin)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrCommit)
	require.NoError(t, mock.ExpectationsWereMet())
}

func testDBWithMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, mock
}

type noopLogger struct{}

func (noopLogger) Warn(_ string, _ ...any) {
	return // do nothing
}

type databaseError struct{ message string }

func (e *databaseError) Error() string { return e.message }
