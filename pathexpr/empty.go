package pathexpr

import (
	"encoding/json"
	"reflect"
	"strings"
)

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	if number, ok := value.(json.Number); ok && numberPattern.MatchString(string(number)) {
		mantissa := strings.SplitN(string(number), "e", 2)[0]
		mantissa = strings.SplitN(mantissa, "E", 2)[0]
		for _, r := range mantissa {
			if r >= '1' && r <= '9' {
				return false
			}
		}
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}
