package errorsx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/velmie/x/svc/errorsx"
)

type customError struct {
	message string
}

func (c *customError) Error() string {
	return c.message
}

func (c *customError) Unwrap() error {
	return errors.New("wrapped by the custom error")
}

func TestAs(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		expectedErr error
	}{
		{
			name:        "Exact error type match",
			err:         &customError{message: "exact match"},
			expectedErr: &customError{message: "exact match"},
		},
		{
			name:        "Wrapped error type match",
			err:         fmt.Errorf("this is wrapped error: %w", &customError{"wrapped"}),
			expectedErr: &customError{message: "wrapped"},
		},
		{
			name:        "No match in chain",
			err:         fmt.Errorf("%w", errors.New("something went wrong")),
			expectedErr: nil,
		},
		{
			name:        "nil error",
			err:         nil,
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ce := errorsx.As[*customError](tt.err)
			if tt.expectedErr == nil && ce != nil {
				t.Errorf("expected nil, got %v", ce)
				return
			}
			if tt.expectedErr == nil && ce == nil {
				return
			}
			if tt.expectedErr.Error() != ce.Error() {
				t.Errorf("expected %s, got %s", tt.expectedErr, ce)
				return
			}
		})
	}
}

func TestUnwrapF(t *testing.T) {
	tests := []struct {
		name string
		err  error
		f    func(error) bool
		want bool
	}{
		{
			name: "Simple error without wrapping, f returns true",
			err:  errors.New("simple error"),
			f:    func(e error) bool { return e.Error() == "simple error" },
			want: true,
		},
		{
			name: "Simple error without wrapping, f returns false",
			err:  errors.New("simple error"),
			f:    func(e error) bool { return e.Error() == "different error" },
			want: false,
		},
		{
			name: "Wrapped error, f returns true for inner error",
			err:  fmt.Errorf("outer error: %w", errors.New("inner error")),
			f:    func(e error) bool { return e.Error() == "inner error" },
			want: true,
		},
		{
			name: "Wrapped error, f returns true for outer error",
			err:  fmt.Errorf("outer error: %w", errors.New("inner error")),
			f:    func(e error) bool { return e.Error() == "outer error: inner error" },
			want: true,
		},
		{
			name: "Wrapped error, f returns false for both errors",
			err:  fmt.Errorf("outer error: %w", errors.New("inner error")),
			f:    func(e error) bool { return e.Error() == "different error" },
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorsx.UnwrapF(tt.err, tt.f)
			if got != tt.want {
				t.Errorf("UnwrapF() = %v, want %v", got, tt.want)
			}
		})
	}
}
