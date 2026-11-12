package condexpr_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/velmie/x/condexpr"
	"github.com/velmie/x/pathexpr"
)

func TestComparisonKinds(t *testing.T) {
	values := []any{nil, true, 1, "a", []int{1}, map[string]int{"a": 1}}
	for i, a := range values {
		for j, b := range values {
			for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
				equality := op == "==" || op == "!="
				want, wantError := false, false
				switch {
				case equality && i != j:
					want = op == "!="
				case !equality && (i == 0 || j == 0):
				case i == j && (i == 2 || i == 3 || equality && i < 2):
					want = op == "==" || op == "<=" || op == ">="
				default:
					wantError = true
				}
				expr, err := condexpr.Compile("a " + op + " b")
				if err != nil {
					t.Fatal(err)
				}
				got, err := expr.Eval(map[string]any{"a": a, "b": b})
				if got != want || (err != nil) != wantError {
					t.Errorf("kinds %d %s %d: got %v, %v; want %v, error=%v", i, op, j, got, err, want, wantError)
				}
			}
		}
	}
}

func TestBooleanPrecedenceAndShortCircuit(t *testing.T) {
	for _, tc := range []struct {
		source    string
		want      bool
		wantError bool
	}{
		{
			source: "1 == 1 || 1 == 2 && 1 == 2",
			want:   true,
		},
		{
			source: "(1 == 1 || 1 == 2) && 1 == 2",
		},
		{
			source: "!1 == 2 && !!1 == 1",
			want:   true,
		},
		{
			source: "1 == 1 || missing == true",
			want:   true,
		},
		{
			source: "1 == 2 && missing == true",
		},
		{
			source:    "missing == true || 1 == 1",
			wantError: true,
		},
		{
			source:    "missing == true && 1 == 2",
			wantError: true,
		},
		{
			source:    "!(missing == true)",
			wantError: true,
		},
		{
			source: "1 == 1 || invalidNumber == 1",
			want:   true,
		},
		{
			source:    "contains(null, missing)",
			wantError: true,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := condexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(map[string]any{"invalidNumber": json.Number("1e10001")})
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestFunctionsAndStrings(t *testing.T) {
	type text string
	type flag bool
	root := map[string]any{
		"array":    []any{[]int{1}, map[string]any{"key": "private"}, json.Number("12.10"), nil, true, "tag"},
		"fixed":    [2]int{1, 2},
		"object":   map[string]int{"key": 1},
		"nilSlice": []int(nil),
		"nilMap":   map[string]int(nil),
		"text":     text("é日🙂"),
		"flag":     flag(true),
		"pointer":  new(int),
	}
	for _, source := range []string{
		"contains(null, 'x') == true", // A boolean call cannot be an operand.
	} {
		if _, err := condexpr.Compile(source); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
	for _, source := range []string{
		"!contains(null, 'x')", "!starts_with(null, null)", "!ends_with(null, 1)", "len(null) == 0",
		"contains('order.paid', 'der.')", "starts_with('order.paid', 'order.')", "ends_with('order.paid', '.paid')",
		"contains('', '') && starts_with('', '') && ends_with('', '')",
		"!contains('abc', 'd') && !starts_with('abc', 'b') && !ends_with('abc', 'b')",
		"len(text) == 3", "len('é') == 2", "'é' > 'z'", "'abc' < 'abd'", "'a' < 'aa'",
		"len(array) == 6", "len(fixed) == 2", "len(nilSlice) == 0", "len('') == 0",
		"contains(array, 12.1)", "contains(array, null)", "contains(array, true)", "contains(array, 'tag')",
		"!contains(array, object)", "!contains(array, fixed)", "contains(fixed, 2)", "!contains(nilSlice, 1)",
		"flag == true", "text == 'é日🙂'", "nilMap != null", "nilSlice != null",
		`'\\\'\n\r\t' == '\\\'\n\r\t'`, "len('a\nb') == 3",
	} {
		t.Run(source, func(t *testing.T) {
			expr, err := condexpr.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if !got || err != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
	for _, source := range []string{
		"contains(1, 1)", "contains(true, true)", "contains(object, 'key')", "contains('abc', null)",
		"contains('abc', 1)", "starts_with(array, 1)", "starts_with('abc', true)", "ends_with('abc', 1)",
		"len(1) == 0", "len(true) == 0", "len(object) == 1", "len(len('a')) == 1",
		"pointer == null", "len(pointer) == 0", "nilMap == object", "nilSlice == fixed",
	} {
		t.Run(source, func(t *testing.T) {
			expr, err := condexpr.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			var detail *condexpr.EvalError
			if got || !errors.As(err, &detail) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestPathParityAndLiteralPrecedence(t *testing.T) {
	type key string
	type named map[key]any
	root := named{
		"event":    []map[string]int{{"value": 7}},
		"12":       map[string]int{"10": 8},
		"true":     "key",
		"01":       9,
		"1e":       9,
		"contains": 9,
		"len":      9,
		"null":     map[string]int{"value": 9},
		"😀-id":     9,
	}
	for _, path := range []string{"event[0].value", "event [ 0 ] . value", "12 . 10", "01", "1e", "contains", "len", "null.value", "😀-id"} {
		t.Run(path, func(t *testing.T) {
			compiled, err := pathexpr.CompilePath(path)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := compiled.Get(root)
			if err != nil {
				t.Fatal(err)
			}
			root["expected"] = selected
			expr, err := condexpr.Compile(path + " == expected")
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if !got || err != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
	for _, source := range []string{"true == true", "false != true", "null == null", "12.10 == 12.1"} {
		expr, err := condexpr.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(root)
		if !got || err != nil || len(expr.Inspect().Paths) != 0 {
			t.Fatalf("%s: got %v, %v", source, got, err)
		}
	}
	for _, tc := range []struct {
		source string
		root   any
	}{
		{
			source: ". == 42",
			root:   42,
		},
		{
			source: "[0] == 42",
			root:   []int{42},
		},
	} {
		expr, err := condexpr.Compile(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(tc.root)
		if !got || err != nil {
			t.Fatalf("%s: got %v, %v", tc.source, got, err)
		}
	}
}

func TestConcurrentReuseAndInputOwnership(t *testing.T) {
	expr, err := condexpr.Compile("event.id == 'id' && contains(event.tags, 'live') && event.absent? == null")
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"event": map[string]any{"id": "id", "tags": []string{"live", "test"}}}
	before := map[string]any{"event": map[string]any{"id": "id", "tags": []string{"live", "test"}}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got, err := expr.Eval(root)
				if !got || err != nil {
					t.Errorf("got %v, %v", got, err)
					return
				}
				info := expr.Inspect()
				info.Paths[0] = "changed"
			}
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(root, before) {
		t.Fatal("input changed")
	}
}

func FuzzCompile(f *testing.F) {
	for _, seed := range []string{"", "a>=1", "!contains(a?, 'x')", "len(a[0]) == 1", "\xff", "'a\\é' == ''", "a . b == 2", "1e10001==0", strings.Repeat("!", 129) + "a==1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		expr, err := condexpr.Compile(source)
		if err == nil {
			_, _ = expr.Eval(map[string]any{})
			_ = expr.Inspect()
		} else {
			var syntax *condexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("expected SyntaxError: %v", err)
			}
		}
	})
}
