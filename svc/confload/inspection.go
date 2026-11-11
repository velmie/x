package confload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"

	"github.com/knadh/koanf/v2"
)

const (
	inspectionRedacted                 = "[REDACTED]"
	inspectionUnknownSource SourceType = "unknown"
)

// InspectOptions supplies presentation choices owned by the application.
type InspectOptions struct {
	// EnvName resolves the declared environment name. Nil uses the unprefixed
	// default; a loader's EnvName method also applies its prefix and aliases.
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
		sensitive := field.Attributes[sensitiveTag] == sensitiveTagEnabled || collectionContainsSensitive(field.Type)
		if sensitive {
			field.Default = inspectionRedacted
		} else if defaults != nil {
			if !defaults.Exists(field.Path) {
				field.Default = nil
			} else if provided.Exists(field.Path) {
				field.Default = defaults.Get(field.Path)
			}
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
				row.Value = value
			}
			origin := inspectionOrigin(snapshot.Origins[field.Path], snapshot.Files, row.Env, opts.OverrideLabel)
			row.Origin = &origin
		}
		rows = append(rows, row)
	}
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
