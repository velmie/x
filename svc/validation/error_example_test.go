package validation_test

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"

	"github.com/velmie/x/svc/validation"
)

func ExampleFromStruct() {
	type config struct {
		Port int `k:"port" validate:"gte=1"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	cfg := config{}
	var failure *validation.Error
	if errors.As(validation.FromStruct(cfg, v.Struct(cfg)), &failure) {
		fmt.Println(failure.Violations[0].Field, failure.Violations[0].Rule, failure.Violations[0].Param)
	}
	// Output: port gte 1
}

func ExampleInvalid() {
	err := validation.Invalid("tls_cert_path", apperror.RuleEQ, "", errors.New("conflicting TLS settings"))
	fmt.Println(err)
	// Output: tls_cert_path: eq: conflicting TLS settings
}

func ExampleAt() {
	err := validation.Invalid("host", apperror.RuleRequired, "", nil)
	fmt.Println(validation.At("db", err))
	// Output: db.host: required
}

func ExampleError() {
	type authConfig struct {
		Issuer string `k:"issuer" validate:"required"`
		Retry  int    `k:"retry"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	cfg := authConfig{
		Retry: -1,
	}
	var failures validation.Error
	failures.AddStruct(cfg, v.Struct(cfg))
	if cfg.Retry < 0 {
		failures.Add("retry", apperror.RuleGTE, "0", nil)
	}
	var failure *validation.Error
	if errors.As(validation.At("auth", failures.Err()), &failure) {
		for _, violation := range failure.Violations {
			fmt.Printf("%s %s %q\n", violation.Field, violation.Rule, violation.Param)
		}
	}
	// Output:
	// auth.issuer required ""
	// auth.retry gte "0"
}
