package sqltx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/velmie/x/sqltx"
)

func ExampleDefaultWrapper_WithTransaction_panic() {
	db, mock, err := sqlmock.New()
	if err != nil {
		panic(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectRollback()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wrapper := sqltx.NewDefaultWrapper(db, logger)
	notFound := errors.New("not found")
	err = wrapper.WithTransaction(context.Background(), func(context.Context) error {
		panic(notFound)
	})

	var panicErr *sqltx.PanicError
	switch {
	case errors.As(err, &panicErr):
		fmt.Println("internal failure")
	case errors.Is(err, notFound):
		fmt.Println("not found")
	}
	// Output: internal failure
}
