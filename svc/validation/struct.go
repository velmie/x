package validation

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldNames returns native tag-based naming with untagged embedded structs flattened.
// Configure WithTagNameFuncBlankOmit on the validator to flatten embedded fields.
func FieldNames(fieldTag string) validator.TagNameFunc {
	return func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get(fieldTag), ",")
		if name != "" {
			return name
		}
		typ := field.Type
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if field.Anonymous && typ.Kind() == reflect.Struct {
			return ""
		}
		return field.Name
	}
}

// FieldPath removes root's named type prefix from a configured validator namespace.
func FieldPath(root any, failure validator.FieldError) string {
	typ := reflect.TypeOf(root)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	path := failure.Namespace()
	if name := typ.Name(); name != "" {
		path = strings.TrimPrefix(path, name+".")
	}
	return path
}
