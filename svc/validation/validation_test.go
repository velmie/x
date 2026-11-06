package validation

import (
	"errors"
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestNonzeroCanonicalUUID(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{
			name:  "v4",
			value: "123e4567-e89b-42d3-a456-426614174000",
			valid: true,
		},
		{
			name:  "v7",
			value: "01902abc-1234-7abc-8abc-123456789abc",
			valid: true,
		},
		{
			name:  "unrestricted version and variant",
			value: "00000000-0000-f000-f000-000000000000",
			valid: true,
		},
		{
			name:  "nonzero final digit",
			value: "00000000-0000-0000-0000-000000000001",
			valid: true,
		},
		{
			name:  "zero",
			value: "00000000-0000-0000-0000-000000000000",
		},
		{
			name: "empty",
		},
		{
			name:  "uppercase",
			value: "123E4567-e89b-42d3-a456-426614174000",
		},
		{
			name:  "compact",
			value: "123e4567e89b42d3a456426614174000",
		},
		{
			name:  "braces",
			value: "{123e4567-e89b-42d3-a456-426614174000}",
		},
		{
			name:  "urn",
			value: "urn:uuid:123e4567-e89b-42d3-a456-426614174000",
		},
		{
			name:  "separator",
			value: "123e4567_e89b-42d3-a456-426614174000",
		},
		{
			name:  "nonhex",
			value: "123g4567-e89b-42d3-a456-426614174000",
		},
		{
			name:  "unicode",
			value: "é3e4567-e89b-42d3-a456-426614174000",
		},
		{
			name:  "space",
			value: " 123e4567-e89b-42d3-a456-426614174000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NonzeroCanonicalUUID(tc.value); got != tc.valid {
				t.Fatalf("NonzeroCanonicalUUID() = %v, want %v", got, tc.valid)
			}
			v := registered(t)
			if got := v.Var(tc.value, "nonzero_canonical_uuid") == nil; got != tc.valid {
				t.Fatalf("nonzero_canonical_uuid accepted = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestRegisteredTextRules(t *testing.T) {
	v := registered(t)
	for _, tc := range []struct {
		name  string
		value string
		tag   string
		valid bool
	}{
		{
			name:  "empty utf8",
			tag:   "utf8",
			valid: true,
		},
		{
			name:  "unicode utf8",
			value: "Книга 📖",
			tag:   "utf8",
			valid: true,
		},
		{
			name:  "invalid utf8",
			value: string([]byte{0xff}),
			tag:   "utf8",
		},
		{
			name: "empty nonblank",
			tag:  "nonblank",
		},
		{
			name:  "ascii whitespace",
			value: " \t\r\n\v\f",
			tag:   "nonblank",
		},
		{
			name:  "unicode whitespace",
			value: "\u00a0\u2003\u2028",
			tag:   "nonblank",
		},
		{
			name:  "trimmed text",
			value: "  book \t",
			tag:   "nonblank",
			valid: true,
		},
		{
			name:  "nul is nonblank",
			value: "\x00",
			tag:   "nonblank",
			valid: true,
		},
		{
			name:  "control is nonblank",
			value: "\x1f",
			tag:   "nonblank",
			valid: true,
		},
		{
			name:  "zero width space",
			value: "\u200b",
			tag:   "nonblank",
			valid: true,
		},
		{
			name:  "invalid utf8 is independently nonblank",
			value: string([]byte{0xff}),
			tag:   "nonblank",
			valid: true,
		},
		{
			name:  "combined rules",
			value: string([]byte{0xff}),
			tag:   "utf8,nonblank",
		},
		{
			name:  "native limit",
			value: "abcd",
			tag:   "utf8,nonblank,max=3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Var(tc.value, tc.tag)
			if got := err == nil; got != tc.valid {
				t.Fatalf("validation accepted = %v, want %v", got, tc.valid)
			}
			if err != nil {
				if _, ok := err.(validator.ValidationErrors); !ok {
					t.Fatalf("error type = %T", err)
				}
			}
		})
	}
}

func TestNativeOptionalPointers(t *testing.T) {
	v := registered(t)
	type input struct {
		Value *string `validate:"omitnil,utf8,nonblank"`
	}
	if err := v.Struct(input{}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "\t", string([]byte{0xff})} {
		candidate := input{
			Value: &value,
		}
		if err := v.Struct(candidate); err == nil {
			t.Fatal("invalid present value accepted")
		}
	}
	value := "hello"
	candidate := input{
		Value: &value,
	}
	if err := v.Struct(candidate); err != nil {
		t.Fatal(err)
	}
}

func ExampleRegister() {
	v := validator.New(validator.WithRequiredStructEnabled())
	if err := Register(v); err != nil {
		panic(err)
	}
	input := struct {
		ID   string `validate:"nonzero_canonical_uuid"`
		Name string `validate:"utf8,nonblank,max=64"`
	}{
		ID:   "123e4567-e89b-42d3-a456-426614174000",
		Name: "A book",
	}
	if err := v.Struct(input); err != nil {
		panic(err)
	}
	// Output:
}

func registered(t *testing.T) *validator.Validate {
	t.Helper()
	v := validator.New(validator.WithRequiredStructEnabled())
	if err := Register(v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMaxBytes(t *testing.T) {
	v := registered(t)
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{
			value: "éa",
			valid: true,
		},
		{
			value: "éé",
			valid: false,
		},
	} {
		input := struct {
			Value string `validate:"maxbytes=3"`
		}{
			Value: tc.value,
		}
		err := v.Struct(input)
		if (err == nil) != tc.valid {
			t.Fatalf("value=%q err=%v", tc.value, err)
		}
		if err != nil {
			var failures validator.ValidationErrors
			if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Field() != "Value" ||
				failures[0].Param() != "3" || failures[0].Tag() != RuleMaxBytes {
				t.Fatalf("field or limit lost: %v", err)
			}
		}
	}
}
