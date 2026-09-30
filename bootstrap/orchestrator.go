package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var ErrNoRegisteredServices = errors.New("orchestrator has no registered services")

// Orchestrator starts registered services and coordinates their shutdown.
// Register services before calling Serve. Only one Serve call may be active.
type Orchestrator struct {
	logger          Logger
	services        []Service
	stopCh          chan os.Signal
	signals         []os.Signal
	shutDownTimeout time.Duration
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
// If not specified, NewNoopLogger is used and produces no output.
func WithLogger(logger Logger) option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
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

// Serve starts the services and waits for a stop request, a configured signal,
// or a startup error. It then calls each Service.Stop concurrently and waits for
// those calls to return. Each service owns stopping and joining its own work.
// Serve returns the first observed startup error and all shutdown errors.
// Their causes remain available through errors.Is and errors.As.
func (o *Orchestrator) Serve() (err error) {
	// verify at least one service is present
	if len(o.services) == 0 {
		return ErrNoRegisteredServices
	}

	errCh := make(chan serviceFailure)
	signal.Notify(o.stopCh, o.signals...)
	// stop notifying channel after exit since no listeners will be present
	defer signal.Stop(o.stopCh)

	o.logger.Info("services are registered", "numberOfServices", len(o.services))

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serviceErrors := make([]error, len(o.services)+1)

	for i := range o.services {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if stopErr := o.serveLifecycle(ctx, o.services[i], i, errCh); stopErr != nil {
				serviceErrors[i+1] = fmt.Errorf("bootstrap: service %d shutdown: %w", i, stopErr)
				o.logger.Error("unexpected error occurred on service shutdown: ", "operation", "serve", "stage", "shutdown",
					"reason", "service_stop_failed", "outcome", "failed", "service_index", i, "error", stopErr)
			}
		}(i)
	}

	select {
	// first startup error is assigned to return result
	case failure := <-errCh:
		err = failure.err
		o.logger.Error("stopping services because of error: ", "operation", "serve", "stage", "startup",
			"reason", "service_start_failed", "outcome", "shutdown_requested", "service_index", failure.index, "error", err)
	case sig := <-o.stopCh:
		o.logger.Info("stopping the services...", "signal", sig.String())
	}

	cancel()

	o.logger.Info("waiting for services to be stopped")
	wg.Wait()

	serviceErrors[0] = err
	for _, stopErr := range serviceErrors[1:] {
		if stopErr != nil {
			return errors.Join(serviceErrors...)
		}
	}
	// Preserve the original startup error when shutdown adds no failures.
	return err
}

// Stop requests graceful shutdown without waiting for it. Repeated and
// concurrent calls are safe before, during, and after Serve. A request made
// before Serve is retained. Wait for Serve to return to observe shutdown.
func (o *Orchestrator) Stop() {
	select {
	case o.stopCh <- os.Interrupt:
	default:
	}
}

type option func(o *options)

type options struct {
	signals         []os.Signal
	shutDownTimeout time.Duration
	logger          Logger
}

type serviceFailure struct {
	index int
	err   error
}

func (o *Orchestrator) serveLifecycle(ctx context.Context, svc Service, index int, errCh chan<- serviceFailure) error {
	go func() {
		if err := svc.Start(); err != nil {
			// Serve returns the first observed failure. Later failures still
			// need diagnostics, even when shutdown has already been requested.
			select {
			case errCh <- serviceFailure{index: index, err: err}:
			case <-ctx.Done():
				log := o.logger.Error
				reason := "service_start_failed"
				if errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
					log = o.logger.Info
					reason = "service_stopped"
				}
				log("service returned an error during shutdown", "operation", "serve", "stage", "startup",
					"reason", reason, "outcome", "shutdown_in_progress", "service_index", index, "error", err)
			}
		}
	}()

	<-ctx.Done()

	stopCtx, stopCancel := o.shutdownContext()
	defer stopCancel()

	return svc.Stop(stopCtx)
}

func (o *Orchestrator) shutdownContext() (context.Context, context.CancelFunc) {
	if o.shutDownTimeout > 0 {
		return context.WithTimeout(context.Background(), o.shutDownTimeout)
	}
	return context.WithCancel(context.Background())
}
