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
