package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var ErrNoRegisteredServices = errors.New("orchestrator has no registered services")

type option func(o *options)

type options struct {
	signals         []os.Signal
	shutDownTimeout time.Duration
	logger          Logger
}

// WithStopSignals allows to specify signals which are considered as stop signals by Orchestrator.
// By default, syscall.SIGINT, syscall.SIGTERM signals are considered as stop signals
func WithStopSignals(signals ...os.Signal) option {
	return func(o *options) {
		if len(signals) > 0 {
			o.signals = signals
		}
	}
}

// WithShutdownTimeout sets service shutdown timeout (timeout for each service to be stopped). There is no default timeout
func WithShutdownTimeout(t time.Duration) option {
	return func(o *options) {
		if t > 0 {
			o.shutDownTimeout = t
		}
	}
}

// WithLogger allows to set logger. Provided logger must implement Logger interface.
// If not specified, DefaultLogger is used for logging
func WithLogger(logger Logger) option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// Orchestrator helps to automate application services startup and graceful shutdown
type Orchestrator struct {
	logger          Logger
	services        []Service
	stopCh          chan os.Signal
	signals         []os.Signal
	shutDownTimeout time.Duration
}

// NewOrchestrator builds new Orchestrator
func NewOrchestrator(opts ...option) *Orchestrator {
	o := options{
		signals: []os.Signal{syscall.SIGINT, syscall.SIGTERM},
		logger:  NewNoopLogger(),
	}

	for _, opt := range opts {
		opt(&o)
	}

	return &Orchestrator{
		logger:          o.logger,
		stopCh:          make(chan os.Signal, 1),
		signals:         o.signals,
		shutDownTimeout: o.shutDownTimeout,
	}
}

// Register registers Service for further serving
func (o *Orchestrator) Register(svc Service) {
	o.services = append(o.services, svc)
}

// Serve begins services startup and schedules further graceful shutdown procedures. Function behavior is blocking, any
// stop signal sent begins graceful shutdown procedure
func (o *Orchestrator) Serve() (err error) {
	// verify at least one service is present
	if len(o.services) == 0 {
		return ErrNoRegisteredServices
	}

	errCh := make(chan error)
	signal.Notify(o.stopCh, o.signals...)
	// stop notifying channel after exit since no listeners will be present
	defer signal.Stop(o.stopCh)

	o.logger.Info("services are registered", "numberOfServices", len(o.services))

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	for i := range o.services {
		wg.Add(1)
		go o.serveLifecycle(ctx, o.services[i], &wg, errCh)
	}

	select {
	// first startup error is assigned to return result
	case err = <-errCh:
		o.logger.Error("stopping services because of error: ", "error", err.Error())
	case sig := <-o.stopCh:
		o.logger.Info("stopping the services...", "signal", sig.String())
	}

	cancel()

	o.logger.Info("waiting for services to be stopped")
	wg.Wait()

	return err
}

// Stop sends stop signal, so starting graceful shutdown procedure
func (o *Orchestrator) Stop() {
	o.stopCh <- os.Interrupt
}

func (o *Orchestrator) serveLifecycle(ctx context.Context, svc Service, wg *sync.WaitGroup, errCh chan error) {
	defer wg.Done()

	go func() {
		if err := svc.Start(); err != nil {
			// main error channel accepts only first error, so if error has been already passed by other service,
			// just quit because of canceled context
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
	}()

	<-ctx.Done()

	stopCtx, stopCancel := o.shutdownContext()
	defer stopCancel()

	if err := svc.Stop(stopCtx); err != nil {
		o.logger.Error("unexpected error occurred on service shutdown: ", err)
	}
}

func (o *Orchestrator) shutdownContext() (context.Context, context.CancelFunc) {
	if o.shutDownTimeout > 0 {
		return context.WithTimeout(context.Background(), o.shutDownTimeout)
	}
	return context.WithCancel(context.Background())
}
