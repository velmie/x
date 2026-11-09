package validation_test

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"

	"github.com/velmie/x/svc/validation"
)

func ExampleDecodeQuery() {
	type listQuery struct {
		Sort string   `json:"sort" query:"single" validate:"oneof=sku -sku"`
		SKUs []string `json:"sku" validate:"max=20,unique,dive,required"`
	}
	input := listQuery{
		Sort: "sku",
	}
	v := validator.New()
	if err := validation.DecodeQuery("sku=book-a&sku=book-b", &input, v); err != nil {
		return
	}
	fmt.Println(input.Sort, input.SKUs)
	// Output: sku [book-a book-b]
}

func ExampleDecodeQuery_single() {
	type listQuery struct {
		Sort string `json:"sort" query:"single"`
	}
	var input listQuery
	err := validation.DecodeQuery("sort=sku&SORT=", &input, validator.New())
	var failure *validation.QueryError
	if errors.As(err, &failure) {
		fmt.Println(failure.Field, failure.Rule, failure.Param)
	}
	// Output: sort max 1
}
