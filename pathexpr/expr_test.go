package pathexpr_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/velmie/x/pathexpr"
)

func TestProjection(t *testing.T) {
	expression, err := pathexpr.Compile(`{
        id: event.id,
        details: {sku: event.lines[0].sku, note: 'a, {b}'},
        exact: 9007199254740993,
        decimal: 12.10
    }`)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{
		"event": map[string]any{
			"id":    "e1",
			"lines": []map[string]string{{"sku": "sku1"}},
		},
	}
	got, err := expression.Eval(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"id":      "e1",
		"details": map[string]any{"sku": "sku1", "note": "a, {b}"},
		"exact":   json.Number("9007199254740993"),
		"decimal": json.Number("12.10"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCompileAndEvaluatePaths(t *testing.T) {
	type named map[string]any
	root := named{
		"a": []map[string]int{{"value": 7}},
	}
	cases := []struct {
		source string
		want   any
	}{
		{
			source: ".",
			want:   root,
		},
		{
			source: "a[0].value",
			want:   7,
		},
		{
			source: "{a[0].value}",
			want:   map[string]any{"a[0].value": 7},
		},
		{
			source: "{empty:{}}",
			want:   map[string]any{"empty": map[string]any{}},
		},
		{
			source: "{name:'a\\'b\\n'}",
			want:   map[string]any{"name": "a'b\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := pathexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestCompileRejectsIncompleteExpressions(t *testing.T) {
	for _, source := range []string{"", "a.", "a[01]", "a[-1]", "{a,}", "{a,,b}", "{x:a,x:b}", "{a} trailing", "'bad\\q'", "{true}"} {
		t.Run(source, func(t *testing.T) {
			if _, err := pathexpr.Compile(source); err == nil {
				t.Fatal("expected syntax error")
			}
		})
	}
}

func TestNumberAndWhitespaceSeparatedPath(t *testing.T) {
	root := map[string]any{"12": map[string]any{"10": "selected"}}
	for _, tc := range []struct {
		source string
		want   any
	}{
		{
			source: "12.10",
			want:   json.Number("12.10"),
		},
		{
			source: "12 . 10",
			want:   "selected",
		},
		{
			source: "{12 . 10}",
			want:   map[string]any{"12.10": "selected"},
		},
	} {
		expr, err := pathexpr.Compile(tc.source)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		got, err := expr.Eval(root)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %#v, %v; want %#v", tc.source, got, err, tc.want)
		}
	}
}

func TestInvalidEscapePositionStartsAtCharacter(t *testing.T) {
	_, err := pathexpr.Compile("'a\\é'")
	var syntax *pathexpr.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("Compile() error = %v, want SyntaxError", err)
	}
	if syntax.Pos.Offset != 3 || syntax.Pos.Column != 4 {
		t.Fatalf("position = %+v, want offset 3 column 4", syntax.Pos)
	}
}
