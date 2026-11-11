package confload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

const (
	inspectionRedacted                 = "[REDACTED]"
	inspectionUnknownSource SourceType = "unknown"
)

// InspectOptions supplies presentation choices owned by the application.
type InspectOptions struct {
	// EnvName resolves the declared environment name. Nil uses the unprefixed
	// default; a loader's EnvName method also applies its prefix, section
	// prefixes and aliases.
	EnvName func(path string) string
	// OverrideLabel names an override source. Empty selects "override".
	OverrideLabel string
}

// InspectionField contains metadata and an optional decoded value for display.
// An Origin is present when decoded values were requested, even when no source
// is known. Sensitive defaults and values are masked before they are returned.
type InspectionField struct {
	Field  FieldDescriptor
	Env    string
	Value  any
	Origin *ValueOrigin
}

// Inspect describes fields and optionally reads their decoded values.
// decoded == nil requests schema-only inspection. Otherwise decoded must have
// the same struct type used to create fields, as a value or non-nil pointer.
// Nil nested pointers produce nil field values. Snapshot.Values is never read.
// Sensitive fields are masked before value formatting or marshaling.
func Inspect(
	fields FieldCollection,
	decoded any,
	snapshot Snapshot,
	opts InspectOptions,
) ([]InspectionField, error) {
	return inspect(fields, decoded, snapshot, opts, nil, nil)
}

// Inspect describes fields using the loader's configured defaults and environment names.
// It does not read configuration sources or invoke decode hooks.
func (l *Loader) Inspect(
	fields FieldCollection,
	decoded any,
	snapshot Snapshot,
	opts InspectOptions,
) ([]InspectionField, error) {
	k := koanf.New(".")
	schema, err := walkSchema(fields.rootType, "", normalizeTagName(l.opts.TagName))
	if err != nil {
		return nil, err
	}
	tagDefaults := schemaDefaults(schema)
	if err := l.loadStructTagDefaults(k, tagDefaults, nil); err != nil {
		return nil, err
	}
	if err := l.loadDefaults(k, nil); err != nil {
		return nil, err
	}
	provided := koanf.New(".")
	if err := l.loadDefaults(provided, nil); err != nil {
		return nil, err
	}
	if opts.EnvName == nil {
		opts.EnvName = l.EnvName
	}
	return inspect(fields, decoded, snapshot, opts, k, provided)
}

func inspect(
	fields FieldCollection,
	decoded any,
	snapshot Snapshot,
	opts InspectOptions,
	defaults *koanf.Koanf,
	provided *koanf.Koanf,
) ([]InspectionField, error) {
	var root reflect.Value
	if decoded != nil {
		root = reflect.ValueOf(decoded)
		for root.Kind() == reflect.Pointer {
			if root.IsNil() {
				return nil, errors.New("decoded configuration must be a non-nil struct or pointer")
			}
			root = root.Elem()
		}
		if root.Kind() != reflect.Struct || root.Type() != fields.rootType {
			return nil, errors.New("decoded configuration does not match the described struct")
		}
	}
	envName := opts.EnvName
	if envName == nil {
		envName = func(path string) string { return EnvName("", path) }
	}
	rows := make([]InspectionField, 0, fields.Len())
	for _, field := range fields.Fields() {
		var node *schemaNode
		for i := range fields.schema.fields {
			item := &fields.schema.fields[i]
			if item.path == field.Path {
				node = item.element
				break
			}
		}
		sensitive := field.Attributes[sensitiveTag] == sensitiveTagEnabled ||
			(node == nil && collectionContainsSensitive(field.Type))
		if sensitive {
			field.Default = inspectionRedacted
		} else if defaults != nil {
			if !defaults.Exists(field.Path) {
				field.Default = nil
			} else if provided.Exists(field.Path) {
				field.Default = defaults.Get(field.Path)
			}
		}
		if node != nil && field.Default != nil && !sensitive {
			normalized, err := normalizeInspectionCollection(node, reflect.ValueOf(field.Default))
			if err != nil {
				return nil, fmt.Errorf("inspect default for %s: %w", field.Path, err)
			}
			field.Default = projectCollection(node, reflect.ValueOf(normalized))
		}
		row := InspectionField{
			Field: field,
			Env:   envName(field.Path),
		}
		if decoded != nil {
			if sensitive {
				row.Value = inspectionRedacted
			} else {
				value, err := inspectionValue(root, &field)
				if err != nil {
					return nil, err
				}
				if node != nil {
					value = projectCollection(node, reflect.ValueOf(value))
				}
				row.Value = value
			}
			originValue := snapshot.Origins[field.Path]
			if node != nil {
				originValue = collectionOrigin(field.Path, originValue, snapshot.Origins)
			}
			origin := inspectionOrigin(originValue, snapshot.Files, row.Env, opts.OverrideLabel)
			row.Origin = &origin
		}
		rows = append(rows, row)
		if node != nil && (!sensitive || decoded == nil) {
			var v reflect.Value
			if decoded != nil {
				value, _ := inspectionValue(root, &field)
				v = reflect.ValueOf(value)
			}
			rows = appendElementRows(rows, node, field.Path, v, decoded != nil, snapshot, opts, envName, fields.schema.tagName)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Field.Path < rows[j].Field.Path })
	return rows, nil
}

// WriteInspection renders inspection fields as text or JSON. Empty format
// selects text. The order is the order of fields. Writer errors are returned
// unchanged. Fields must be rows returned by Inspect. Values must be
// serializable by encoding/json.
func WriteInspection(w io.Writer, format string, fields []InspectionField) error {
	switch format {
	case "", "text":
		for i := range fields {
			field := &fields[i]
			defaultValue, err := json.Marshal(field.Field.Default)
			if err != nil {
				return fmt.Errorf("format inspection default: %w", err)
			}
			if _, err := fmt.Fprintf(
				w,
				"%s type=%s env=%s default=%s required=%t sensitive=%t validation=%q hint=%q",
				field.Field.Path,
				field.Field.Type,
				field.Env,
				defaultValue,
				field.Field.Required,
				field.Field.Attributes[sensitiveTag] == sensitiveTagEnabled,
				field.Field.Validation,
				field.Field.Hint,
			); err != nil {
				return err
			}
			if field.Origin != nil {
				value, err := json.Marshal(field.Value)
				if err != nil {
					return fmt.Errorf("format inspection value: %w", err)
				}
				if _, err := fmt.Fprintf(
					w,
					" value=%s source=%s identifier=%s",
					value,
					field.Origin.Source,
					field.Origin.Identifier,
				); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		return nil
	case "json": //nolint:goconst // Output formats and struct tags are independent vocabularies.
		rows := make([]inspectionJSONField, 0, len(fields))
		for i := range fields {
			field := fields[i]
			row := inspectionJSONField{
				Name:       field.Field.Path,
				Type:       field.Field.Type.String(),
				Env:        field.Env,
				Default:    field.Field.Default,
				Required:   field.Field.Required,
				Hint:       field.Field.Hint,
				Validation: field.Field.Validation,
				Sensitive:  field.Field.Attributes[sensitiveTag] == sensitiveTagEnabled,
			}
			if field.Origin != nil {
				row.Value = &field.Value
				row.Origin = &inspectionJSONOrigin{
					Source:     field.Origin.Source,
					Identifier: field.Origin.Identifier,
				}
			}
			rows = append(rows, row)
		}
		return json.NewEncoder(w).Encode(rows)
	default:
		return errors.New("inspection format must be text or json")
	}
}

type inspectionJSONField struct {
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	Env        string                `json:"env"`
	Default    any                   `json:"default"`
	Required   bool                  `json:"required"`
	Hint       string                `json:"hint"`
	Validation string                `json:"validation"`
	Sensitive  bool                  `json:"sensitive"`
	Value      *any                  `json:"value,omitempty"`
	Origin     *inspectionJSONOrigin `json:"origin,omitempty"`
}

type inspectionJSONOrigin struct {
	Source     SourceType `json:"source"`
	Identifier string     `json:"identifier"`
}

func collectionContainsSensitive(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Map && t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return false
	}
	return typeContainsSensitive(t.Elem(), make(map[reflect.Type]bool))
}

func typeContainsSensitive(t reflect.Type, visited map[reflect.Type]bool) bool {
	if visited[t] {
		return false
	}
	visited[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return typeContainsSensitive(t.Elem(), visited)
	case reflect.Map:
		return typeContainsSensitive(t.Key(), visited) || typeContainsSensitive(t.Elem(), visited)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.Tag.Get(sensitiveTag) == sensitiveTagEnabled || typeContainsSensitive(field.Type, visited) {
				return true
			}
		}
	}
	return false
}

func inspectionValue(root reflect.Value, field *FieldDescriptor) (any, error) {
	value := root
	for _, index := range field.Index {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return nil, nil
			}
			value = value.Elem()
		}
		value = value.Field(index)
	}
	if !value.CanInterface() {
		return nil, fmt.Errorf("configuration field %s is not accessible", field.Path)
	}
	return value.Interface(), nil
}

func inspectionOrigin(origin ValueOrigin, files []string, env, label string) ValueOrigin {
	switch origin.Source {
	case SourceStructDefault, SourceDefaults:
		return ValueOrigin{
			Source:     origin.Source,
			Identifier: string(origin.Source),
		}
	case SourceEnv:
		return ValueOrigin{
			Source:     SourceEnv,
			Identifier: env,
		}
	case SourceOverride:
		if label == "" {
			label = string(SourceOverride)
		}
		return ValueOrigin{
			Source:     SourceOverride,
			Identifier: label,
		}
	case SourceFile:
		for i := len(files) - 1; i >= 0; i-- {
			if files[i] == origin.Identifier {
				return ValueOrigin{
					Source:     SourceFile,
					Identifier: "file[" + strconv.Itoa(i+1) + "]",
				}
			}
		}
		return ValueOrigin{
			Source:     SourceFile,
			Identifier: string(SourceFile),
		}
	case SourceReader:
		return ValueOrigin{
			Source:     SourceReader,
			Identifier: "reader",
		}
	case SourceFlag:
		return ValueOrigin{
			Source:     SourceFlag,
			Identifier: "flag",
		}
	default:
		return ValueOrigin{
			Source:     inspectionUnknownSource,
			Identifier: string(inspectionUnknownSource),
		}
	}
}

func normalizeInspectionCollection(node *schemaNode, v reflect.Value) (any, error) {
	v = indirectValue(v)
	if !v.IsValid() {
		return nil, nil
	}
	switch node.kind {
	case reflect.Map:
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || node.element == nil {
			return v.Interface(), nil
		}
		out := make(map[string]any, v.Len())
		it := v.MapRange()
		for it.Next() {
			value, err := normalizeInspectionCollection(node.element, it.Value())
			if err != nil {
				return nil, err
			}
			out[it.Key().String()] = value
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || node.element == nil {
			return v.Interface(), nil
		}
		out := make([]any, v.Len())
		for i := range out {
			value, err := normalizeInspectionCollection(node.element, v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case reflect.Struct:
		if v.Kind() == reflect.Struct && v.Type() != node.typ {
			object, err := configurationStructMap(v, node.tag, node.fields)
			if err != nil {
				return nil, err
			}
			v = reflect.ValueOf(object)
		}
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return v.Interface(), nil
		}
		object := normalizeObject(v)
		for i := range node.fields {
			field := &node.fields[i]
			if field.element == nil {
				continue
			}
			value := schemaValue(reflect.ValueOf(object), field.path)
			if !value.IsValid() {
				continue
			}
			normalized, err := normalizeInspectionCollection(field.element, value)
			if err != nil {
				return nil, err
			}
			setSchemaValue(object, field.path, normalized)
		}
		return object, nil
	}
	return v.Interface(), nil
}

func projectCollection(node *schemaNode, v reflect.Value) any {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	switch node.kind {
	case reflect.Map:
		if !inspectionStringMap(v) {
			return inspectionOpaqueValue(node.typ, v)
		}
		if v.IsNil() {
			return nil
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if node.element == nil {
				out[key] = inspectionOpaqueValue(node.typ.Elem(), iter.Value())
			} else {
				out[key] = projectCollection(node.element, iter.Value())
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return inspectionOpaqueValue(node.typ, v)
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			if node.element == nil {
				out[i] = inspectionOpaqueValue(node.typ.Elem(), v.Index(i))
			} else {
				out[i] = projectCollection(node.element, v.Index(i))
			}
		}
		return out
	case reflect.Struct:
		out := make(map[string]any)
		skipped := make([]string, 0)
		for i := range node.fields {
			field := &node.fields[i]
			skip := false
			for _, parent := range skipped {
				if strings.HasPrefix(field.path, parent+".") {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			if field.container {
				fv := schemaFieldValue(v, field)
				if field.sensitive {
					setProjected(out, field.path, inspectionRedacted)
					skipped = append(skipped, field.path)
					continue
				}
				if !fv.IsValid() || ((fv.Kind() == reflect.Pointer || fv.Kind() == reflect.Interface) && fv.IsNil()) {
					setProjected(out, field.path, nil)
					skipped = append(skipped, field.path)
				}
				continue
			}
			var value any
			if field.sensitive || (field.element == nil && collectionContainsSensitive(field.field.Type)) {
				value = inspectionRedacted
			} else {
				fv := schemaFieldValue(v, field)
				if fv.IsValid() {
					if field.element != nil {
						value = projectCollection(field.element, fv)
					} else if fv.CanInterface() {
						value = fv.Interface()
					}
				}
			}
			setProjected(out, field.path, value)
		}
		return out
	}
	return v.Interface()
}

func inspectionStringMap(value reflect.Value) bool {
	return value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String
}

func inspectionOpaqueValue(t reflect.Type, value reflect.Value) any {
	if collectionContainsSensitive(t) {
		return inspectionRedacted
	}
	return value.Interface()
}

func setProjected(out map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := out[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			out[part] = next
		}
		out = next
	}
	out[parts[len(parts)-1]] = value
}

func appendElementRows(
	rows []InspectionField,
	node *schemaNode,
	path string,
	v reflect.Value,
	values bool,
	snapshot Snapshot,
	opts InspectOptions,
	env func(string) string,
	tag string,
) []InspectionField {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			v = reflect.Value{}
			break
		}
		v = v.Elem()
	}
	if node.kind == reflect.Map || node.kind == reflect.Slice || node.kind == reflect.Array {
		if node.element == nil {
			return rows
		}
		if !values {
			suffix := "[]"
			if node.kind == reflect.Map {
				suffix = ".<name>"
			}
			return appendElementRows(rows, node.element, path+suffix, reflect.Value{}, false, snapshot, opts, env, tag)
		}
		if !v.IsValid() {
			return rows
		}
		if node.kind == reflect.Map && v.Kind() == reflect.Map {
			keys := v.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, key := range keys {
				rows = appendElementRows(rows, node.element, path+"."+key.String(), v.MapIndex(key), true, snapshot, opts, env, tag)
			}
		}
		if (node.kind == reflect.Slice || node.kind == reflect.Array) && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
			for i := 0; i < v.Len(); i++ {
				rows = appendElementRows(rows, node.element, path+"."+strconv.Itoa(i), v.Index(i), true, snapshot, opts, env, tag)
			}
		}
		return rows
	}
	if node.kind != reflect.Struct {
		return rows
	}
	desc := describeSchema(nil, tag, node.fields)
	for _, field := range desc.fields {
		var item schemaField
		for i := range node.fields {
			candidate := &node.fields[i]
			if candidate.path == field.Path {
				item = *candidate
				break
			}
		}
		localPath := field.Path
		field.Path = path + "." + localPath
		sensitive := item.sensitive || (item.element == nil && collectionContainsSensitive(item.field.Type))
		if sensitive {
			field.Default = inspectionRedacted
		}
		row := InspectionField{Field: field, Env: env(field.Path)}
		if values {
			if sensitive {
				row.Value = inspectionRedacted
			} else {
				fv := schemaFieldValue(v, &item)
				if fv.IsValid() {
					if item.element != nil {
						row.Value = projectCollection(item.element, fv)
					} else if fv.CanInterface() {
						row.Value = fv.Interface()
					}
				}
			}
			origin := inspectionOrigin(snapshot.Origins[field.Path], snapshot.Files, row.Env, opts.OverrideLabel)
			row.Origin = &origin
		}
		rows = append(rows, row)
		if item.element != nil && (!item.sensitive || !values) {
			rows = appendElementRows(rows, item.element, field.Path, schemaFieldValue(v, &item), values, snapshot, opts, env, tag)
		}
	}
	return rows
}

func collectionOrigin(path string, origin ValueOrigin, origins map[string]ValueOrigin) ValueOrigin {
	for key, current := range origins {
		if strings.HasPrefix(key, path+".") && current != origin {
			return ValueOrigin{}
		}
	}
	return origin
}
