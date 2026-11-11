package transaction_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/transaction"
)

func TestClassifyRealTransactionOutcomes(t *testing.T) {
	for _, stage := range []string{"begin", "commit", "panic"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			cause := context.Canceled
			switch stage {
			case "begin":
				mock.ExpectBegin().WillReturnError(cause)
			case "commit":
				mock.ExpectBegin()
				mock.ExpectCommit().WillReturnError(cause)
			case "panic":
				mock.ExpectBegin()
				mock.ExpectRollback()
			}
			err = sqltx.NewDefaultWrapper(db, nil).WithTransaction(context.Background(), func(context.Context) error {
				if stage == "panic" {
					panic(cause)
				}
				return nil
			})
			if !errors.Is(err, cause) {
				t.Fatalf("cause missing: %v", err)
			}
			if got := transaction.OnlyCancellation(err, context.Canceled); got != (stage == "begin") {
				t.Fatalf("OnlyCancellation(%v) = %t", err, got)
			}
			_, panicked := transaction.NormalizePanic(err)
			if panicked != (stage == "panic") {
				t.Fatalf("NormalizePanic(%v) = %t", err, panicked)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func ExampleOnlyCancellation() {
	err := fmt.Errorf("begin transaction: %w", context.Canceled)
	fmt.Println(transaction.OnlyCancellation(err, context.Canceled))
	// Output: true
}

func ExampleNormalizePanic() {
	err, panicked := transaction.NormalizePanic(&sqltx.PanicError{
		Value: "unexpected",
	})
	fmt.Println(panicked, err != nil)
	// Output: true true
}
