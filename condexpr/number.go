package condexpr

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

const maxExponent = 10000

var numberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// decimal keeps significant digits and their order without expanding exponents.
type decimal struct {
	digits   string
	order    int
	negative bool
}

func parseNumber(s string) (decimal, error) {
	if !numberPattern.MatchString(s) {
		return decimal{}, errors.New("expected a finite JSON number")
	}
	n := decimal{
		negative: s[0] == '-',
	}
	s = strings.TrimPrefix(s, "-")
	exponent := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		value, err := strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil || value < -maxExponent || value > maxExponent {
			return decimal{}, errors.New("number exponent must be between -10000 and 10000")
		}
		exponent = int(value)
		s = s[:i]
	}
	point := strings.IndexByte(s, '.')
	if point < 0 {
		point = len(s)
	}
	digits := strings.ReplaceAll(s, ".", "")
	significant := strings.TrimLeft(digits, "0")
	n.digits = strings.TrimRight(significant, "0")
	if n.digits == "" {
		return decimal{}, nil
	}
	n.order = point - (len(digits) - len(significant)) + exponent
	return n, nil
}

func goNumber(v reflect.Value) (decimal, error) {
	var s string
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s = strconv.FormatUint(v.Uint(), 10)
	default:
		s = strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())
	}
	return parseNumber(s)
}

func (a decimal) compare(b decimal) int {
	if a.negative != b.negative {
		if a.negative {
			return -1
		}
		return 1
	}
	result := 0
	switch {
	case a.digits == "" && b.digits == "":
	case a.digits == "":
		result = -1
	case b.digits == "":
		result = 1
	case a.order < b.order:
		result = -1
	case a.order > b.order:
		result = 1
	default:
		for i := 0; i < max(len(a.digits), len(b.digits)); i++ {
			x, y := byte('0'), byte('0')
			if i < len(a.digits) {
				x = a.digits[i]
			}
			if i < len(b.digits) {
				y = b.digits[i]
			}
			if x < y {
				result = -1
				break
			}
			if x > y {
				result = 1
				break
			}
		}
	}
	if a.negative {
		return -result
	}
	return result
}
