package connectx_test

import (
	"fmt"

	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/connectx"
)

func ExampleCode() {
	fmt.Println(connectx.Code(apperror.Conflict))
	// Output: aborted
}

func ExampleFromApplicationError() {
	err := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
		Message:  "book not found",
	}
	rpcErr := connectx.FromApplicationError(err, connectx.ErrorOptions{})
	fmt.Println(rpcErr.Code(), rpcErr.Message())
	// Output: not_found book not found
}
