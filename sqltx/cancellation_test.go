package sqltx_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/velmie/x/sqltx"
)

func TestWithTransaction_Cancellation(t *testing.T) {
	for _, stage := range []string{"after begin", "callback", "commit"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rolledBack := make(chan struct{})
			conn := &cancelConn{rolledBack: rolledBack}
			if stage == "after begin" {
				conn.onBegin = cancel
			}
			db := sql.OpenDB(cancelConnector{conn: conn})
			defer db.Close()
			wrapper := sqltx.NewDefaultWrapper(db, noopLogger{})
			err := wrapper.WithTransaction(ctx, func(ctx context.Context) error {
				if stage == "after begin" {
					t.Fatal("callback must not run after cancellation")
				}
				cancel()
				if stage == "callback" {
					return wrapper.WithTransaction(ctx, func(ctx context.Context) error { return ctx.Err() })
				}
				return nil
			})
			require.NotErrorIs(t, err, sqltx.ErrBegin)
			if stage == "commit" {
				require.ErrorIs(t, err, sqltx.ErrCommit)
				require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone), "unexpected commit cause: %v", err)
			} else {
				require.Same(t, context.Canceled, err)
				require.NotErrorIs(t, err, sqltx.ErrCommit)
			}
			select {
			case <-rolledBack:
			case <-time.After(5 * time.Second):
				t.Fatal("database/sql did not finish rollback")
			}
		})
	}
}

type cancelConnector struct{ conn *cancelConn }

func (c cancelConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c cancelConnector) Driver() driver.Driver                        { return cancelDriver{} }

type cancelDriver struct{}

func (cancelDriver) Open(string) (driver.Conn, error) { panic("use connector") }

type cancelConn struct {
	onBegin    func()
	rolledBack chan struct{}
}

func (c *cancelConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.onBegin != nil {
		c.onBegin()
	}
	return c, nil
}

func (*cancelConn) Begin() (driver.Tx, error)           { panic("use BeginTx") }
func (*cancelConn) Prepare(string) (driver.Stmt, error) { panic("unexpected Prepare") }
func (*cancelConn) Close() error                        { return nil }
func (*cancelConn) Commit() error                       { panic("canceled commit must not reach driver") }
func (c *cancelConn) Rollback() error                   { close(c.rolledBack); return nil }
