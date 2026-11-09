package transaction_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/transaction"
)

func TestNormalizePanicPreservesChain(t *testing.T) {
	cause := errors.New("transaction callback failed")
	panicErr := &sqltx.PanicError{Value: cause}
	original := fmt.Errorf("transaction: %w", panicErr)
	got, panicked := transaction.NormalizePanic(original)
	var app *apperror.Error
	if !panicked || !errors.As(got, &app) || app.Category != apperror.Internal || !errors.Is(got, cause) || !errors.Is(got, original) {
		t.Fatalf("panic=%t error=%v", panicked, got)
	}
	got, panicked = transaction.NormalizePanic(cause)
	if panicked || got != cause {
		t.Fatalf("ordinary error changed: panic=%t error=%v", panicked, got)
	}
}

func TestOnlyCancellationRequiresEveryCause(t *testing.T) {
	failure := errors.New("independent failure")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancellation", fmt.Errorf("request: %w", context.Canceled), true},
		{"joined cancellations", errors.Join(context.Canceled, context.DeadlineExceeded), true},
		{"independent failure", errors.Join(context.Canceled, failure), false},
		{"application rejection", &apperror.Error{Category: apperror.Invalid, Cause: context.Canceled}, false},
		{"transaction panic", &sqltx.PanicError{Value: context.Canceled}, false},
		{"uncertain commit", errors.Join(sqltx.ErrCommit, context.Canceled), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transaction.OnlyCancellation(tt.err, context.Canceled, context.DeadlineExceeded); got != tt.want {
				t.Fatalf("OnlyCancellation(%v)=%t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
