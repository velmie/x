package pathexpr_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/velmie/x/pathexpr"
)

func TestProjectionNumberFidelityAndOwnership(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"amount":9007199254740993,"price":12.10,"data":{"key":"original"}}`))
	decoder.UseNumber()
	var event map[string]any
	if err := decoder.Decode(&event); err != nil {
		t.Fatal(err)
	}
	expr, err := pathexpr.Compile(`{amount: event.amount, price: event.price, selected: event.data, fresh: {value:event.amount}}`)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"event": event}
	got, err := expr.Eval(root)
	if err != nil {
		t.Fatal(err)
	}
	out := got.(map[string]any)
	bytes, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bytes), `"amount":9007199254740993`) || !strings.Contains(string(bytes), `"price":12.10`) {
		t.Fatalf("lost fidelity: %s", bytes)
	}
	again, err := expr.Eval(root)
	if err != nil {
		t.Fatal(err)
	}
	out["fresh"].(map[string]any)["value"] = "changed"
	if again.(map[string]any)["fresh"].(map[string]any)["value"] != json.Number("9007199254740993") {
		t.Fatal("constructed objects alias")
	}
	out["selected"].(map[string]any)["key"] = "alias"
	if event["data"].(map[string]any)["key"] != "alias" {
		t.Fatal("selected value did not preserve alias")
	}
}

func TestConcurrentReuse(t *testing.T) {
	expr, err := pathexpr.Compile(`{id:event.id, optional:event.missing?, literal:12.10}`)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"event": map[string]any{"id": "id"}}
	want := map[string]any{"id": "id", "literal": json.Number("12.10")}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got, err := expr.Eval(root)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("got %#v, %v", got, err)
					return
				}
				_ = expr.Inspect()
			}
		}()
	}
	wg.Wait()
}

func TestCompleteSyntaxRejection(t *testing.T) {
	cases := []string{"[01]", "a[18446744073709551616]", "a[]", "a[0]z", "a(b)", "{a:b?,a:c}", "{a:b? ?}", "{a:b???}", "a??b", "{a:'unescaped'quote'}", "\xff", "{é:\r\n }"}
	for _, source := range cases {
		t.Run(source, func(t *testing.T) {
			_, err := pathexpr.Compile(source)
			var syntax *pathexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("expected SyntaxError, got %v", err)
			}
		})
	}
}

func FuzzCompile(f *testing.F) {
	for _, seed := range []string{"", "{}", "{a: b[0].c??}", "'x,}'", "[0]", "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		expr, err := pathexpr.Compile(source)
		if err == nil {
			_, _ = expr.Eval(map[string]any{})
			_ = expr.Inspect()
		}
		path, err := pathexpr.CompilePath(source)
		if err == nil {
			_, _ = path.Get(map[string]any{})
		}
	})
}

func TestLegacyExpressionCases(t *testing.T) {
	root := map[string]any{
		"simplePath1": "resolvedValue1", "key2": "resolvedValue2", "key": []string{"val1", "val2", "val3"},
		"nested":       map[string]any{"key1": "nestedValue1", "key2": "nestedValue2", "key3": "nestedValue3"},
		"availableKey": "value", "scalar": "private-value",
	}
	cases := []struct {
		source string
		want   any
	}{
		{
			source: "true",
			want:   true,
		},
		{
			source: "false",
			want:   false,
		},
		{
			source: "42",
			want:   json.Number("42"),
		},
		{
			source: "3.14",
			want:   json.Number("3.14"),
		},
		{
			source: "'hello'",
			want:   "hello",
		},
		{
			source: "'a'",
			want:   "a",
		},
		{
			source: "key[1]",
			want:   "val2",
		},
		{
			source: "{key1:simplePath1,key2}",
			want:   map[string]any{"key1": "resolvedValue1", "key2": "resolvedValue2"},
		},
		{
			source: "{from_nested:{nested1:nested.key1,nested2:nested.key2,nested.key3}}",
			want:   map[string]any{"from_nested": map[string]any{"nested1": "nestedValue1", "nested2": "nestedValue2", "nested.key3": "nestedValue3"}},
		},
		{
			source: "{optionalKey?,alias:optionalKey2?,nested:{optionalKey3?},availableKey}",
			want:   map[string]any{"nested": map[string]any{}, "availableKey": "value"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := pathexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
	for _, source := range []string{"key[5]", "scalar[0]", "scalar.nested"} {
		expr, err := pathexpr.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(root)
		if got != nil || err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("%s: got %#v, %v", source, got, err)
		}
	}
}

func TestLegacyInvalidProjectionCases(t *testing.T) {
	cases := []string{
		"{?}", "{??}", "{claim:?}", "{claim:??}", "{claim:}",
		"{,claim:true}", "{claim:true,,other:true}", "{claim:true,}",
		"{claim:true,claim:false}", "{claim:missing?,claim:true}",
		"{claim:{nested:true}junk}", "{claim:{nested:true}",
	}
	for _, source := range cases {
		t.Run(source, func(t *testing.T) {
			_, err := pathexpr.Compile(source)
			var syntax *pathexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("got %v, want syntax error", err)
			}
		})
	}
}

func TestTypedValuesAndFailurePhase(t *testing.T) {
	type child map[string]any
	root := map[string]child{"parent": {"array": [2]int{1, 2}, "ptr": new(int), "slice": []string{"value"}}}
	for _, tc := range []struct {
		source string
		want   any
	}{
		{
			source: "parent.array",
			want:   [2]int{1, 2},
		},
		{
			source: "parent.slice[0]",
			want:   "value",
		},
	} {
		expr, err := pathexpr.Compile(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(root)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %#v, %v", tc.source, got, err)
		}
	}
	for _, source := range []string{"parent.array[0]", "parent.ptr.value"} {
		expr, err := pathexpr.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(root)
		var typed *pathexpr.TypeError
		if got != nil || !errors.As(err, &typed) {
			t.Fatalf("%s: got %#v, %v", source, got, err)
		}
	}
}
