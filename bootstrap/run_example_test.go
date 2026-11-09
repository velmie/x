package bootstrap_test

import (
	"context"
	"fmt"

	"github.com/velmie/x/bootstrap"
)

func ExampleRun() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := bootstrap.Run(ctx, bootstrap.Component{
		Name: "worker",
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	fmt.Println(err)
	// Output: <nil>
}
