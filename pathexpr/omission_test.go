package pathexpr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/velmie/x/pathexpr"
)

func TestOmissionAndInspection(t *testing.T) {
	expr, err := pathexpr.Compile(`{missing: absent?, nilValue: nilValue?, zero: zero??, tiny: tiny??, outer: {required: absent}?, forbidden: private.secret?}`)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"nilValue": nil, "zero": json.Number("-0.00e99"), "tiny": json.Number("1e-999"), "private": map[string]any{"secret": "hidden"}}
	got, err := expr.Eval(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"nilValue": nil, "tiny": json.Number("1e-999"), "forbidden": "hidden"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	info := expr.Inspect()
	if !info.IsObject || info.Depth != 2 || len(info.Keys) != 7 {
		t.Fatalf("shape: %#v", info)
	}
	allowed := map[string]bool{"absent": true, "nilValue": true, "zero": true, "tiny": true}
	rejected := false
	for _, path := range info.Paths {
		if !allowed[path] {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("validator accepted optional forbidden path")
	}
}

func TestOptionalStillReportsTypeFailure(t *testing.T) {
	for _, suffix := range []string{"?", "??"} {
		expr, err := pathexpr.Compile("{result: user.name" + suffix + "}")
		if err != nil {
			t.Fatal(err)
		}
		value, err := expr.Eval(map[string]any{"user": "secret"})
		var typed *pathexpr.TypeError
		var eval *pathexpr.EvalError
		if value != nil || !errors.As(err, &typed) || !errors.As(err, &eval) {
			t.Fatalf("got %#v, %v", value, err)
		}
		if eval.Path != "user.name" || eval.At != "user" {
			t.Fatalf("bad location: %#v", eval)
		}
	}
}

func TestEmptyValueMatrix(t *testing.T) {
	type zeroStruct struct {
		Count int
	}
	var nilMap map[string]any
	var nilSlice []any
	values := map[string]any{
		"nil": nil, "false": false, "int": 0, "number": json.Number("0e-999"),
		"text": "", "struct": zeroStruct{}, "array": [2]int{}, "nilMap": nilMap, "nilSlice": nilSlice,
		"map": map[string]any{}, "slice": []any{}, "nonzero": json.Number("1e-999"),
	}
	expr, err := pathexpr.Compile(`{nil??, falseValue: false??, int??, number??, text??, struct??, array??, nilMap??, nilSlice??, map??, slice??, nonzero??}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := expr.Eval(values)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"map": map[string]any{}, "slice": []any{}, "nonzero": json.Number("1e-999")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEventExportShapePolicy(t *testing.T) {
	validate := func(source string) bool {
		expr, err := pathexpr.Compile(source)
		if err != nil {
			return false
		}
		info := expr.Inspect()
		if !info.IsObject || info.Depth > 8 || len(info.Keys) > 64 {
			return false
		}
		for _, key := range info.Keys {
			if len(key) == 0 || len(key) > 128 {
				return false
			}
		}
		for _, path := range info.Paths {
			if path != "event.id" && path != "event.name" {
				return false
			}
		}
		return true
	}
	cases := []struct {
		source string
		valid  bool
	}{
		{
			source: `{id:event.id,name:event.name?}`,
			valid:  true,
		},
		{
			source: `{id:event.id,secret:event.secret?}`,
			valid:  false,
		},
		{
			source: `event.id`,
			valid:  false,
		},
		{
			source: `{a:{b:{c:{d:{e:{f:{g:{h:{i:event.id}}}}}}}}}`,
			valid:  false,
		},
		{
			source: `{` + strings.Repeat("a", 129) + `:event.id}`,
			valid:  false,
		},
	}
	var fields []string
	for i := 0; i < 65; i++ {
		fields = append(fields, fmt.Sprintf("k%d:event.id", i))
	}
	cases = append(cases, struct {
		source string
		valid  bool
	}{
		source: "{" + strings.Join(fields, ",") + "}",
		valid:  false,
	})
	for _, tc := range cases {
		if got := validate(tc.source); got != tc.valid {
			t.Errorf("%s: got %v", tc.source, got)
		}
	}
}

func TestDiagnosticsPreserveCauseAndPosition(t *testing.T) {
	expr, err := pathexpr.Compile("{\r\n  output: user.name\r\n}")
	if err != nil {
		t.Fatal(err)
	}
	result, err := expr.Eval(map[string]any{"user": map[string]any{}})
	var eval *pathexpr.EvalError
	if result != nil || !errors.Is(err, pathexpr.ErrNotFound) || !errors.As(err, &eval) {
		t.Fatalf("got %#v, %v", result, err)
	}
	if eval.Path != "user.name" || eval.At != "user.name" || eval.Pos.Line != 2 || eval.Pos.Column != 16 || !strings.Contains(err.Error(), `field "output"`) {
		t.Fatalf("diagnostic: %v (%#v)", err, eval)
	}
	_, err = pathexpr.Compile("{\r\n  名: event.\r\n}")
	var syntax *pathexpr.SyntaxError
	if !errors.As(err, &syntax) || syntax.Pos.Line != 3 || syntax.Pos.Column != 1 {
		t.Fatalf("syntax: %#v, %v", syntax, err)
	}
}
