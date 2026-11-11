package pathexpr_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/velmie/x/pathexpr"
)

type namedMap map[string]any

func TestPathGetAndSet(t *testing.T) {
	root := namedMap{"profile": namedMap{"name": "old"}, "items": []any{map[string]any{"name": "before"}}}
	path, err := pathexpr.CompilePath("profile.name")
	if err != nil {
		t.Fatal(err)
	}
	old, err := path.Get(root)
	if err != nil || old != "old" {
		t.Fatalf("get: %v %v", old, err)
	}
	if err := path.Set(root, "new"); err != nil {
		t.Fatal(err)
	}
	nested, err := pathexpr.CompilePath("created.branch.value")
	if err != nil {
		t.Fatal(err)
	}
	if err := nested.Set(root, 42); err != nil {
		t.Fatal(err)
	}
	indexed, err := pathexpr.CompilePath("items[0].name")
	if err != nil {
		t.Fatal(err)
	}
	if err := indexed.Set(root, nil); err != nil {
		t.Fatal(err)
	}
	want := namedMap{"profile": namedMap{"name": "new"}, "created": map[string]any{"branch": map[string]any{"value": 42}}, "items": []any{map[string]any{"name": nil}}}
	if !reflect.DeepEqual(root, want) {
		t.Fatalf("got %#v, want %#v", root, want)
	}
}

func TestPathSetFailuresLeaveRootUnchanged(t *testing.T) {
	for _, source := range []string{"missing[0].value", "items[2]", "typed.value", "profile.name.value", "."} {
		t.Run(source, func(t *testing.T) {
			root := namedMap{"profile": namedMap{"name": "old"}, "items": []any{"first"}, "typed": map[string]string{"value": "old"}}
			before := namedMap{"profile": namedMap{"name": "old"}, "items": []any{"first"}, "typed": map[string]string{"value": "old"}}
			path, err := pathexpr.CompilePath(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := path.Set(root, "new"); err == nil {
				t.Fatal("expected failure")
			}
			if !reflect.DeepEqual(root, before) {
				t.Fatalf("root changed: %#v", root)
			}
		})
	}
	path, _ := pathexpr.CompilePath("typed.value")
	err := path.Set(namedMap{"typed": map[string]string{"value": "old"}}, "new")
	var typed *pathexpr.TypeError
	var positioned *pathexpr.EvalError
	if !errors.As(err, &typed) || !errors.As(err, &positioned) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPathAssignmentContracts(t *testing.T) {
	root := namedMap{"true": 1, "42": "old", "nilMap": map[string]any(nil), "typed": []string{"old"}, "items": []any{1}}
	for _, key := range []string{"true", "42"} {
		path, err := pathexpr.CompilePath(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := path.Set(root, "new"); err != nil {
			t.Fatal(err)
		}
		got, err := path.Get(root)
		if err != nil || got != "new" {
			t.Fatalf("%s: %v, %v", key, got, err)
		}
	}
	for _, source := range []string{"nilMap.value", "typed[0]", "items[2]"} {
		p, err := pathexpr.CompilePath(source)
		if err != nil {
			t.Fatal(err)
		}
		err = p.Set(root, "new")
		if err == nil {
			t.Fatalf("%s: expected failure", source)
		}
		if source == "items[2]" && !errors.Is(err, pathexpr.ErrNotFound) {
			t.Fatalf("%s: %v", source, err)
		}
	}
}
