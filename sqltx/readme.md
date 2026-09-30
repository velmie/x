# sqltx - SQL Transaction Wrapper

`sqltx` is a Go package that provides a convenient way to work with SQL transactions in the context of the application.
It offers a way to wrap and share transactions within a context, as well as retrieve connections.

## Features

- Context-aware transaction management
- Supports nested transactions
- Panic recovery within transactions
- Returned transaction errors and structured rollback diagnostics


## Usage

### Creating a Default Wrapper

```go
db, err := sql.Open("driver", "your-database-connection-string")
if err != nil {
	log.Fatal(err)
}

logger := yourLoggerImplementation{}

wrapper := sqltx.NewDefaultWrapper(db, logger)
```

### Running transaction

To run a function within a transaction:

```go
err := wrapper.WithTransaction(ctx, func(ctx context.Context) error {
	// your database operations here
	// use wrapper.Connection(ctx) to get the current connection (db or transaction)
	return nil
})

if err != nil {
	log.Println("Error while performing transaction:", err)
}
```

This function will:

* Start a new transaction if there is not an ongoing one in the context.
* Use the ongoing transaction if there is one.
* Handle panics and rollbacks gracefully.
* Commit the transaction if no error returned.
* Rollback the transaction if the callback returns an error.

### Distinguishing transaction failures

Use `errors.Is(err, sqltx.ErrBegin)` to identify a failure to begin a transaction,
and `errors.Is(err, sqltx.ErrCommit)` to identify a failure returned by `Commit`.
Both errors unwrap to the original cause, so `errors.Is` and `errors.As` also
work for driver errors and context cancellation.

```go
switch {
case errors.Is(err, sqltx.ErrBegin):
	// The transaction did not begin and the callback was not called.
case errors.Is(err, sqltx.ErrCommit):
	// Commit failed. The transaction outcome needs application-specific handling.
case err != nil:
	// Handle the callback error or another transaction-lifetime error.
}
```

A commit failure is not proof that the transaction rolled back and is not, by
itself, permission to retry the operation. A driver may report a failure after
the server has committed the transaction.

An error returned by the callback is passed through unchanged. Nested calls
reuse the outer transaction without adding a Begin or Commit phase. If the
callback itself returns an error containing a phase marker, that marker remains
part of its error chain.

Begin errors are now wrapped. Consumers that compared `err == cause` must use
`errors.Is(err, cause)` instead. Existing function signatures and interfaces are
unchanged.

Cancellation is classified by the operation that observes it. A canceled
`BeginTx` matches `ErrBegin`. Cancellation after Begin but before the callback
returns `ctx.Err()` directly. A callback error stays unchanged. An error from
Commit matches `ErrCommit`, including a context error or `sql.ErrTxDone` caused
by cancellation racing with `database/sql` rollback.

Rollback failures are logged once with `operation=transaction`, `stage=rollback`,
the callback or panic trigger, and the original error object in `error`.
`sql.ErrTxDone` means the transaction already ended and is not logged as a new
rollback failure. The callback error is still returned unchanged. A recovered
panic whose value implements `error` preserves that cause for `errors.Is/As`.
The supplied logger must redact sensitive application data before output while
retaining useful failure details. Begin and Commit failures are returned for the
caller to handle and log.

### Getting the Current Connection

```go
conn := wrapper.Connection(ctx)
```
