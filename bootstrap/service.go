package bootstrap

import "context"

// Service owns the work started by Start. Start and Stop may overlap.
type Service interface {
	Start() error
	// Stop requests termination and waits for the service's own work to finish.
	// It should honor ctx and return an error if shutdown cannot complete.
	// The orchestrator waits for Stop, not for an arbitrary Start goroutine.
	Stop(ctx context.Context) error
}

// StartFunc is a service startup func
type StartFunc func() error

// StopFunc is a service stop func
type StopFunc func(ctx context.Context) error

// ServiceFunc builds a Service from start and stop functions. The stop function
// is responsible for stopping and waiting for work started by the start function.
func ServiceFunc(start StartFunc, stop StopFunc) *serviceFunc {
	return &serviceFunc{start: start, stop: stop}
}

// Start is called on service startup
func (s *serviceFunc) Start() error {
	return s.start()
}

// Stop is executed right after stop signal is sent
func (s *serviceFunc) Stop(ctx context.Context) error {
	return s.stop(ctx)
}

// serviceFunc implements Service using caller-owned functions.
type serviceFunc struct {
	start StartFunc
	stop  StopFunc
}
