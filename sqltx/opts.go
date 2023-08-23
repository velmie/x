package sqltx

import "database/sql"

// Option is a function type that modifies the transaction options.
type Option func(opts *sql.TxOptions)

// ReadOnly returns an Option to set the transaction as read-only.
func ReadOnly() Option {
	return func(opts *sql.TxOptions) {
		// Setting the transaction to be read-only.
		opts.ReadOnly = true
	}
}

// WithIsolationLevel returns an Option to set the isolation level for the transaction.
// The 'level' parameter specifies the desired isolation level.
func WithIsolationLevel(level sql.IsolationLevel) Option {
	return func(opts *sql.TxOptions) {
		opts.Isolation = level
	}
}
