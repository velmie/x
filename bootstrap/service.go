package bootstrap

import "context"

// Service represents service for orchestration
type Service interface {
	Start() error
	Stop(ctx context.Context) error
}

// StartFunc is a service startup func
type StartFunc func() error

// StopFunc is a service stop func
type StopFunc func(ctx context.Context) error

// serviceFunc represents wrapper over StartFunc and StopFunc func for Service interface implementation
type serviceFunc struct {
	start StartFunc
	stop  StopFunc
}

// Start is called on service startup
func (s *serviceFunc) Start() error {
	return s.start()
}

// Stop is executed right after stop signal is sent
func (s *serviceFunc) Stop(ctx context.Context) error {
	return s.stop(ctx)
}

// ServiceFunc builds serviceFunc
func ServiceFunc(start StartFunc, stop StopFunc) *serviceFunc {
	return &serviceFunc{start: start, stop: stop}
}
