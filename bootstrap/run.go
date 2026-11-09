package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Component is a named long-running function that owns and joins its work.
// Name must be nonempty and unique within a Run call. Run must be non-nil.
type Component struct {
	Name string
	Run  func(context.Context) error
}

// Run starts all components, stops their shared context when one exits
// unexpectedly or the parent is canceled, and waits for every component.
// Each component is responsible for stopping and joining its own work.
// Retained errors identify their component and are joined in declaration order.
func Run(ctx context.Context, components ...Component) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]error, len(components))
	var group sync.WaitGroup
	for i, component := range components {
		i, component := i, component
		group.Add(1)
		go func() {
			defer group.Done()
			err := component.Run(runCtx)
			if stop := runCtx.Err(); stop == nil {
				if err == nil {
					err = fmt.Errorf("component %q stopped unexpectedly", component.Name)
				} else {
					err = fmt.Errorf("component %q stopped unexpectedly: %w", component.Name, err)
				}
				results[i] = err
				cancel()
			} else if err != nil && err != stop {
				results[i] = fmt.Errorf("component %q: %w", component.Name, err)
			}
		}()
	}
	group.Wait()
	return errors.Join(results...)
}
