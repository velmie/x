package confload

import (
	"fmt"
	"reflect"
	"strings"
)

type schemaField struct {
	field     reflect.StructField
	path      string
	index     []int
	sensitive bool
	container bool
}

type schemaPath struct {
	goName    string
	container bool
}

// walkSchema is the common source of configuration paths and reflection indexes.
func walkSchema(t reflect.Type, prefix, tagName string) ([]schemaField, error) {
	var fields []schemaField
	seen := make(map[string]schemaPath)
	var walk func(reflect.Type, string, string, []int, bool) error
	walk = func(t reflect.Type, path, goPath string, index []int, sensitive bool) error {
		if t == nil {
			return nil
		}
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return nil
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			raw := strings.TrimSpace(f.Tag.Get(tagName))
			if raw == "" || raw == "-" {
				continue
			}
			parts := strings.Split(raw, ",")
			name := strings.TrimSpace(parts[0])
			squash := false
			for _, option := range parts[1:] {
				if option == "squash" {
					squash = true
				}
			}
			goName := f.Name
			if goPath != "" {
				goName = goPath + "." + goName
			}
			if squash && (!f.Anonymous || f.Type.Kind() != reflect.Struct || name != "") {
				return fmt.Errorf("confload: squash on %s requires an embedded struct value with an empty name", goName)
			}
			if !squash && name == "" {
				continue
			}
			key := path
			if !squash {
				if key != "" {
					key += "."
				}
				key += name
			}
			idx := appendIndex(index, i)
			isSensitive := sensitive || f.Tag.Get(sensitiveTag) == sensitiveTagEnabled
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			container := ft.Kind() == reflect.Struct && ft.PkgPath() != netURLPkg
			if !squash {
				if err := checkSchemaCollision(seen, key, goName, container); err != nil {
					return err
				}
				seen[key] = schemaPath{
					goName:    goName,
					container: container,
				}
				fields = append(
					fields,
					schemaField{
						field:     f,
						path:      key,
						index:     idx,
						sensitive: isSensitive,
						container: container,
					},
				)
			}
			if container {
				if err := walk(ft, key, goName, idx, isSensitive); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(t, prefix, "", nil, false); err != nil {
		return nil, err
	}
	return fields, nil
}

func checkSchemaCollision(
	seen map[string]schemaPath,
	key string,
	name string,
	container bool,
) error {
	for previousPath, previous := range seen {
		if previousPath == key || (!previous.container && strings.HasPrefix(key, previousPath+".")) ||
			(!container && strings.HasPrefix(previousPath, key+".")) {
			return fmt.Errorf("confload: configuration path %q conflicts between %s and %s", key, previous.goName, name)
		}
	}
	return nil
}

func schemaDefaults(fields []schemaField) map[string]any {
	defaults := make(map[string]any)
	for _, item := range fields {
		if item.container {
			continue
		}
		value, ok := item.field.Tag.Lookup("default")
		if !ok {
			continue
		}
		t := item.field.Type
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			sep := item.field.Tag.Get("default_sep")
			if sep == "" {
				sep = ","
			}
			parts := strings.Split(value, sep)
			list := make([]any, 0, len(parts))
			for _, part := range parts {
				list = append(list, strings.TrimSpace(part))
			}
			defaults[item.path] = list
		} else {
			defaults[item.path] = value
		}
	}
	return defaults
}
