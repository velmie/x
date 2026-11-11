package pathexpr

import (
	"errors"
	"reflect"
)

// Path is an immutable location obtained through CompilePath.
type Path struct {
	path pathNode
	pos  Position
}

// CompilePath validates a path. Literal-looking components remain keys.
func CompilePath(source string) (Path, error) {
	p, err := newParser(source)
	if err != nil {
		return Path{}, err
	}
	p.space()
	pos := p.position()
	path, err := p.path()
	if err != nil {
		return Path{}, err
	}
	p.space()
	if p.i != len(p.s) {
		return Path{}, p.fail("unexpected trailing input")
	}
	return Path{
		path: path,
		pos:  pos,
	}, nil
}

// Get selects a value without modifying root.
func (p Path) Get(root any) (any, error) { return p.path.eval(root) }

// Set assigns a value, creating missing object branches where allowed. It never
// grows slices or replaces the root. On failure, root remains unchanged.
// Concurrent mutation of root is the caller's responsibility.
func (p Path) Set(root any, value any) error {
	if len(p.path.steps) == 0 {
		return &EvalError{
			Op:   "set",
			Path: p.path.text,
			At:   ".",
			Pos:  p.pos,
			Err:  errors.New("cannot assign the root"),
		}
	}
	current := root
	for i, s := range p.path.steps {
		container := reflect.ValueOf(current)
		at := "."
		if i > 0 {
			at = p.path.steps[i-1].prefix
		}
		var elem reflect.Type
		if s.indexed {
			if !container.IsValid() || container.Kind() != reflect.Slice || container.Type().Elem().Kind() != reflect.Interface {
				return p.setError(s, at, &TypeError{
					Want: "slice with interface elements",
					Got:  reflect.TypeOf(current),
				})
			}
			if s.index >= uint64(container.Len()) {
				return p.setError(s, s.prefix, ErrNotFound)
			}
			elem = container.Type().Elem()
			if i == len(p.path.steps)-1 {
				assigned, err := assignment(value, elem)
				if err != nil {
					return p.setError(s, s.prefix, err)
				}
				container.Index(int(s.index)).Set(assigned)
				return nil
			}
			current = container.Index(int(s.index)).Interface()
			continue
		}
		if !container.IsValid() || container.Kind() != reflect.Map ||
			container.Type().Key().Kind() != reflect.String ||
			container.Type().Elem().Kind() != reflect.Interface || container.IsNil() {
			return p.setError(s, at, &TypeError{
				Want: "non-nil string-keyed map with interface elements",
				Got:  reflect.TypeOf(current),
			})
		}
		elem = container.Type().Elem()
		key := reflect.ValueOf(s.key).Convert(container.Type().Key())
		if i == len(p.path.steps)-1 {
			assigned, err := assignment(value, elem)
			if err != nil {
				return p.setError(s, s.prefix, err)
			}
			container.SetMapIndex(key, assigned)
			return nil
		}
		next := container.MapIndex(key)
		if next.IsValid() {
			current = next.Interface()
			continue
		}
		tail := value
		for j := len(p.path.steps) - 1; j > i; j-- {
			child := p.path.steps[j]
			if child.indexed {
				return p.setError(child, child.prefix, ErrNotFound)
			}
			tail = map[string]any{child.key: tail}
		}
		assigned, err := assignment(tail, elem)
		if err != nil {
			return p.setError(s, s.prefix, err)
		}
		container.SetMapIndex(key, assigned)
		return nil
	}
	return nil
}

func assignment(value any, elem reflect.Type) (reflect.Value, error) {
	if value == nil {
		return reflect.Zero(elem), nil
	}
	v := reflect.ValueOf(value)
	if !v.Type().AssignableTo(elem) {
		return reflect.Value{}, &TypeError{
			Want: elem.String(),
			Got:  v.Type(),
		}
	}
	return v, nil
}

func (p Path) setError(s step, at string, err error) error {
	return &EvalError{
		Op:   "set",
		Path: p.path.text,
		At:   at,
		Pos:  s.pos,
		Err:  err,
	}
}
