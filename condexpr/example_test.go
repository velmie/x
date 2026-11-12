package condexpr_test

import (
	"errors"
	"fmt"

	"github.com/velmie/x/condexpr"
	"github.com/velmie/x/pathexpr"
)

func ExampleCompile() {
	expr, err := condexpr.Compile("event.type == 'order.paid' && event.totalMinor >= 100000")
	if err != nil {
		panic(err)
	}
	send, err := expr.Eval(map[string]any{
		"event": map[string]any{"type": "order.paid", "totalMinor": 125000},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(send)
	// Output: true
}

func ExampleExpr_Inspect() {
	expr, err := condexpr.Compile("event.id != '' || event.private? != null")
	if err != nil {
		panic(err)
	}
	fmt.Println(expr.Inspect().Paths)
	// Output: [event.id event.private]
}

func ExampleExpr_Eval_diagnostics() {
	expr, err := condexpr.Compile("event.total > 0")
	if err != nil {
		panic(err)
	}
	_, err = expr.Eval(map[string]any{"event": map[string]any{}})
	fmt.Println(errors.Is(err, pathexpr.ErrNotFound))
	var detail *pathexpr.EvalError
	if errors.As(err, &detail) {
		fmt.Println(detail.Path, detail.At)
	}
	// Output:
	// true
	// event.total event.total
}
