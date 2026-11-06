//nolint:goconst // Independent test cases and examples repeat literal contract values.
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

func TestNewConnectionContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := NewConnectionContext(ctx, &Config{
		Host: "localhost",
		Port: 3306,
	}, connectionLogger{})
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
		db, connectErr := NewConnectionContext(ctx, &Config{
			Host: host,
			Port: port,
		}, connectionLogger{})
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
	for _, closeFails := range []bool{
		false,
		true,
	} {
		t.Run(fmt.Sprintf("close_error=%t", closeFails), func(t *testing.T) {
			cause := &connectionError{
				message: "fixture sensitive dependency detail",
			}
			closeCause := errors.New("fixture close failure")
			conn := &initializationConn{
				ping: func(context.Context) error { return cause },
			}
			if closeFails {
				conn.closeErr = closeCause
			}
			db := initializationDB(t, conn)
			var logs strings.Builder
			result, err := initializeConnection(context.Background(), db, &Config{}, connectionLogger{
				output: &logs,
			})
			require.Nil(t, result)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, cause.Error())
			var typed *connectionError
			require.ErrorAs(t, err, &typed)
			require.Same(t, cause, typed)
			if closeFails {
				require.ErrorIs(t, err, closeCause)
			}
			require.EqualValues(t, 1, conn.closes.Load())
			require.Zero(t, db.Stats().OpenConnections)
			require.Error(t, db.PingContext(context.Background()))
			require.Empty(t, logs.String())
		})
	}
}

func TestInitializeConnectionCancellation(t *testing.T) {
	for _, deadline := range []bool{
		false,
		true,
	} {
		t.Run(fmt.Sprintf("deadline=%t", deadline), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			}
			defer cancel()
			conn := &initializationConn{
				ping: func(callCtx context.Context) error {
					if !deadline {
						cancel()
					}
					select {
					case <-callCtx.Done():
						return callCtx.Err()
					case <-time.After(2 * time.Second):
						return errors.New("context did not reach driver")
					}
				},
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

func TestInitializeConnectionPoolSettings(t *testing.T) {
	for _, test := range []struct {
		name        string
		cfg         Config
		wantOpen    int
		wantIdle    int
		wantLife    time.Duration
		wantIdleAge time.Duration
		writeOpen   int
		writeIdle   int
	}{
		{
			name:        "defaults",
			wantOpen:    10,
			wantIdle:    10,
			wantLife:    3 * time.Minute,
			wantIdleAge: 3 * time.Minute,
			writeOpen:   10,
			writeIdle:   10,
		},
		{
			name: "idle follows open",
			cfg: Config{
				MaxOpenConnections: 7,
			},
			wantOpen:    7,
			wantIdle:    7,
			wantLife:    3 * time.Minute,
			wantIdleAge: 3 * time.Minute,
			writeOpen:   7,
			writeIdle:   7,
		},
		{
			name: "clamped idle",
			cfg: Config{
				MaxOpenConnections: 4,
				MaxIdleConnections: 8,
			},
			wantOpen:    4,
			wantIdle:    4,
			wantLife:    3 * time.Minute,
			wantIdleAge: 3 * time.Minute,
			writeOpen:   4,
			writeIdle:   8,
		},
		{
			name: "unlimited open",
			cfg: Config{
				MaxOpenConnections: -1,
			},
			wantOpen:    0,
			wantIdle:    10,
			wantLife:    3 * time.Minute,
			wantIdleAge: 3 * time.Minute,
			writeOpen:   -1,
			writeIdle:   10,
		},
		{
			name: "negative disable",
			cfg: Config{
				MaxIdleConnections: -1,
				ConnMaxLifetime:    -1,
				ConnMaxIdleTime:    -1,
			},
			wantOpen:    10,
			wantIdle:    0,
			wantLife:    0,
			wantIdleAge: 0,
			writeOpen:   10,
			writeIdle:   -1,
		},
		{
			name: "explicit",
			cfg: Config{
				MaxOpenConnections: 5,
				MaxIdleConnections: 3,
				ConnMaxLifetime:    8 * time.Minute,
				ConnMaxIdleTime:    time.Minute,
			},
			wantOpen:    5,
			wantIdle:    3,
			wantLife:    8 * time.Minute,
			wantIdleAge: time.Minute,
			writeOpen:   5,
			writeIdle:   3,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &initializationConn{
				query: func(context.Context) (driver.Rows, error) { return nil, errors.New("unexpected query") },
			}
			db := initializationDB(t, conn)
			log := &recordingLogger{}
			cfg := test.cfg
			result, err := initializeConnection(context.Background(), db, &cfg, log)
			require.NoError(t, err)
			require.Same(t, db, result)
			require.Equal(t, test.writeOpen, cfg.MaxOpenConnections)
			require.Equal(t, test.writeIdle, cfg.MaxIdleConnections)
			require.Equal(t, test.wantOpen, db.Stats().MaxOpenConnections)
			require.Equal(t, []any{
				"maxOpenConnections",
				test.wantOpen,
				"maxIdleConnections",
				test.wantIdle,
				"connMaxLifetime",
				test.wantLife,
				"connMaxIdleTime",
				test.wantIdleAge,
			}, log.records[0])
			require.Len(t, log.records, 1)
			if test.wantIdle == 0 {
				require.Zero(t, db.Stats().Idle)
			} else {
				require.Equal(t, 1, db.Stats().Idle)
			}
		})
	}
}

func initializationDB(t *testing.T, conn *initializationConn) *sql.DB {
	t.Helper()
	db := sql.OpenDB(initializationConnector{
		conn: conn,
	})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	return db
}

type initializationConnector struct {
	conn *initializationConn
}

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

	return &settingsRows{
		value: int64(100),
	}, nil
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

func (*settingsRows) Columns() []string {
	return []string{
		"Variable_name",
		"Value",
	}
}
func (*settingsRows) Close() error { return nil }
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

type connectionError struct {
	message string
}

func (e *connectionError) Error() string { return e.message }

type connectionLogger struct {
	output io.Writer
}

func (l connectionLogger) Info(msg string, args ...any) {
	if l.output != nil {
		_, _ = fmt.Fprintln(l.output, msg, args)
	}
}
