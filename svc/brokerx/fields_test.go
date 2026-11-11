package brokerx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/velmie/broker"
	"github.com/velmie/x/svc/brokerx"
	"github.com/velmie/x/svc/validation"
)

type limitError struct{}

func (limitError) Error() string { return "must be at most 5" }
func (limitError) Param() string { return "5" }

func TestFieldErrors(t *testing.T) {
	first := &broker.FieldError{
		Field:  "url",
		Reason: "required",
		Cause:  errors.New("required"),
	}
	second := &broker.FieldError{
		Field:  "retries",
		Reason: "max",
		Cause:  limitError{},
	}
	cause := errors.Join(fmt.Errorf("connection: %w", first), second)
	err := validation.At("broker", brokerx.FieldErrors(cause))
	var got *validation.Error
	if !errors.As(err, &got) || len(got.Violations) != 2 || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("error: %#v", err)
	}
	if got.Violations[0].Field != "broker.url" || got.Violations[0].Rule != "required" ||
		got.Violations[1].Field != "broker.retries" || got.Violations[1].Rule != "max" || got.Violations[1].Param != "5" {
		t.Fatalf("violations: %#v", got.Violations)
	}
	other := errors.New("other")
	if brokerx.FieldErrors(other) != other || brokerx.FieldErrors(nil) != nil {
		t.Fatal("non-field errors must remain unchanged")
	}
}
