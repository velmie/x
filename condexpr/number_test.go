package condexpr_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/velmie/x/condexpr"
)

func TestNumberRepresentations(t *testing.T) {
	type integer int64
	type unsigned uint64
	type floating float32
	values := []any{int(12), int8(12), int16(12), int32(12), int64(12), uint(12), uint8(12), uint16(12), uint32(12), uint64(12), uintptr(12), float32(12), float64(12), json.Number("12.00e0"), integer(12), unsigned(12), floating(12)}
	expr, err := condexpr.Compile("a == b && a >= b && a <= b")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range values {
		for _, b := range values {
			got, err := expr.Eval(map[string]any{"a": a, "b": b})
			if !got || err != nil {
				t.Errorf("%T versus %T: got %v, %v", a, b, got, err)
			}
		}
	}
}

func TestExactNumbers(t *testing.T) {
	for _, source := range []string{
		"9007199254740993 > 9007199254740992", "12.10 == 12.1", "1e3 == 1000", "1E+3 == 1000.0",
		"1e-3 == 0.001", "-1e3 < -999", "-0 == 0", "-0.000e10000 == 0",
		"-1e-10000 < 0", "0 < 1e-10000", "1e10000 > 9e9999", "1e-10000 < 1e-9999",
		"0.0012 == 12e-4", "0.00120 > 0.00119", "123 < 123.000000000000000000000001",
		"-123 > -123.000000000000000000000001", "123000000000000000000000000000000 == 123e30",
		"1.000000000000000000000000000001 != 1", "1e000001 == 10", "0.00 == 0.0",
	} {
		t.Run(source, func(t *testing.T) {
			expr, err := condexpr.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(nil)
			if !got || err != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		source string
		value  any
	}{
		{
			source: "n == 18446744073709551615",
			value:  uint64(math.MaxUint64),
		},
		{
			source: "n == -9223372036854775808",
			value:  int64(math.MinInt64),
		},
		{
			source: "n > 9007199254740992",
			value:  int64(9007199254740993),
		},
		{
			source: "n == 12.1",
			value:  float32(12.1),
		},
		{
			source: "n == 12.1",
			value:  float64(12.1),
		},
		{
			source: "n > 0",
			value:  math.SmallestNonzeroFloat64,
		},
		{
			source: "n < 1e309",
			value:  math.MaxFloat64,
		},
		{
			source: "n == 0",
			value:  math.Copysign(0, -1),
		},
	} {
		expr, err := condexpr.Compile(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(map[string]any{"n": tc.value})
		if !got || err != nil {
			t.Fatalf("%s: got %v, %v", tc.source, got, err)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(`{"n":9007199254740993}`))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	expr, err := condexpr.Compile("n == 9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := expr.Eval(root); !got || err != nil {
		t.Fatalf("decoded number: %v, %v", got, err)
	}
}

func TestInvalidNumbers(t *testing.T) {
	for _, literal := range []string{"1e10001", "1e-10001", "0e10001", "1e9999999999999999999999999999999999"} {
		for _, prefix := range []string{"event.totalMinor > ", "true == true ||\r\n  event.totalMinor > "} {
			source := prefix + literal
			_, err := condexpr.Compile(source)
			var syntax *condexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("%s: expected SyntaxError, got %v", source, err)
			}
			line, column := 1, 20
			if strings.Contains(prefix, "\n") {
				line, column = 2, 22
			}
			if syntax.Pos.Offset != len(prefix) || syntax.Pos.Line != line || syntax.Pos.Column != column {
				t.Fatalf("%s: unexpected position %+v", source, syntax.Pos)
			}
		}
	}
	for _, value := range []any{json.Number("private-number"), json.Number("01"), json.Number("1e10001"), json.Number("1e-10001"), math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, source := range []string{"n == 0", "contains(items, 0)"} {
			expr, err := condexpr.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(map[string]any{"n": value, "items": []any{value}})
			if got || err == nil || strings.Contains(err.Error(), "private-number") {
				t.Fatalf("%s: got %v, %v", source, got, err)
			}
		}
	}
}
