package slogx_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/velmie/x/svc/slogx"
)

func TestConfigValidationTagsMatchHandler(t *testing.T) {
	for _, fieldName := range []string{"Level", "Format"} {
		t.Run(fieldName, func(t *testing.T) {
			field, ok := reflect.TypeOf(slogx.Config{}).FieldByName(fieldName)
			if !ok {
				t.Fatal("missing configuration field")
			}
			tag := field.Tag.Get("validate")
			const prefix = "omitempty,oneofci="
			if !strings.HasPrefix(tag, prefix) {
				t.Fatalf("unexpected validation tag %q", tag)
			}
			names := strings.Fields(strings.TrimPrefix(tag, prefix))
			if len(names) == 0 {
				t.Fatal("validation tag has no values")
			}
			check := func(value string, wantError bool) {
				t.Helper()
				var cfg slogx.Config
				reflect.ValueOf(&cfg).Elem().FieldByName(fieldName).SetString(value)
				_, err := slogx.NewHandler(io.Discard, cfg)
				if (err != nil) != wantError {
					t.Errorf("value %q: error = %v, want error = %t", value, err, wantError)
				}
			}
			check("", false)
			for _, name := range names {
				lower := strings.ToLower(name)
				mixed := []rune(lower)
				for i := range mixed {
					if i%2 == 0 {
						mixed[i] = []rune(strings.ToUpper(string(mixed[i])))[0]
					}
				}
				for _, value := range []string{lower, strings.ToUpper(name), string(mixed)} {
					check(value, false)
				}
			}
			invalid := "not-a-configured-value"
			for _, name := range names {
				if strings.EqualFold(name, invalid) {
					t.Fatal("invalid test value is listed in the tag")
				}
			}
			check(invalid, true)
		})
	}
}
