package sqltx_test

import (
	"context"
	"database/sql"
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
