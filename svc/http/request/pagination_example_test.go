package request_test

import (
	"fmt"

	"github.com/velmie/x/svc/http/request"
)

func ExampleResolvePage() {
	page := int64(2)
	limit := int64(25)
	window, err := request.ResolvePage(
		request.Page{
			Number: &page,
			Limit:  &limit,
		},
		request.PagePolicy{},
	)
	if err != nil {
		return
	}
	fmt.Println(window.Page, window.Limit, window.Offset)
	// Output: 2 25 25
}
