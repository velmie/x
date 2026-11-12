package condexpr_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/velmie/x/condexpr"
	"github.com/velmie/x/pathexpr"
)

func TestRoutingExamples(t *testing.T) {
	root := map[string]any{
		"event": map[string]any{
			"type": "order.paid", "currency": "EUR", "totalMinor": 100000,
			"items": []map[string]string{{"sku": "A-1"}, {"sku": "B-2"}},
		},
		"message": map[string]string{"id": "msg-1"},
	}
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{
			source: "event.type == 'order.paid' && event.currency == 'EUR' && event.totalMinor >= 100000",
			want:   true,
		},
		{
			source: "starts_with(event.type, 'order.') && (event.currency == 'EUR' || event.currency == 'USD')",
			want:   true,
		},
		{
			source: "!contains(event.tags?, 'test')",
			want:   true,
		},
		{
			source: "event.items[0].sku == 'A-1'",
			want:   true,
		},
		{
			source: "len(event.items) > 1",
			want:   true,
		},
		{
			source: "event.discount? != null",
			want:   false,
		},
		{
			source: "event.discount? > 0",
			want:   false,
		},
		{
			source: "message.id != ''",
			want:   true,
		},
		{
			source: "event.totalMinor>=100000",
			want:   true,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := condexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestOptionalPaths(t *testing.T) {
	for _, tc := range []struct {
		source  string
		root    any
		missing bool
		typed   bool
	}{
		{
			source:  "event.missing == null",
			root:    map[string]any{"event": map[string]any{}},
			missing: true,
		},
		{
			source: "event.missing? == null",
			root:   map[string]any{"event": map[string]any{}},
		},
		{
			source: "event[2]? == null",
			root:   map[string]any{"event": []int{1}},
		},
		{
			source: "event.missing? == null",
			root:   map[string]any{"event": "private-input"},
			typed:  true,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := condexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(tc.root)
			var typed *pathexpr.TypeError
			if errors.Is(err, pathexpr.ErrNotFound) != tc.missing || errors.As(err, &typed) != tc.typed {
				t.Fatalf("unexpected result: %v, %v", got, err)
			}
			if !tc.missing && !tc.typed && (!got || err != nil) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestInspect(t *testing.T) {
	expr, err := condexpr.Compile("event . items [ 0 ] . sku == message.id || contains(event.tags?, event.items[0].sku)")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"event.items[0].sku", "message.id", "event.tags", "event.items[0].sku"}
	info := expr.Inspect()
	if !reflect.DeepEqual(info.Paths, want) {
		t.Fatalf("paths: %v", info.Paths)
	}
	info.Paths[0] = "changed"
	if !reflect.DeepEqual(expr.Inspect().Paths, want) {
		t.Fatal("Inspect aliases expression")
	}
}
