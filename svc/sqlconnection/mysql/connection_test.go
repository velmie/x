package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	pingStage  = "ping"
	queryStage = "query"
	scanStage  = "scan"
)

func TestNewConnectionContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := NewConnectionContext(ctx, &Config{Host: "localhost", Port: 3306}, connectionLogger{})
	require.Nil(t, db)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewConnectionContextCancelsStalledHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	accepted, peerClosed := make(chan struct{}), make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			peerClosed <- acceptErr

			return
		}
		defer conn.Close()
		deadline, _ := ctx.Deadline()
		if deadlineErr := conn.SetReadDeadline(deadline); deadlineErr != nil {
			peerClosed <- deadlineErr

			return
		}
		close(accepted)
		var one [1]byte
		_, readErr := conn.Read(one[:])
		peerClosed <- readErr
	}()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		db, connectErr := NewConnectionContext(ctx, &Config{Host: host, Port: port}, connectionLogger{})
		if db != nil {
			_ = db.Close()
		}
		result <- connectErr
	}()
	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("connection never reached peer")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("handshake ignored cancellation")
	}
	select {
	case err := <-peerClosed:
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(3 * time.Second):
		t.Fatal("connection remained open after cancellation")
	}
}

func TestInitializeConnectionFailureClosesPool(t *testing.T) {
	for _, stage := range []string{pingStage, queryStage, scanStage} {
		for _, closeFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/close_error=%t", stage, closeFails), func(t *testing.T) {
				cause := &connectionError{message: "fixture sensitive dependency detail"}
				closeCause := errors.New("fixture close failure")
				conn := &initializationConn{}
				if closeFails {
					conn.closeErr = closeCause
				}
				switch stage {
				case pingStage:
					conn.ping = func(context.Context) error { return cause }
				case queryStage:
					conn.query = func(context.Context) (driver.Rows, error) { return nil, cause }
				case scanStage:
					conn.query = func(context.Context) (driver.Rows, error) {
						return &settingsRows{readErr: cause}, nil
					}
				}
				db := initializationDB(t, conn)
				var logs strings.Builder
				result, err := initializeConnection(context.Background(), db, &Config{}, connectionLogger{output: &logs})
				require.Nil(t, result)
				require.ErrorIs(t, err, cause)
				var typed *connectionError
				require.ErrorAs(t, err, &typed)
				require.Same(t, cause, typed)
				if closeFails {
					require.ErrorIs(t, err, closeCause)
				}
				require.EqualValues(t, 1, conn.closes.Load())
				require.Zero(t, db.Stats().OpenConnections)
				require.Error(t, db.PingContext(context.Background()))
				require.NotContains(t, logs.String(), "sensitive")
			})
		}
	}
}

func TestInitializeConnectionCancellation(t *testing.T) {
	for _, stage := range []string{pingStage, queryStage} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deadline=%t", stage, deadline), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
				}
				defer cancel()
				wait := func(callCtx context.Context) error {
					if !deadline {
						cancel()
					}
					select {
					case <-callCtx.Done():
						return callCtx.Err()
					case <-time.After(2 * time.Second):
						return errors.New("context did not reach driver")
					}
				}
				conn := &initializationConn{}
				if stage == pingStage {
					conn.ping = wait
				} else {
					conn.query = func(callCtx context.Context) (driver.Rows, error) { return nil, wait(callCtx) }
				}
				db := initializationDB(t, conn)
				result, err := initializeConnection(ctx, db, &Config{}, connectionLogger{})
				require.Nil(t, result)
				if deadline {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
				require.EqualValues(t, 1, conn.closes.Load())
				require.Zero(t, db.Stats().OpenConnections)
			})
		}
	}
}

func TestInitializeConnectionPoolSettings(t *testing.T) {
	for _, test := range []struct {
		name           string
		cfg            Config
		maxConnections int64
		wantOpen       int
		wantIdle       int
		wantQuery      bool
	}{
		{"defaults", Config{}, 100, 90, 10, true},
		{"minimum", Config{}, 1, 1, 1, true},
		{"explicit open", Config{MaxOpenConnections: 7}, 100, 7, 10, true},
		{"explicit idle", Config{MaxIdleConnections: 3}, 200, 180, 3, true},
		{"explicit settings", Config{MaxOpenConnections: 5, MaxIdleConnections: 3, ConnMaxIdleTime: time.Minute, ConnMaxLifetime: 2 * time.Minute}, 100, 5, 3, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			queried := false
			conn := &initializationConn{query: func(context.Context) (driver.Rows, error) {
				queried = true

				return &settingsRows{value: test.maxConnections}, nil
			}}
			db := initializationDB(t, conn)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := test.cfg
			result, err := initializeConnection(ctx, db, &cfg, connectionLogger{})
			require.NoError(t, err)
			require.Same(t, db, result)
			require.Equal(t, test.wantQuery, queried)
			require.Equal(t, test.wantOpen, cfg.MaxOpenConnections)
			require.Equal(t, test.wantIdle, cfg.MaxIdleConnections)
			require.Equal(t, test.wantOpen, db.Stats().MaxOpenConnections)
			if test.cfg.ConnMaxIdleTime == 0 {
				require.Equal(t, 10*time.Minute, cfg.ConnMaxIdleTime)
				require.Equal(t, time.Hour, cfg.ConnMaxLifetime)
			} else {
				require.Equal(t, test.cfg.ConnMaxIdleTime, cfg.ConnMaxIdleTime)
				require.Equal(t, test.cfg.ConnMaxLifetime, cfg.ConnMaxLifetime)
			}
			cancel()
			require.NoError(t, db.PingContext(context.Background()))
			require.Zero(t, conn.closes.Load())
		})
	}
}

func initializationDB(t *testing.T, conn *initializationConn) *sql.DB {
	t.Helper()
	db := sql.OpenDB(initializationConnector{conn: conn})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	return db
}

type initializationConnector struct{ conn *initializationConn }

func (c initializationConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (initializationConnector) Driver() driver.Driver                          { return initializationDriver{} }

type initializationDriver struct{}

func (initializationDriver) Open(string) (driver.Conn, error) { panic("use connector") }

type initializationConn struct {
	ping     func(context.Context) error
	query    func(context.Context) (driver.Rows, error)
	closeErr error
	closes   atomic.Int32
}

func (c *initializationConn) Ping(ctx context.Context) error {
	if c.ping != nil {
		return c.ping(ctx)
	}

	return nil
}

func (c *initializationConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.query != nil {
		return c.query(ctx)
	}

	return &settingsRows{value: int64(100)}, nil
}

func (c *initializationConn) Close() error {
	c.closes.Add(1)

	return c.closeErr
}

func (*initializationConn) Begin() (driver.Tx, error)           { panic("unexpected Begin") }
func (*initializationConn) Prepare(string) (driver.Stmt, error) { panic("unexpected Prepare") }

type settingsRows struct {
	value   driver.Value
	read    bool
	readErr error
}

func (*settingsRows) Columns() []string { return []string{"Variable_name", "Value"} }
func (*settingsRows) Close() error      { return nil }
func (r *settingsRows) Next(values []driver.Value) error {
	if r.readErr != nil {
		return r.readErr
	}
	if r.read {
		return io.EOF
	}
	r.read = true
	values[0], values[1] = "max_connections", r.value

	return nil
}

type connectionError struct{ message string }

func (e *connectionError) Error() string { return e.message }

type connectionLogger struct{ output io.Writer }

func (l connectionLogger) Info(msg string, args ...any) {
	if l.output != nil {
		_, _ = fmt.Fprintln(l.output, msg, args)
	}
}
