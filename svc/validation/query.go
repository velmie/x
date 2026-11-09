package validation

import (
	"encoding"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
	"github.com/velmie/x/svc/apperror"
)

const querySingle = "single"

// QueryError describes a query decoding failure.
// Field, Rule, and Param describe declared DTO metadata. Cause is the unchanged
// native parsing or binding error.
type QueryError struct {
	Field  string
	Rule   string
	Param  string
	Cause  error
	detail string
}

// Error describes the failed operation, declared constraint, and any cause.
func (e *QueryError) Error() string {
	detail := e.detail
	if detail == "" {
		detail = "query decoding failed"
		if e.Field != "" && e.Rule != "" {
			detail = fmt.Sprintf("query field %s violates %s", e.Field, e.Rule)
			if e.Param != "" {
				detail += "=" + e.Param
			}
		}
	}
	if e.Cause != nil {
		detail += ": " + e.Cause.Error()
	}
	return detail
}

// Unwrap returns the unchanged parsing or binding error, when present.
func (e *QueryError) Unwrap() error { return e.Cause }

// DecodeQuery decodes a flat URL query into dst and validates it with v.
// dst must be a non-nil pointer to a struct; v must be configured before use.
// Exported scalar fields use json tag names, or their Go names when untagged.
// Untagged exported embedded value structs are flattened. Unexported embedded
// structs, embedded pointer fields, and duplicate Go field names across embeddings,
// including ignored fields, are rejected. Scalars, pointers, scalar slices, and
// encoding.TextUnmarshaler implementations are supported. A slice of pointers
// to named scalars must implement TextUnmarshaler on its element type. Nested
// query paths and unknown names are rejected. Matching is case-insensitive, repeated
// scalars use the last value, and empty ordinary scalar values leave defaults
// unchanged. query:"single" rejects repeated occurrences, including empty
// values and case aliases. A failure may partially populate dst; discard it.
// Invalid destination or DTO metadata returns an ordinary error. Validator
// failures are returned unchanged. QueryError wraps the native parsing or
// binding cause without inspecting or projecting its error tree.
func DecodeQuery(raw string, dst any, v *validator.Validate) error {
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return errors.New("query destination must be a non-nil pointer to a struct")
	}
	fields, err := queryFields(value.Elem().Type(), false, make(map[string]bool), nil)
	if err != nil {
		return err
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
	values := make([][]string, len(fields))
	var unknown []string
	for raw != "" {
		part, remaining, _ := strings.Cut(raw, "&")
		raw = remaining
		if part == "" {
			continue
		}
		parsed, parseErr := url.ParseQuery(part)
		if parseErr != nil {
			return &QueryError{
				Cause:  parseErr,
				detail: "invalid URL query encoding",
			}
		}
		for name, items := range parsed {
			index := -1
			for i := range fields {
				if strings.EqualFold(name, fields[i].name) {
					index = i
					break
				}
			}
			if index == -1 {
				unknown = append(unknown, name)
				continue
			}
			values[index] = append(values[index], items...)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return &QueryError{
			detail: "unknown query parameter " + unknown[0],
		}
	}
	for i, field := range fields {
		if field.single && len(values[i]) > 1 {
			return &QueryError{
				Field: field.name,
				Rule:  apperror.RuleMax,
				Param: "1",
			}
		}
	}
	decoder := schema.NewDecoder()
	decoder.SetAliasTag("json")
	for i, field := range fields {
		if len(values[i]) == 0 || field.ordinaryScalar && values[i][len(values[i])-1] == "" {
			continue
		}
		owner := dst
		if len(field.owner) != 0 {
			owner = value.Elem().FieldByIndex(field.owner).Addr().Interface()
		}
		if bindErr := decoder.Decode(owner, map[string][]string{field.name: values[i]}); bindErr != nil {
			return &QueryError{
				Field: field.name,
				Rule:  apperror.RuleType,
				Param: field.expected,
				Cause: bindErr,
			}
		}
	}
	return v.Struct(dst)
}

type queryField struct {
	name           string
	owner          []int
	expected       string
	single         bool
	ordinaryScalar bool
}

var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

func queryFields(typ reflect.Type, ignored bool, names map[string]bool, owner []int) ([]queryField, error) {
	var fields []queryField
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		base := field.Type
		if base.Kind() == reflect.Pointer {
			base = base.Elem()
		}
		if field.Anonymous && base.Kind() == reflect.Struct && field.PkgPath != "" {
			return nil, invalidQueryField(field.Name, "embedded structs must be exported")
		}
		if field.Anonymous && field.Type.Kind() == reflect.Pointer {
			detail := "embedded pointer fields are not supported"
			if base.Kind() == reflect.Struct {
				detail = "embedded pointer structs are not supported"
			}
			return nil, invalidQueryField(field.Name, detail)
		}
		if names[field.Name] {
			return nil, invalidQueryField(field.Name, "Go field name is ambiguous across embedded structs")
		}
		names[field.Name] = true
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Anonymous && base.Kind() == reflect.Struct {
			index := append(append([]int(nil), owner...), i)
			if queryTextType(field.Type) {
				if _, err := queryFields(base, true, names, index); err != nil {
					return nil, err
				}
			} else if ignored || name == "" || name == "-" {
				if !ignored && name != "-" && field.Tag.Get("query") != "" {
					return nil, invalidQueryField(field.Name, "embedded DTO cannot declare a query tag")
				}
				nested, err := queryFields(base, ignored || name == "-", names, index)
				if err != nil {
					return nil, err
				}
				fields = append(fields, nested...)
				continue
			}
		}
		if ignored || name == "-" || field.PkgPath != "" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if strings.ContainsAny(name, ".[]") {
			return nil, invalidQueryField(field.Name, "query names must be flat")
		}
		queryTag := field.Tag.Get("query")
		if queryTag != "" && queryTag != querySingle {
			return nil, invalidQueryField(field.Name, "invalid query tag")
		}
		expected, ordinary, ok := queryType(field.Type)
		if !ok {
			return nil, invalidQueryField(field.Name, "unsupported query field type")
		}
		fields = append(
			fields,
			queryField{
				name:           name,
				owner:          owner,
				expected:       expected,
				single:         queryTag == querySingle,
				ordinaryScalar: ordinary,
			},
		)
	}
	// A sibling may conflict with a field promoted from a later embedded DTO.
	for i := range fields {
		for j := 0; j < i; j++ {
			if strings.EqualFold(fields[i].name, fields[j].name) {
				return nil, invalidQueryField(fields[i].name, "ambiguous query field name")
			}
		}
	}
	return fields, nil
}

func invalidQueryField(name, detail string) error {
	return fmt.Errorf("invalid query DTO field %s: %s", name, detail)
}

func queryType(typ reflect.Type) (expected string, ordinaryScalar, ok bool) {
	text := queryTextType(typ)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	slice := typ.Kind() == reflect.Slice
	if slice {
		typ = typ.Elem()
		if typ.Kind() == reflect.Pointer {
			if typ.Elem().PkgPath() != "" && !queryTextType(typ) {
				return "", false, false
			}
			typ = typ.Elem()
		}
	}
	switch typ.Kind() {
	case reflect.String:
		expected = "string"
	case reflect.Bool:
		expected = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		expected = "integer"
	case reflect.Float32, reflect.Float64:
		expected = "number"
	default:
		if text && !slice {
			return "text", false, true
		}
		return "", false, false
	}
	return expected, !text && !slice, true
}

func queryTextType(typ reflect.Type) bool {
	return typ.Implements(textUnmarshalerType) ||
		typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(textUnmarshalerType)
}
