package pathexpr_test

import (
	"fmt"

	"github.com/velmie/x/pathexpr"
)

func ExampleCompile() {
	expr, err := pathexpr.Compile("{id: event.id}")
	if err != nil {
		panic(err)
	}
	result, err := expr.Eval(map[string]any{
		"event": map[string]any{
			"id": "evt-1",
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.(map[string]any)["id"])
	// Output: evt-1
}

func ExampleExpr_Inspect() {
	expr, err := pathexpr.Compile("{id: event.id, extra: event.secret?}")
	if err != nil {
		panic(err)
	}
	info := expr.Inspect()
	fmt.Println(info.IsObject, info.Paths, info.Keys, info.Depth)
	// Output: true [event.id event.secret] [id extra] 1
}

func ExampleCompilePath() {
	path, err := pathexpr.CompilePath("profile.name")
	if err != nil {
		panic(err)
	}
	source := map[string]any{}
	if err := path.Set(source, "Alex"); err != nil {
		panic(err)
	}
	value, err := path.Get(source)
	if err != nil {
		panic(err)
	}
	fmt.Println(value)
	// Output: Alex
}
