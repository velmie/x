package confload

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/v2"
)

// UnknownKeysError reports configuration keys that are not present in the target struct.
type UnknownKeysError struct {
	Keys    []string
	Origins map[string]ValueOrigin
}

func (e *UnknownKeysError) Error() string {
	if e == nil || len(e.Keys) == 0 {
		return "unknown configuration keys"
	}

	var b strings.Builder
	b.WriteString("unknown configuration keys: ")
	for i, key := range e.Keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(key)
		if origin, ok := e.Origins[key]; ok {
			b.WriteString(" (")
			b.WriteString(string(origin.Source))
			if origin.Identifier != "" {
				b.WriteString(" ")
				b.WriteString(origin.Identifier)
			}
			b.WriteString(")")
		}
	}

	return b.String()
}

type strictState struct {
	err error
}

func validateStrict(k *koanf.Koanf, origins, boundaryOrigins map[string]ValueOrigin, fields []schemaField, prefix, tag string) error {
	unknown := make(map[string]ValueOrigin)
	state := &strictState{}
	strictStruct(reflect.ValueOf(k.Get(prefix)), fields, prefix, prefix, prefix, "", origins, boundaryOrigins, unknown, tag, state)
	if state.err != nil {
		return state.err
	}
	if len(unknown) == 0 {
		return nil
	}
	keys := make([]string, 0, len(unknown))
	for key := range unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return &UnknownKeysError{Keys: keys, Origins: unknown}
}

func strictStruct(
	v reflect.Value,
	fields []schemaField,
	base, displayBase, raw, part string,
	origins, boundaryOrigins, unknown map[string]ValueOrigin,
	tag string,
	state *strictState,
) {
	v = indirectValue(v)
	if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
		return
	}
	iter := v.MapRange()
	for iter.Next() {
		key := iter.Key().String()
		match := key
		if part != "" {
			match = part + "." + key
		}
		full := match
		if base != "" {
			full = base + "." + match
		}
		var found *schemaField
		descendants := false
		for i := range fields {
			f := &fields[i]
			if f.path == full {
				found = f
			}
			if strings.HasPrefix(f.path, full+".") {
				descendants = true
			}
		}
		display := configurationPath(displayBase, key)
		rawKey := key
		if raw != "" {
			rawKey = raw + "." + key
		}
		if found != nil && !found.container {
			if found.element != nil {
				strictCollection(iter.Value(), found.element, display, rawKey, origins, boundaryOrigins, unknown, tag, state)
			}
			continue
		}
		if descendants || (found != nil && found.container) {
			strictStruct(iter.Value(), fields, base, display, rawKey, match, origins, boundaryOrigins, unknown, tag, state)
			continue
		}
		strictUnknown(iter.Value(), display, rawKey, origins, boundaryOrigins, unknown)
	}
}

func strictCollection(
	v reflect.Value,
	node *schemaNode,
	display, raw string,
	origins, boundaryOrigins, unknown map[string]ValueOrigin,
	tag string,
	state *strictState,
) {
	v = indirectValue(v)
	if !v.IsValid() {
		return
	}
	switch node.kind {
	case reflect.Struct:
		if v.Kind() == reflect.Struct {
			if v.Type() == node.typ {
				return
			}
			var converted map[string]any
			decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &converted, TagName: tag})
			if err == nil {
				err = decoder.Decode(v.Interface())
			}
			if err != nil {
				state.err = fmt.Errorf("validate structured configuration at %s: %w", display, err)
				return
			}
			v = reflect.ValueOf(converted)
		}
		strictStruct(v, node.fields, "", display, raw, "", origins, boundaryOrigins, unknown, tag, state)
	case reflect.Map:
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || node.element == nil {
			return
		}
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			strictCollection(iter.Value(), node.element, configurationPath(display, key), raw+"."+key, origins, boundaryOrigins, unknown, tag, state)
		}
	case reflect.Slice, reflect.Array:
		if (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || node.element == nil {
			return
		}
		for i := 0; i < v.Len(); i++ {
			index := strconv.Itoa(i)
			strictCollection(v.Index(i), node.element, display+"."+index, raw+"."+index, origins, boundaryOrigins, unknown, tag, state)
		}
	}
}

func indirectValue(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func configurationPath(base, key string) string {
	if strings.ContainsAny(key, ".[]\" ") || key == "" {
		return base + "[" + strconv.Quote(key) + "]"
	}
	if base == "" {
		return key
	}
	return base + "." + key
}

func strictUnknown(v reflect.Value, display, raw string, origins, boundaryOrigins, unknown map[string]ValueOrigin) {
	v = indirectValue(v)
	if v.IsValid() && v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String && v.Len() > 0 {
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			strictUnknown(iter.Value(), configurationPath(display, key), raw+"."+key, origins, boundaryOrigins, unknown)
		}
		return
	}
	if origin, ok := boundaryOrigins[display]; ok {
		unknown[display] = origin
		return
	}
	for path := raw; path != ""; path = parentPath(path) {
		if origin, ok := origins[path]; ok {
			unknown[display] = origin
			return
		}
	}
	unknown[display] = ValueOrigin{}
}
