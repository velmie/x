package envx_test

import (
	"errors"
	"testing"

	. "github.com/velmie/x/envx"
)

func TestError_Error_NoCause(t *testing.T) {
	err := Error{
		VarName: "USERNAME",
		Reason:  "value is required",
		Cause:   nil,
	}
	want := `variable "USERNAME" value is required`
	if got := err.Error(); got != want {
		t.Errorf("Error.Error() = %v, want %v", got, want)
	}
}

func TestError_Error_WithCause(t *testing.T) {
	err := Error{
		VarName: "USERNAME",
		Reason:  "",
		Cause:   ErrRequired,
	}
	want := `variable "USERNAME": value is required`
	if got := err.Error(); got != want {
		t.Errorf("Error.Error() = %v, want %v", got, want)
	}
}

func TestError_Unwrap(t *testing.T) {
	err := Error{
		VarName: "USERNAME",
		Reason:  "some details",
		Cause:   ErrInvalidValue,
	}
	if !errors.Is(err, ErrInvalidValue) {
		t.Error("unexpected error cause")
	}
}
