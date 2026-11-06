package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
)

type contractInput struct {
	Name string `json:"name" validate:"required,max=3"`
}

func TestContractNativeErrorsAndJSON(t *testing.T) {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(FieldNames("json"))
	if err := Register(v); err != nil {
		t.Fatal(err)
	}
	var dst contractInput
	err := DecodeJSON([]byte(`{"name":"long"}`), &dst, v)
	var fields validator.ValidationErrors
	if !errors.As(err, &fields) || len(fields) != 1 || FieldPath(dst, fields[0]) != "name" ||
		fields[0].Tag() != "max" || fields[0].Param() != "3" {
		t.Fatalf("field error: %v", err)
	}
	for _, tc := range []struct {
		input    string
		sentinel error
	}{
		{" \t\r\n", ErrEmptyInput},
		{"null", ErrObjectRequired},
		{"{} {}", ErrTrailingData},
		{string([]byte{0xff}), ErrInvalidUTF8},
	} {
		err := DecodeJSON([]byte(tc.input), &dst, v)
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("%q: %v", tc.input, err)
		}
	}
	err = DecodeJSON([]byte(`{"name":`), &dst, v)
	var syntax *json.SyntaxError
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncation cause: %v", err)
	}
	err = DecodeJSON([]byte(`{} !`), &dst, v)
	if !errors.Is(err, ErrTrailingData) || !errors.As(err, &syntax) {
		t.Fatalf("trailing syntax cause: %v", err)
	}
}

type embeddedLines struct {
	Lines []struct {
		Quantity int `json:"quantity" validate:"gt=0"`
	} `json:"lines" validate:"dive"`
}
type embeddedMetadata struct {
	ID string `json:"id" validate:"required"`
}
type namedRoot struct {
	embeddedLines
	embeddedMetadata
	Named   embeddedMetadata `json:"nested"`
	Ignored string           `json:"-" validate:"-"`
}

func TestFieldPaths(t *testing.T) {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(FieldNames("json"))
	value := namedRoot{embeddedLines: embeddedLines{Lines: []struct {
		Quantity int `json:"quantity" validate:"gt=0"`
	}{{}}}}
	err := v.Struct(value)
	var fields validator.ValidationErrors
	if !errors.As(err, &fields) {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for _, f := range fields {
		got[FieldPath(&value, f)] = true
	}
	for _, path := range []string{"lines[0].quantity", "id", "nested.id"} {
		if !got[path] {
			t.Errorf("missing %s: %v", path, got)
		}
	}
}

func TestJSONStandardSemantics(t *testing.T) {
	v := validator.New()
	for _, tc := range []struct {
		body string
		want *string
	}{
		{`{"Name":"one"}`, stringPointer("one")},
		{`{"name":"bad","name":"ok"}`, stringPointer("ok")},
		{`{"name":null}`, nil},
		{"{} \n\t", nil},
		{`{"name":"\ud800"}`, stringPointer("�")},
	} {
		var plain struct {
			Name *string `json:"name"`
		}
		if err := DecodeJSON([]byte(tc.body), &plain, v); err != nil {
			t.Errorf("%s: %v", tc.body, err)
			continue
		}
		if (plain.Name == nil) != (tc.want == nil) || plain.Name != nil && *plain.Name != *tc.want {
			t.Errorf("%s: got %v, want %v", tc.body, plain.Name, tc.want)
		}
	}
}

func stringPointer(value string) *string { return &value }

func TestNamingAndConcurrentReuse(t *testing.T) {
	v := validator.New(validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(FieldNames("k"))
	if err := Register(v); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			var input struct {
				Value string `k:"setting,omitempty" json:"value" validate:"required"`
			}
			err := DecodeJSON([]byte(`{}`), &input, v)
			var failures validator.ValidationErrors
			if !errors.As(err, &failures) || FieldPath(input, failures[0]) != "setting" || failures[0].Tag() != "required" {
				t.Fatalf("failure: %v", err)
			}
		})
	}
}

func TestJSONDecoderCauses(t *testing.T) {
	v := validator.New()
	for _, body := range []string{`{"name":3}`, `{"unknown":true}`, `{"name":]}`} {
		var input contractInput
		err := DecodeJSON([]byte(body), &input, v)
		if err == nil {
			t.Errorf("accepted %s", body)
		}
		if strings.Contains(body, `3`) {
			var cause *json.UnmarshalTypeError
			if !errors.As(err, &cause) {
				t.Errorf("type cause: %v", err)
			}
		}
		if strings.Contains(body, `]`) {
			var cause *json.SyntaxError
			if !errors.As(err, &cause) {
				t.Errorf("syntax cause: %v", err)
			}
		}
	}
}

func TestNativeExtensionsAndProgrammingErrors(t *testing.T) {
	v := validator.New()
	if err := Register(v); err != nil {
		t.Fatal(err)
	}
	if err := v.RegisterValidation("custom", func(f validator.FieldLevel) bool { return f.Field().String() == "ok" }); err != nil {
		t.Fatal(err)
	}
	v.RegisterAlias("custom_alias", "custom")
	err := v.Var("bad", "custom_alias")
	var fields validator.ValidationErrors
	if !errors.As(err, &fields) || fields[0].Tag() != "custom_alias" || fields[0].ActualTag() != "custom" {
		t.Fatalf("alias: %v", err)
	}
	var notStruct int
	err = DecodeJSON([]byte(`{}`), &notStruct, v)
	var decode *json.UnmarshalTypeError
	if !errors.As(err, &decode) {
		t.Fatalf("decode: %v", err)
	}
	var dst map[string]any
	err = DecodeJSON([]byte(`{}`), &dst, v)
	var invalid *validator.InvalidValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("validator configuration error: %v", err)
	}
}

func TestTrimmedNonblankAndUnits(t *testing.T) {
	v := validator.New()
	if err := Register(v); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"", false}, {"  ", false}, {"\u2003text", false}, {"text\u00a0", false}, {"text", true}, {"a b", true}, {string([]byte{0xff}), true},
	} {
		if TrimmedNonblank(tc.value) != tc.valid || (v.Var(tc.value, "trimmed_nonblank") == nil) != tc.valid {
			t.Errorf("value %q", tc.value)
		}
	}
	if v.Var("éé", "max=2") != nil || v.Var("éé", "maxbytes=2") == nil || v.Var("", "maxbytes=-1") == nil {
		t.Fatal("rune and byte limits differ")
	}
}
