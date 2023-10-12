package sqltx

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
)

// txKey used as a key for context in order to wrap database transaction
type txKey struct{}

// Wrapper defines a way to work with transaction
type Wrapper interface {
	// WithTransaction must use context in order to share transaction
	WithTransaction(ctx context.Context, f func(ctx context.Context) error, opts ...Option) error
	// Connection retrieves database transaction from the given context
	// if there is no transaction then it uses default connection
	Connection(ctx context.Context) Connection
}

// Connection represents a generic interface implemented by both sql.DB and sql.Tx.
type Connection interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// Logger specifies simple logg
type Logger interface {
	Warn(msg string, args ...any)
}

// DefaultWrapper implements Wrapper with Connection
type DefaultWrapper struct {
	db     *sql.DB
	logger Logger
}

// NewDefaultWrapper is DefaultWrapper constructor
func NewDefaultWrapper(db *sql.DB, logger Logger) *DefaultWrapper {
	return &DefaultWrapper{db: db, logger: logger}
}

func (g *DefaultWrapper) WithTransaction(ctx context.Context, f func(ctx context.Context) error, opts ...Option) (err error) {
	_, alreadyInTx := ctx.Value(txKey{}).(Connection)
	// if transaction already started then just pass existing context (nested transaction)
	// it ignores options
	if alreadyInTx {
		return f(ctx)
	}

	var txOpts *sql.TxOptions
	if len(opts) > 0 {
		txOpts = &sql.TxOptions{}
	}
	for _, opt := range opts {
		opt(txOpts)
	}
	tx, err := g.db.BeginTx(ctx, txOpts)
	if err != nil {
		return err
	}
	c := context.WithValue(ctx, txKey{}, tx)

	defer func() {
		if perr := recover(); perr != nil {
			rbErr := tx.Rollback()
			if rbErr != nil {
				g.logger.Warn("sqltx: transaction rollback error: " + rbErr.Error())
			}

			err = fmt.Errorf("panic recovered:\n%g\n%s", perr, stackTrace())
		}
	}()

	select {
	default:
	case <-ctx.Done():
		// if context is canceled then transaction is already rolled back
		return ctx.Err()
	}
	err = f(c)
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil && strings.Contains(err.Error(), "context canceled") {
			g.logger.Warn("sqltx: transaction rollback error: ", rbErr.Error())
		}
		return err
	}

	cErr := tx.Commit()
	if cErr != nil {
		g.logger.Warn("sqltx: transaction commit error: " + cErr.Error())
	}
	return err
}

func (g *DefaultWrapper) Connection(ctx context.Context) Connection {
	tx, ok := ctx.Value(txKey{}).(Connection)
	if !ok {
		return g.db
	}
	return tx
}

func stackTrace() string {
	const size = 4096
	buf := make([]byte, size)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
