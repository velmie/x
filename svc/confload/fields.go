package confload

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

const (
	omitEmptyOption = "omitempty"
	omitZeroOption  = "omitzero"
)

type schemaField struct {
	field     reflect.StructField
	path      string
	index     []int
	sensitive bool
	container bool
	element   *schemaNode
}

type schemaPath struct {
	goName    string
	container bool
}

// walkSchema is the common source of configuration paths and reflection indexes.
func walkSchema(t reflect.Type, prefix, tagName string) ([]schemaField, error) {
	fields, err := walkSchemaFields(t, prefix, tagName)
	if err != nil {
		return nil, err
	}
	for i := range fields {
		if fields[i].container {
			continue
		}
		fields[i].element, err = collectionSchema(fields[i].field.Type, tagName, fields[i].sensitive, make(map[reflect.Type]bool))
		if err != nil {
			return nil, fmt.Errorf("configuration field %s: %w", fields[i].path, err)
		}
	}
	return fields, nil
}

func walkSchemaFields(t reflect.Type, prefix, tagName string) ([]schemaField, error) {
	var fields []schemaField
	seen := make(map[string]schemaPath)
	active := make(map[reflect.Type]bool)
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
		if active[t] {
			return fmt.Errorf("recursive configuration field %s has type %s", path, t)
		}
		active[t] = true
		defer delete(active, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
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
			if squash && (!f.IsExported() || !f.Anonymous || f.Type.Kind() != reflect.Struct || name != "") {
				return fmt.Errorf("confload: squash on %s requires an exported embedded struct value with an empty name", goName)
			}
			if !f.IsExported() {
				continue
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
	for i := range fields {
		item := &fields[i]
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

// schemaNode represents a collection or the fields of a structured element.
type schemaNode struct {
	kind    reflect.Kind
	typ     reflect.Type
	tag     string
	fields  []schemaField
	element *schemaNode
}

func collectionSchema(t reflect.Type, tag string, sensitive bool, active map[reflect.Type]bool) (*schemaNode, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if active[t] {
		return nil, fmt.Errorf("recursive configuration type %s", t)
	}
	active[t] = true
	defer delete(active, t)
	node := &schemaNode{kind: t.Kind(), typ: t, tag: tag}
	switch t.Kind() {
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, nil
		}
		fallthrough
	case reflect.Slice, reflect.Array:
		child, err := collectionSchema(t.Elem(), tag, sensitive, active)
		if err != nil {
			return nil, err
		}
		node.element = child
	case reflect.Struct:
		if t.PkgPath() == netURLPkg {
			return nil, nil
		}
		fields, err := walkSchemaFields(t, "", tag)
		if err != nil {
			return nil, err
		}
		for i := range fields {
			fields[i].sensitive = fields[i].sensitive || sensitive
			if !fields[i].container {
				child, err := collectionSchema(fields[i].field.Type, tag, fields[i].sensitive, active)
				if err != nil {
					return nil, fmt.Errorf("configuration field %s: %w", fields[i].path, err)
				}
				fields[i].element = child
			}
		}
		node.fields = fields
	default:
		return nil, nil
	}
	return node, nil
}

func applyCollectionDefaults(
	k *koanf.Koanf,
	fields []schemaField,
	origins map[string]ValueOrigin,
	boundaryOrigins map[string]ValueOrigin,
) (*koanf.Koanf, error) {
	hasCollections := false
	for i := range fields {
		if fields[i].element != nil {
			hasCollections = true
			break
		}
	}
	if !hasCollections {
		return k, nil
	}
	raw := normalizeObject(reflect.ValueOf(k.Raw()))
	changed := false
	for i := range fields {
		field := &fields[i]
		if field.element == nil {
			continue
		}
		value, updated, err := applyNodeDefaults(
			schemaValue(reflect.ValueOf(raw), field.path),
			field.element,
			field.path,
			origins,
			boundaryOrigins,
		)
		if err != nil {
			return nil, err
		}
		if updated {
			setSchemaValue(raw, field.path, value)
			changed = true
		}
	}
	if !changed {
		return k, nil
	}
	updated := koanf.New(".")
	if err := updated.Load(confmap.Provider(raw, ""), nil); err != nil {
		return nil, fmt.Errorf("apply element defaults: %w", err)
	}
	return updated, nil
}

func applyNodeDefaults(
	v reflect.Value,
	node *schemaNode,
	path string,
	origins, boundaryOrigins map[string]ValueOrigin,
) (result any, updated bool, resultErr error) {
	v = indirectValue(v)
	if !v.IsValid() {
		return nil, false, nil
	}
	changed := false
	switch node.kind {
	case reflect.Map:
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || v.IsNil() || node.element == nil {
			return nil, false, nil
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			current := iter.Value()
			value, updated, err := applyNodeDefaults(current, node.element, configurationPath(path, key), origins, boundaryOrigins)
			if err != nil {
				return nil, false, err
			}
			if updated {
				out[key] = value
				changed = true
			} else {
				out[key] = current.Interface()
			}
		}
		return out, changed, nil
	case reflect.Slice, reflect.Array:
		if (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || node.element == nil {
			return nil, false, nil
		}
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			value, updated, err := applyNodeDefaults(v.Index(i), node.element, path+"."+strconv.Itoa(i), origins, boundaryOrigins)
			if err != nil {
				return nil, false, err
			}
			if updated {
				out[i] = value
				changed = true
			} else {
				out[i] = v.Index(i).Interface()
			}
		}
		return out, changed, nil
	case reflect.Struct:
		if v.Kind() == reflect.Struct {
			if v.Type() == node.typ {
				return nil, false, nil
			}
			converted, err := configurationStructMap(v, node.tag, node.fields)
			if err != nil {
				return nil, false, fmt.Errorf("apply element defaults at %s: %w", path, err)
			}
			v = reflect.ValueOf(converted)
			changed = true
		}
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || v.IsNil() {
			return nil, false, nil
		}
		object := normalizeObject(v)
		objectValue := reflect.ValueOf(object)
		for key, value := range schemaDefaults(node.fields) {
			if !insertDefault(objectValue, strings.Split(key, "."), value) {
				continue
			}
			full := path + "." + key
			origin := ValueOrigin{Source: SourceStructDefault}
			origins[full] = origin
			boundaryOrigins[full] = origin
			changed = true
		}
		for i := range node.fields {
			field := &node.fields[i]
			if field.element == nil {
				continue
			}
			value, updated, err := applyNodeDefaults(
				schemaValue(objectValue, field.path),
				field.element,
				path+"."+field.path,
				origins,
				boundaryOrigins,
			)
			if err != nil {
				return nil, false, err
			}
			if updated {
				setSchemaValue(object, field.path, value)
				changed = true
			}
		}
		return object, changed, nil
	}
	return nil, false, nil
}

func normalizeObject(v reflect.Value) map[string]any {
	out := make(map[string]any, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		item := iter.Value()
		value := indirectValue(item)
		if value.IsValid() && value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String && !value.IsNil() {
			out[iter.Key().String()] = normalizeObject(value)
		} else {
			out[iter.Key().String()] = item.Interface()
		}
	}
	return out
}

func setSchemaValue(object map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := object[part].(map[string]any)
		if !ok {
			return
		}
		object = next
	}
	object[parts[len(parts)-1]] = value
}

func configurationStructMap(v reflect.Value, tag string, target []schemaField) (map[string]any, error) {
	var result map[string]any
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &result, TagName: tag})
	if err != nil {
		return nil, err
	}
	if err := decoder.Decode(v.Interface()); err != nil {
		return nil, err
	}
	fields, err := walkSchemaFields(v.Type(), "", tag)
	if err != nil {
		return nil, err
	}
	for i := range fields {
		field := &fields[i]
		if field.container {
			continue
		}
		options := strings.Split(field.field.Tag.Get(tag), ",")
		omitted := false
		for _, option := range options[1:] {
			if option == omitEmptyOption || option == omitZeroOption {
				omitted = true
				break
			}
		}
		if !omitted {
			continue
		}
		value := schemaFieldValue(v, field)
		if value.IsValid() && value.CanInterface() {
			insertDefault(reflect.ValueOf(result), strings.Split(field.path, "."), value.Interface())
		}
	}

	// An empty tag name still lets mapstructure use the Go field name.
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		options := strings.Split(field.Tag.Get(tag), ",")
		if len(options) < 2 || options[0] != "" {
			continue
		}
		for _, option := range options[1:] {
			if option == omitEmptyOption || option == omitZeroOption {
				value := v.Field(i)
				if value.CanInterface() {
					insertDefault(reflect.ValueOf(result), []string{field.Name}, value.Interface())
				}
				break
			}
		}
	}
	result = normalizeObject(reflect.ValueOf(result))
	for i := range target {
		canonicalizeStructKeys(result, strings.Split(target[i].path, "."))
	}
	return result, nil
}

func canonicalizeStructKeys(object map[string]any, parts []string) {
	value, ok := object[parts[0]]
	if !ok {
		names := make([]string, 0, len(object))
		for name := range object {
			if strings.EqualFold(name, parts[0]) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			return
		}
		value = object[names[0]]
		delete(object, names[0])
		object[parts[0]] = value
	}
	if len(parts) > 1 {
		if child, ok := value.(map[string]any); ok {
			canonicalizeStructKeys(child, parts[1:])
		}
	}
}

func schemaValue(v reflect.Value, path string) reflect.Value {
	for _, part := range strings.Split(path, ".") {
		v = indirectValue(v)
		if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return reflect.Value{}
		}
		v = v.MapIndex(reflect.ValueOf(part).Convert(v.Type().Key()))
	}
	return v
}

//nolint:gosec // Callers split nonempty schema paths; recursion stops at the last component.
func insertDefault(v reflect.Value, parts []string, value any) bool {
	v = indirectValue(v)
	if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || v.IsNil() {
		return false
	}
	key := reflect.ValueOf(parts[0]).Convert(v.Type().Key())
	current := v.MapIndex(key)
	if !current.IsValid() {
		iter := v.MapRange()
		for iter.Next() {
			if strings.EqualFold(iter.Key().String(), parts[0]) {
				key = iter.Key()
				current = iter.Value()
				break
			}
		}
	}
	if len(parts) == 1 {
		if current.IsValid() {
			return false
		}
		def := reflect.ValueOf(value)
		if !def.IsValid() || !def.Type().AssignableTo(v.Type().Elem()) {
			return false
		}
		v.SetMapIndex(key, def)
		return true
	}
	if !current.IsValid() {
		child := reflect.ValueOf(map[string]any{})
		if !child.Type().AssignableTo(v.Type().Elem()) {
			return false
		}
		v.SetMapIndex(key, child)
		current = child
	}
	return insertDefault(current, parts[1:], value)
}

func schemaFieldValue(v reflect.Value, field *schemaField) reflect.Value {
	if v.Kind() == reflect.Struct {
		for _, i := range field.index {
			for v.IsValid() && v.Kind() == reflect.Pointer {
				if v.IsNil() {
					return reflect.Value{}
				}
				v = v.Elem()
			}
			if !v.IsValid() || v.Kind() != reflect.Struct {
				return reflect.Value{}
			}
			v = v.Field(i)
		}
		return v
	}
	for _, part := range strings.Split(field.path, ".") {
		for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return reflect.Value{}
		}
		v = v.MapIndex(reflect.ValueOf(part).Convert(v.Type().Key()))
	}
	return v
}
