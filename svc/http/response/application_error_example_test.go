package response_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/http/response"
)

func ExampleFromApplicationError() {
	err := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
		Message:  "book not found",
	}
	status, body := response.FromApplicationError(err, response.ErrorOptions{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/books/1", nil)
	if err := response.WriteErrors(w, r, status, body); err != nil {
		panic(err)
	}
	fmt.Printf("%d %s", w.Code, w.Body.String())
	// Output: 404 {"errors":[{"code":"BOOK_NOT_FOUND","title":"book not found","target":"common"}]}
}

func ExampleWriteErrors() {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/books/1", nil)
	body := response.Error(&response.HTTPError{
		Code:   "BOOK_NOT_FOUND",
		Title:  "book not found",
		Target: response.TargetCommon,
	})
	if err := response.WriteErrors(w, r, http.StatusNotFound, body); err != nil {
		panic(err)
	}
	fmt.Println(w.Code, w.Body.String())
	// Output: 404 {"errors":[{"code":"BOOK_NOT_FOUND","title":"book not found","target":"common"}]}
}
