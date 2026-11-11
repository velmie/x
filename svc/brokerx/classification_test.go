package brokerx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/velmie/broker"
	"github.com/velmie/x/sqltx"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
)

func TestCanceledBeginIsCooperative(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wrapper := sqltx.NewDefaultWrapper(db, nil)
	handler := broker.NewHandler(func(ctx context.Context, _ broker.Delivery) (broker.Disposition, error) {
		return nil, wrapper.WithTransaction(ctx, func(context.Context) error { return nil })
	}).WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{}))
	result, err := handler.Handle(ctx, broker.NewDelivery(broker.Message{}, "orders"))
	if result != nil || err != ctx.Err() {
		t.Fatalf("result=%v err=%v, want %v", result, err, ctx.Err())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWrappedDecodeIsRejected(t *testing.T) {
	cause := fmt.Errorf("middleware: %w", &broker.DecodeError{
		Cause: errors.New("invalid message"),
	})
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, cause
	}).WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{}))
	result, err := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if _, ok := result.(broker.Reject); !ok || err != nil {
		t.Fatalf("result=%T err=%v", result, err)
	}
}

func TestDecodeBeforeNestedHandleStageIsRejected(t *testing.T) {
	cause := &broker.DecodeError{
		Cause: &broker.StageError{
			Stage: broker.StageHandle,
			Cause: errors.New("invalid message"),
		},
	}
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, cause
	}).WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{}))
	result, err := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if _, ok := result.(broker.Reject); !ok || err != nil {
		t.Fatalf("result=%T err=%v", result, err)
	}
}

func TestTransactionPanicWithDecodeCauseIsRetried(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectRollback()
	wrapper := sqltx.NewDefaultWrapper(db, nil)
	handler := broker.NewHandler(func(ctx context.Context, _ broker.Delivery) (broker.Disposition, error) {
		return nil, wrapper.WithTransaction(ctx, func(context.Context) error {
			panic(&broker.DecodeError{
				Cause: errors.New("panic value"),
			})
		})
	}).WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{}))
	result, err := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if _, ok := result.(broker.RetryAfter); !ok || err != nil {
		t.Fatalf("result=%T err=%v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessingLogIncludesAllViolations(t *testing.T) {
	cause := &apperror.Error{
		Category: apperror.Invalid,
		Violations: []apperror.Violation{
			{
				Field: "first",
				Rule:  "required",
			},
			{
				Field: "second",
				Rule:  "max",
				Param: "5",
			},
		},
		Cause: &broker.FieldError{
			Field:  "third",
			Reason: "required",
			Cause:  errors.New("required"),
		},
	}
	var output bytes.Buffer
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, cause
	}).WithMiddleware(
		broker.LogProcessing(slog.New(slog.NewJSONHandler(&output, nil)), brokerx.ProcessingLogConfig(brokerx.ErrorPolicy{})),
		brokerx.HandleErrors(brokerx.ErrorPolicy{}),
	)
	_, err := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Attributes map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	var violations []map[string]string
	if err := json.Unmarshal(record.Attributes["violations"], &violations); err != nil {
		t.Fatalf("violations missing: %v, log %s", err, output.String())
	}
	if len(violations) != 2 || violations[0]["field"] != "first" || violations[0]["rule"] != "required" ||
		violations[0]["param"] != "" || violations[1]["field"] != "second" ||
		violations[1]["rule"] != "max" || violations[1]["param"] != "5" {
		t.Fatalf("violations: %#v", violations)
	}
	if _, ok := violations[0]["param"]; ok {
		t.Fatal("empty param must be omitted")
	}
	if _, ok := record.Attributes["field"]; ok {
		t.Fatal("field error attributes must not duplicate application violations")
	}
}

func TestProcessingLogLevel(t *testing.T) {
	retryable := errors.New("retryable")
	failure := errors.New("failure")
	policy := brokerx.ErrorPolicy{
		Retryable: func(err error) bool { return errors.Is(err, retryable) },
	}
	config := brokerx.ProcessingLogConfig(policy)
	for _, tt := range []struct {
		name   string
		result broker.Disposition
		err    error
		want   slog.Level
	}{
		{
			name: "retryable",
			result: broker.RetryAfter{
				Cause: retryable,
			},
			want: slog.LevelWarn,
		},
		{
			name: "other retry",
			result: broker.RetryAfter{
				Cause: failure,
			},
			want: slog.LevelError,
		},
		{
			name: "reject",
			result: broker.Reject{
				Cause: failure,
			},
			want: slog.LevelInfo,
		},
		{
			name: "failure",
			err:  failure,
			want: slog.LevelError,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.Level(tt.result, tt.err); got != tt.want {
				t.Fatalf("level=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestProcessingLogLevelWithoutRetryCause(t *testing.T) {
	config := brokerx.ProcessingLogConfig(brokerx.ErrorPolicy{
		Retryable: func(err error) bool { return err.Error() == "expected" },
	})
	if got := config.Level(broker.RetryAfter{}, nil); got != slog.LevelError {
		t.Fatalf("level=%v, want ERROR", got)
	}
}
