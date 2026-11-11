package pathexpr_test

import (
	"errors"
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

func ExampleExpr_Eval_diagnostics() {
	expr, err := pathexpr.Compile("{name: user.name}")
	if err != nil {
		panic(err)
	}
	_, err = expr.Eval(map[string]any{"user": map[string]any{}})
	fmt.Println(errors.Is(err, pathexpr.ErrNotFound))
	var detail *pathexpr.EvalError
	if errors.As(err, &detail) {
		fmt.Println(detail.Path, detail.At)
	}
	// Output:
	// true
	// user.name user.name
}
