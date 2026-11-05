package apperror_test

import (
	"errors"
	"fmt"

	"github.com/velmie/x/svc/apperror"
)

func ExampleError() {
	sentinel := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
		Message:  "book not found",
	}
	err := fmt.Errorf("load book: %w", sentinel)
	fmt.Println(errors.Is(err, sentinel))
	fmt.Println(err)
	// Output:
	// true
	// load book: book not found
}

func ExampleCategory_String() {
	fmt.Println(apperror.Precondition)
	fmt.Println(apperror.Category(42))
	// Output:
	// precondition
	// unknown(42)
}

func ExampleError_violations() {
	err := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "INVALID_REQUEST",
		Message:  "request fields are invalid",
		Violations: []apperror.Violation{
			{
				Field: "lines[0].quantity",
				Rule:  apperror.RuleGTE,
				Param: "1",
			},
			{
				Field: "title",
				Rule:  apperror.RuleRequired,
			},
		},
		Cause: errors.New("database cause"),
	}
	fmt.Println(err)
	// Output: request fields are invalid: lines[0].quantity (gte=1), title (required): database cause
}
