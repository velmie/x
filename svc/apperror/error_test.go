package apperror_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/velmie/x/svc/apperror"
)

type domainError struct{}

func (*domainError) Error() string { return "database cause" }

func TestErrorIdentityAndDiagnostics(t *testing.T) {
	sentinel := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
		Message:  "book not found",
	}
	wrapped := fmt.Errorf("load book: %w", sentinel)
	if !errors.Is(wrapped, sentinel) || wrapped.Error() != "load book: book not found" {
		t.Fatalf("sentinel identity or message lost: %v", wrapped)
	}
	cause := &domainError{}
	err := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "INVALID_INPUT",
		Violations: []apperror.Violation{
			{
				Field:   "lines[0].quantity",
				Rule:    apperror.RuleGTE,
				Param:   "1",
				Message: "must be positive",
			},
			{
				Field: "title",
				Rule:  apperror.RuleRequired,
			},
		},
		Cause: cause,
	}
	var got *domainError
	if !errors.As(err, &got) || got != cause {
		t.Fatal("typed cause is not reachable")
	}
	want := "INVALID_INPUT: lines[0].quantity (gte=1): must be positive, title (required): database cause"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	for _, part := range []string{"INVALID_INPUT", "lines[0].quantity", "gte", "1", "must be positive", "database cause"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not contain %q", err, part)
		}
	}
	if got := (&apperror.Error{}).Error(); got != "internal" {
		t.Errorf("zero error = %q, want internal", got)
	}
}

func TestCategoryString(t *testing.T) {
	tests := []struct {
		category apperror.Category
		want     string
	}{
		{
			category: apperror.Internal,
			want:     "internal",
		},
		{
			category: apperror.Invalid,
			want:     "invalid",
		},
		{
			category: apperror.Forbidden,
			want:     "forbidden",
		},
		{
			category: apperror.NotFound,
			want:     "not_found",
		},
		{
			category: apperror.Conflict,
			want:     "conflict",
		},
		{
			category: apperror.Precondition,
			want:     "precondition",
		},
		{
			category: apperror.Unavailable,
			want:     "unavailable",
		},
		{
			category: 255,
			want:     "unknown(255)",
		},
	}
	for _, tt := range tests {
		if got := tt.category.String(); got != tt.want {
			t.Errorf("Category(%d).String() = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestConcurrentErrorInspection(t *testing.T) {
	cause := errors.New("original")
	err := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "INVALID_INPUT",
		Violations: []apperror.Violation{
			{
				Field: "name",
				Rule:  apperror.RuleMin,
				Param: "2",
			},
		},
		Cause: cause,
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !strings.Contains(err.Error(), "name") || !errors.Is(err, cause) {
				t.Error("inspection lost field or cause")
			}
		}()
	}
	wg.Wait()
	if err.Violations[0].Param != "2" || err.Cause != cause {
		t.Fatal("inspection mutated error")
	}
}
