package condexpr

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/velmie/x/pathexpr"
)

const (
	nullKind        = "null"
	booleanKind     = "boolean"
	numberKind      = "number"
	stringKind      = "string"
	arrayKind       = "array"
	objectKind      = "object"
	unsupportedKind = "unsupported Go type"
)

func (n *node) eval(root any) (bool, error) {
	switch n.op {
	case "&&", "||":
		for _, child := range n.args {
			result, err := child.eval(root)
			if err != nil {
				return false, err
			}
			if n.op == "&&" && !result || n.op == "||" && result {
				return result, nil
			}
		}
		return n.op == "&&", nil
	case "!":
		result, err := n.args[0].eval(root)
		if err != nil {
			return false, err
		}
		return !result, nil
	}
	a, err := n.args[0].operand(root)
	if err != nil {
		return false, err
	}
	b, err := n.args[1].operand(root)
	if err != nil {
		return false, err
	}
	var result bool
	if isBoolCall(n.op) {
		result, err = predicate(n.op, a, b)
	} else {
		result, err = compare(n.op, a, b)
	}
	if err != nil {
		return false, n.failure(err)
	}
	return result, nil
}

func (n *node) operand(root any) (any, error) {
	value := n.value
	switch n.op {
	case "literal":
		return value, nil
	case "path":
		var err error
		value, err = n.path.Get(root)
		if n.optional && errors.Is(err, pathexpr.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			var detail *pathexpr.EvalError
			if errors.As(err, &detail) {
				adjusted := *detail
				adjusted.Pos = relativePosition(n.pos, detail.Pos)
				failure := n.failure(&adjusted)
				failure.Pos = adjusted.Pos
				return nil, failure
			}
			return nil, n.failure(err)
		}
	case "len":
		arg, err := n.args[0].operand(root)
		if err != nil {
			return nil, err
		}
		length := 0
		switch kind(arg) {
		case nullKind:
		case stringKind:
			length = utf8.RuneCountInString(reflect.ValueOf(arg).String())
		case arrayKind:
			length = reflect.ValueOf(arg).Len()
		default:
			return nil, n.failure(fmt.Errorf("expected string, array or null, got %s", kind(arg)))
		}
		value = length
	}
	normalized, err := normalizeNumber(value)
	if err != nil {
		return nil, n.failure(err)
	}
	return normalized, nil
}

func normalizeNumber(value any) (any, error) {
	if v, ok := value.(json.Number); ok {
		return parseNumber(string(v))
	}
	if kind(value) == numberKind {
		return goNumber(reflect.ValueOf(value))
	}
	return value, nil
}

func (n *node) failure(err error) *EvalError {
	result := &EvalError{
		Op:  n.op,
		Pos: n.pos,
		Err: err,
	}
	n.collectPaths(&result.Paths)
	return result
}

func (n *node) collectPaths(paths *[]string) {
	if n.op == "path" {
		*paths = append(*paths, n.text)
	}
	for _, arg := range n.args {
		arg.collectPaths(paths)
	}
}

func kind(value any) string {
	if value == nil {
		return nullKind
	}
	switch value.(type) {
	case json.Number, decimal:
		return numberKind
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Bool:
		return booleanKind
	case reflect.String:
		return stringKind
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return numberKind
	case reflect.Slice, reflect.Array:
		return arrayKind
	case reflect.Map:
		return objectKind
	default:
		return unsupportedKind
	}
}

func compare(op string, a, b any) (bool, error) {
	ak, bk := kind(a), kind(b)
	equality := op == "==" || op == "!="
	if !equality && (ak == nullKind || bk == nullKind) {
		return false, nil
	}
	if ak == unsupportedKind || bk == unsupportedKind {
		return false, errors.New("expected null, boolean, number, string, array or object")
	}
	if equality && ak != bk {
		return op == "!=", nil
	}
	order := 0
	switch {
	case ak == numberKind && bk == numberKind:
		order = a.(decimal).compare(b.(decimal))
	case ak == stringKind && bk == stringKind:
		order = strings.Compare(reflect.ValueOf(a).String(), reflect.ValueOf(b).String())
	case equality && ak == nullKind:
	case equality && ak == booleanKind:
		if reflect.ValueOf(a).Bool() != reflect.ValueOf(b).Bool() {
			order = 1
		}
	default:
		return false, fmt.Errorf("unsupported operands %s and %s", ak, bk)
	}
	switch op {
	case "==":
		return order == 0, nil
	case "!=":
		return order != 0, nil
	case "<":
		return order < 0, nil
	case "<=":
		return order <= 0, nil
	case ">":
		return order > 0, nil
	default:
		return order >= 0, nil
	}
}

func predicate(op string, a, b any) (bool, error) {
	if a == nil {
		return false, nil
	}
	ak, bk := kind(a), kind(b)
	if op == "contains" && ak == arrayKind {
		array := reflect.ValueOf(a)
		for i := 0; i < array.Len(); i++ {
			element := array.Index(i).Interface()
			if kind(element) == arrayKind || kind(element) == objectKind {
				continue
			}
			value, err := normalizeNumber(element)
			if err != nil {
				return false, fmt.Errorf("array element %d: %w", i, err)
			}
			match, err := compare("==", value, b)
			if err != nil {
				return false, fmt.Errorf("array element %d: %w", i, err)
			}
			if match {
				return true, nil
			}
		}
		return false, nil
	}
	if ak != stringKind || bk != stringKind {
		want := "two strings"
		if op == "contains" {
			want += " or an array and a value"
		}
		return false, fmt.Errorf("expected %s, got %s and %s", want, ak, bk)
	}
	x, y := reflect.ValueOf(a).String(), reflect.ValueOf(b).String()
	switch op {
	case "contains":
		return strings.Contains(x, y), nil
	case "starts_with":
		return strings.HasPrefix(x, y), nil
	default:
		return strings.HasSuffix(x, y), nil
	}
}
