# sqltx - SQL Transaction Wrapper

`sqltx` is a Go package that provides a convenient way to work with SQL transactions in the context of the application.
It offers a way to wrap and share transactions within a context, as well as retrieve connections.

## Features

- Context-aware transaction management
- Supports nested transactions
- Panic recovery within transactions
- Convenient logging for rollback and commit errors


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
* Rollback the transaction if error is returned.

### Getting the Current Connection

```go
conn := wrapper.Connection(ctx)
```