package confload

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var errDescribeNonStruct = errors.New("confload: DescribeStruct expects struct type")

// FieldDescriptor describes a single configuration field exposed via config tags.
type FieldDescriptor struct {
	Path       string
	Type       reflect.Type
	Hint       string
	Default    any
	Required   bool
	Validation string
	Attributes map[string]string
	Index      []int
}

// EnvName renders the environment variable name for the descriptor using the provided prefix.
func (d FieldDescriptor) EnvName(prefix string) string {
	return EnvName(prefix, d.Path)
}

// FieldCollection aggregates configuration field descriptors for a struct tree.
type FieldCollection struct {
	fields []FieldDescriptor
	index  map[string]int
}

// Fields returns a copy of the collected descriptors ordered by their dotted paths.
func (c FieldCollection) Fields() []FieldDescriptor {
	out := make([]FieldDescriptor, len(c.fields))
	copy(out, c.fields)

	return out
}

// Len exposes the number of collected descriptors.
func (c FieldCollection) Len() int { return len(c.fields) }

// Lookup returns a descriptor by its dotted path.
func (c FieldCollection) Lookup(path string) (FieldDescriptor, bool) {
	idx, ok := c.index[path]
	if !ok {
		return FieldDescriptor{}, false
	}

	return c.fields[idx], true
}

// EnvMap builds a map of dotted paths to their environment variable names using the provided prefix.
func (c FieldCollection) EnvMap(prefix string) map[string]string {
	if len(c.fields) == 0 {
		return nil
	}

	out := make(map[string]string, len(c.fields))
	for i := range c.fields {
		out[c.fields[i].Path] = c.fields[i].EnvName(prefix)
	}

	return out
}

// DescribeStruct enumerates the configuration fields declared on the provided struct type using the default tag name.
func DescribeStruct[T any]() (FieldCollection, error) {
	return DescribeStructWithTag[T](defaultTagName)
}

// DescribeStructWithTag enumerates the configuration fields declared on the provided struct type using the tag name.
func DescribeStructWithTag[T any](tagName string) (FieldCollection, error) {
	var target T
	typeOf := reflect.TypeOf(target)
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return FieldCollection{}, fmt.Errorf("%w: %s", errDescribeNonStruct, typeOf.Kind())
	}

	tagName = normalizeTagName(tagName)

	fields := make([]FieldDescriptor, 0)
	collectFieldDescriptors(typeOf, "", nil, tagName, &fields)

	defaults := collectTagDefaults(typeOf, "", tagName)
	for i := range fields {
		if def, ok := defaults[fields[i].Path]; ok {
			fields[i].Default = normalizeDefault(def)
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})

	index := make(map[string]int, len(fields))
	for i := range fields {
		index[fields[i].Path] = i
	}

	return FieldCollection{fields: fields, index: index}, nil
}

func collectFieldDescriptors(t reflect.Type, prefix string, index []int, tagName string, acc *[]FieldDescriptor) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		currentIndex := appendIndex(index, i)
		tags := parseStructTags(f.Tag)
		tag := strings.TrimSpace(tags[tagName])
		if tag == "" || tag == "-" {
			continue
		}

		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}

		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		if ft.Kind() == reflect.Struct && ft.PkgPath() != netURLPkg {
			collectFieldDescriptors(ft, key, currentIndex, tagName, acc)

			continue
		}

		validation := strings.TrimSpace(tags["validate"])
		required := hasValidateRequired(validation)

		descriptor := FieldDescriptor{
			Path:       key,
			Type:       f.Type,
			Hint:       strings.TrimSpace(tags["hint"]),
			Required:   required,
			Validation: validation,
			Attributes: filterKnownTags(tags, tagName),
			Index:      currentIndex,
		}

		*acc = append(*acc, descriptor)
	}
}

func hasValidateRequired(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return false
	}

	parts := strings.Split(tag, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "required") {
			return true
		}
	}

	return false
}

func normalizeDefault(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}

		return out
	case map[string]any:
		// deterministic order
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		ordered := make(map[string]any, len(v))
		for _, key := range keys {
			ordered[key] = v[key]
		}

		return ordered
	default:
		return value
	}
}

func appendIndex(base []int, value int) []int {
	if base == nil {
		return []int{value}
	}

	out := make([]int, len(base)+1)
	copy(out, base)
	out[len(base)] = value

	return out
}

func parseStructTags(tag reflect.StructTag) map[string]string {
	result := make(map[string]string)
	raw := string(tag)
	for raw != "" {
		raw = strings.TrimLeft(raw, " `")
		if raw == "" {
			break
		}

		key, rest, found := strings.Cut(raw, ":")
		if !found {
			break
		}
		key = strings.TrimSpace(key)
		if key == "" {
			raw = rest

			continue
		}

		rest = strings.TrimLeft(rest, " ")
		if rest == "" || rest[0] != '"' {
			raw = rest

			continue
		}

		rest = rest[1:]
		idx := strings.IndexByte(rest, '"')
		if idx == -1 {
			break
		}

		value := rest[:idx]
		result[key] = value
		raw = rest[idx+1:]
	}

	return result
}

var knownTagKeys = map[string]struct{}{
	"default":     {},
	"default_sep": {},
	"validate":    {},
	"hint":        {},
	"json":        {},
	"yaml":        {},
}

func filterKnownTags(tags map[string]string, tagName string) map[string]string {
	if len(tags) == 0 {
		return nil
	}

	attrs := make(map[string]string)
	for key, value := range tags {
		if key == tagName {
			continue
		}
		if _, ok := knownTagKeys[key]; ok {
			continue
		}
		attrs[key] = value
	}

	if len(attrs) == 0 {
		return nil
	}

	return attrs
}

// EnvName converts a dotted configuration path into an environment variable name using the provided prefix.
func EnvName(prefix, dotted string) string {
	return envName(prefix, dotted)
}
