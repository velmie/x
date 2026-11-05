package validation_test

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/validation"
)

func ExampleViolations() {
	type input struct {
		Quantity int `json:"quantity" validate:"gte=1"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("json"))
	dto := input{
		Quantity: 0,
	}
	var failures validator.ValidationErrors
	if err := v.Struct(&dto); errors.As(err, &failures) {
		for _, violation := range validation.Violations(&dto, failures) {
			fmt.Println(violation.Field, violation.Rule, violation.Param)
		}
	}
	// Output: quantity gte 1
}
