// Package otelx assembles explicitly configured OpenTelemetry providers and
// owns their lifecycle. It provides bounded propagation without installing
// global providers, discovering service identity, or opening application files.
package otelx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const defaultFlushTimeout = 2 * time.Second

// ErrInvalidConfig identifies an invalid runtime or propagation configuration.
var ErrInvalidConfig = errors.New("invalid telemetry configuration")

// RuntimeConfig selects providers, propagation, and native SDK configuration.
// Its zero value disables telemetry. A factory transfers ownership of any
// non-nil exporter it returns, including one returned together with an error.
type RuntimeConfig struct {
	// Enabled permits resource acquisition; false returns no-op providers.
	Enabled bool
	// Resource is the explicit resource shared by enabled providers and is required when Enabled is true.
	Resource *resource.Resource
	// Propagator defaults to bounded W3C propagation without baggage.
	Propagator propagation.TextMapPropagator
	// NewSpanExporter optionally constructs the runtime-owned trace exporter.
	NewSpanExporter func(ctx context.Context) (sdktrace.SpanExporter, error)
	// NewMetricExporter optionally constructs the runtime-owned metric exporter.
	NewMetricExporter func(ctx context.Context) (sdkmetric.Exporter, error)
	// PrometheusRegistry selects scraping instead of a periodic metric exporter.
	// The registry must be dedicated to this runtime.
	PrometheusRegistry *prometheus.Registry
	// Sampler defaults to parent-based sampling of all root traces.
	Sampler sdktrace.Sampler
	// Views configure metric streams using native SDK views.
	Views []sdkmetric.View
	// BatchOptions configure the native batch span processor.
	BatchOptions []sdktrace.BatchSpanProcessorOption
	// ReaderOptions configure the native periodic metric reader.
	ReaderOptions []sdkmetric.PeriodicReaderOption
	// FlushTimeout bounds each preliminary flush; zero selects two seconds. Negative values are invalid.
	FlushTimeout time.Duration
}

// Runtime owns providers and exposes their native instrumentation interfaces.
// Stop all producers and the scrape server before Shutdown. Do not shut down
// the exposed providers independently.
type Runtime struct {
	// TracerProvider supplies tracers for application instrumentation.
	TracerProvider trace.TracerProvider
	// MeterProvider supplies meters for application instrumentation.
	MeterProvider metric.MeterProvider
	// Propagator carries the selected boundary policy.
	Propagator propagation.TextMapPropagator
	// PrometheusHandler serves a dedicated registry when Prometheus was selected.
	PrometheusHandler http.Handler

	traces       *sdktrace.TracerProvider
	metrics      *sdkmetric.MeterProvider
	traceExport  *shutdownSpanExporter
	metricExport *shutdownMetricExporter
	flushTimeout time.Duration
	shutdownOnce sync.Once
	shutdownErr  error
}

// Open constructs the selected pipelines and cleans up partial initialization.
// It does not set globals or select an exporter. Enabled runtimes require a
// resource and at least one pipeline. Metric export and Prometheus are mutually
// exclusive. Exporter errors and cleanup failures remain available to errors.Is
// and errors.As. Exporters own native workers; callers retain ownership of
// borrowed files and transports and close them after Shutdown.
//
// The SDK merges OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME into the
// explicit resource, with explicit attributes taking precedence. Native
// exporters may read other OTEL_* settings. Services requiring strict identity
// must validate their environment before calling Open.
func Open(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	propagator := cfg.Propagator
	if propagator == nil {
		propagator = defaultPropagation{}
	}
	r := &Runtime{
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider:  metricnoop.NewMeterProvider(),
		Propagator:     propagator,
	}
	if !cfg.Enabled {
		return r, nil
	}
	if cfg.Resource == nil {
		return nil, fmt.Errorf("Resource is required: %w", ErrInvalidConfig)
	}
	if cfg.NewSpanExporter == nil && cfg.NewMetricExporter == nil && cfg.PrometheusRegistry == nil {
		return nil, fmt.Errorf("at least one exporter or PrometheusRegistry is required: %w", ErrInvalidConfig)
	}
	if cfg.NewMetricExporter != nil && cfg.PrometheusRegistry != nil {
		return nil, fmt.Errorf("NewMetricExporter and PrometheusRegistry are mutually exclusive: %w", ErrInvalidConfig)
	}
	if cfg.FlushTimeout < 0 {
		return nil, fmt.Errorf("FlushTimeout must be nonnegative: %w", ErrInvalidConfig)
	}
	r.flushTimeout = cfg.FlushTimeout
	if r.flushTimeout == 0 {
		r.flushTimeout = defaultFlushTimeout
	}

	var spanExporter sdktrace.SpanExporter
	var metricExporter sdkmetric.Exporter
	var err error
	if cfg.NewSpanExporter != nil {
		spanExporter, err = cfg.NewSpanExporter(ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create span exporter: %w", err), closeSpanExporter(ctx, spanExporter))
		}
		if spanExporter == nil {
			return nil, fmt.Errorf("NewSpanExporter returned nil: %w", ErrInvalidConfig)
		}
	}
	if cfg.NewMetricExporter != nil {
		metricExporter, err = cfg.NewMetricExporter(ctx)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("create metric exporter: %w", err),
				closeMetricExporter(ctx, metricExporter),
				closeSpanExporter(ctx, spanExporter),
			)
		}
		if metricExporter == nil {
			return nil, errors.Join(
				fmt.Errorf("NewMetricExporter returned nil: %w", ErrInvalidConfig),
				closeSpanExporter(ctx, spanExporter),
			)
		}
	}
	var reader sdkmetric.Reader
	if cfg.PrometheusRegistry != nil {
		reader, err = otelprometheus.New(otelprometheus.WithRegisterer(cfg.PrometheusRegistry))
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create Prometheus reader: %w", err), closeSpanExporter(ctx, spanExporter))
		}
		r.PrometheusHandler = promhttp.HandlerFor(cfg.PrometheusRegistry, promhttp.HandlerOpts{})
	} else if metricExporter != nil {
		r.metricExport = &shutdownMetricExporter{
			Exporter: metricExporter,
		}
		reader = sdkmetric.NewPeriodicReader(r.metricExport, cfg.ReaderOptions...)
	}
	if spanExporter != nil {
		r.traceExport = &shutdownSpanExporter{
			exporter: spanExporter,
		}
		sampler := cfg.Sampler
		if sampler == nil {
			sampler = sdktrace.ParentBased(sdktrace.AlwaysSample())
		}
		r.traces = sdktrace.NewTracerProvider(
			sdktrace.WithResource(cfg.Resource),
			sdktrace.WithSampler(sampler),
			sdktrace.WithBatcher(r.traceExport, cfg.BatchOptions...),
		)
		r.TracerProvider = r.traces
	}
	if reader != nil {
		opts := []sdkmetric.Option{
			sdkmetric.WithResource(cfg.Resource),
			sdkmetric.WithReader(reader),
		}
		for _, view := range cfg.Views {
			opts = append(opts, sdkmetric.WithView(view))
		}
		r.metrics = sdkmetric.NewMeterProvider(opts...)
		r.MeterProvider = r.metrics
	}
	return r, nil
}

// Shutdown flushes and stops the owned pipelines exactly once. Each preliminary
// flush is bounded by ctx and FlushTimeout. Final shutdown uses a context with
// cancellation detached and waits for exporter work to finish, so Shutdown may
// outlast ctx. Exporters and borrowed writers must eventually return. Concurrent
// and repeated calls wait for the same completion and return the same result.
func (r *Runtime) Shutdown(ctx context.Context) error {
	r.shutdownOnce.Do(
		func() {
			if r.traceExport != nil {
				r.traceExport.startShutdown()
			}
			if r.metricExport != nil {
				r.metricExport.startShutdown()
			}
			var errs []error
			var traceFlushErr error
			var metricFlushErr error
			if r.traces != nil {
				flushCtx, cancel := context.WithTimeout(ctx, r.flushTimeout)
				traceFlushErr = r.traces.ForceFlush(flushCtx)
				cancel()
				if traceFlushErr != nil {
					errs = append(errs, fmt.Errorf("flush traces: %w", traceFlushErr))
				}
			}
			if r.metrics != nil {
				flushCtx, cancel := context.WithTimeout(ctx, r.flushTimeout)
				metricFlushErr = r.metrics.ForceFlush(flushCtx)
				cancel()
				if metricFlushErr != nil {
					errs = append(errs, fmt.Errorf("flush metrics: %w", metricFlushErr))
				}
			}
			cleanupCtx := context.WithoutCancel(ctx)
			if r.traces != nil {
				if err := r.traces.Shutdown(cleanupCtx); err != nil {
					errs = append(errs, fmt.Errorf("shutdown traces: %w", err))
				}
				for _, err := range r.traceExport.shutdownFailures() {
					if !containsSameError(traceFlushErr, err) {
						errs = append(errs, fmt.Errorf("export traces during shutdown: %w", err))
					}
				}
			}
			if r.metrics != nil {
				metricCleanupCtx := context.WithValue(cleanupCtx, metricShutdownContextKey{}, true)
				metricShutdownErr := r.metrics.Shutdown(metricCleanupCtx)
				var exportFailures []metricExportFailure
				var exporterShutdownErr error
				if r.metricExport != nil {
					exportFailures, exporterShutdownErr = r.metricExport.shutdownFailures()
				}
				shutdownReportedExport := false
				for _, failure := range exportFailures {
					if failure.final && containsSameError(metricShutdownErr, failure.cause) {
						shutdownReportedExport = true
					}
				}
				if metricShutdownErr != nil {
					phase := "shutdown metrics"
					if shutdownReportedExport {
						phase = "export metrics during shutdown"
					}
					errs = append(errs, fmt.Errorf("%s: %w", phase, metricShutdownErr))
				}
				for _, failure := range exportFailures {
					if !failure.final && containsSameError(metricFlushErr, failure.cause) {
						continue
					}
					if failure.final && containsSameError(metricShutdownErr, failure.cause) {
						continue
					}
					errs = append(errs, fmt.Errorf("export metrics during shutdown: %w", failure.cause))
				}
				if exporterShutdownErr != nil {
					errs = append(errs, fmt.Errorf("shutdown metric exporter: %w", exporterShutdownErr))
				}
			}
			r.shutdownErr = errors.Join(errs...)
		},
	)
	return r.shutdownErr
}

func closeSpanExporter(ctx context.Context, exporter sdktrace.SpanExporter) error {
	if exporter == nil {
		return nil
	}
	if err := exporter.Shutdown(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("cleanup span exporter: %w", err)
	}
	return nil
}

func closeMetricExporter(ctx context.Context, exporter sdkmetric.Exporter) error {
	if exporter == nil {
		return nil
	}
	if err := exporter.Shutdown(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("cleanup metric exporter: %w", err)
	}
	return nil
}
