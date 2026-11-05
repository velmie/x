package validation_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/validation"
)

type violationLine struct {
	Quantity int `json:"quantity" validate:"gte=1"`
}

type violationProfile struct {
	Name string `json:"displayName" validate:"short_name"`
}

type violationRequest struct {
	Profile violationProfile `json:"profile" validate:"required"`
	Lines   []violationLine  `json:"lines" validate:"dive"`
	Private string           `json:"private" validate:"max=3"`
}

type noValueFailure struct {
	validator.FieldError
}

func (noValueFailure) Value() any { panic("Value must not be called") }

func TestViolations(t *testing.T) {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(validation.FieldNames("json"))
	v.RegisterAlias("short_name", "min=4")
	input := violationRequest{
		Profile: violationProfile{
			Name: "abc",
		},
		Lines: []violationLine{
			{
				Quantity: 0,
			},
			{
				Quantity: 0,
			},
		},
		Private: "CANARY_REJECTED_VALUE",
	}
	var failures validator.ValidationErrors
	if err := v.Struct(&input); !errors.As(err, &failures) {
		t.Fatalf("expected validation failures, got %v", err)
	}
	withoutValues := make(validator.ValidationErrors, 0, len(failures))
	for _, failure := range failures {
		withoutValues = append(
			withoutValues,
			noValueFailure{
				FieldError: failure,
			},
		)
	}
	got := validation.Violations(&input, withoutValues)
	want := []apperror.Violation{
		{
			Field: "profile.displayName",
			Rule:  "short_name",
			Param: "4",
		},
		{
			Field: "lines[0].quantity",
			Rule:  "gte",
			Param: "1",
		},
		{
			Field: "lines[1].quantity",
			Rule:  "gte",
			Param: "1",
		},
		{
			Field: "private",
			Rule:  "max",
			Param: "3",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %#v, want %#v", got, want)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "CANARY_REJECTED_VALUE") {
		t.Fatal("projection exposed rejected value")
	}
	if got := validation.Violations(&input, nil); len(got) != 0 {
		t.Fatalf("empty failures = %#v", got)
	}
}
