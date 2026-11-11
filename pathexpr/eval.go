package pathexpr

import (
	"fmt"
	"reflect"
)

func (n literalNode) eval(_ any) (any, error) { return n.value, nil }
func (n pathNode) eval(root any) (any, error) {
	current := root
	for _, s := range n.steps {
		v := reflect.ValueOf(current)
		if s.indexed {
			if !v.IsValid() || v.Kind() != reflect.Slice {
				return nil, &EvalError{
					Op:   "get",
					Path: n.text,
					At:   s.prefix,
					Pos:  s.pos,
					Err: &TypeError{
						Want: "slice",
						Got:  reflect.TypeOf(current),
					},
				}
			}
			if s.index >= uint64(v.Len()) {
				return nil, &EvalError{
					Op:   "get",
					Path: n.text,
					At:   s.prefix,
					Pos:  s.pos,
					Err:  ErrNotFound,
				}
			}
			current = v.Index(int(s.index)).Interface()
			continue
		}
		if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return nil, &EvalError{
				Op:   "get",
				Path: n.text,
				At:   s.prefix,
				Pos:  s.pos,
				Err: &TypeError{
					Want: "string-keyed map",
					Got:  reflect.TypeOf(current),
				},
			}
		}
		key := reflect.ValueOf(s.key).Convert(v.Type().Key())
		found := v.MapIndex(key)
		if !found.IsValid() {
			return nil, &EvalError{
				Op:   "get",
				Path: n.text,
				At:   s.prefix,
				Pos:  s.pos,
				Err:  ErrNotFound,
			}
		}
		current = found.Interface()
	}
	return current, nil
}
func (n objectNode) eval(root any) (any, error) {
	result := make(map[string]any, len(n.fields))
	for _, f := range n.fields {
		value, err := f.value.eval(root)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.key, err)
		}
		result[f.key] = value
	}
	return result, nil
}
