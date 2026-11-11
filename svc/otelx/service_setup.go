package otelx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// SetupService opens the selected pipelines and owns their resources.
// res supplies explicit service identity and is required when enabled.
// No global providers are installed. Stop producers and scraping before Shutdown.
func SetupService(ctx context.Context, cfg ServiceConfig, res *resource.Resource) (runtime *Runtime, err error) {
	if !cfg.Enabled {
		return Open(ctx, RuntimeConfig{})
	}
	if err := ValidateServiceConfig(cfg, res); err != nil {
		return nil, err
	}
	var output *os.File
	if cfg.TraceProtocol == "stdout" {
		output, err = os.OpenFile(cfg.StdoutFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fmt.Errorf("create telemetry output: %w", err)
		}
		defer func() {
			if err != nil {
				if closeErr := output.Close(); closeErr != nil {
					err = errors.Join(err, fmt.Errorf("close telemetry output: %w", closeErr))
				}
			}
		}()
	}
	if cfg.TraceProtocol == "" {
		cfg.TraceProtocol = "http"
	}
	if cfg.Propagation == "" {
		cfg.Propagation = FormatW3C
	}
	policy, err := servicePropagation(cfg)
	if err != nil {
		return nil, errors.Join(ErrInvalidConfig, fmt.Errorf("invalid telemetry propagation: %w", err))
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if networkSelected(cfg) && cfg.CAFile != "" {
		contents, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errors.Join(ErrInvalidConfig, fmt.Errorf("read telemetry CA: %w", err))
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(contents) {
			return nil, errors.Join(ErrInvalidConfig, errors.New("telemetry CA invalid"))
		}
		tlsConfig.RootCAs = roots
	}
	var transport *http.Transport
	var client *http.Client
	if cfg.TraceProtocol == "http" || cfg.MetricsMode != "prometheus" {
		transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		client = &http.Client{
			Transport:     transport,
			Timeout:       time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("telemetry redirect rejected") },
		}
	}
	scheme := "https"
	if cfg.LocalPlaintext {
		scheme = "http"
	}
	headers := maps.Clone(cfg.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	merged, err := resource.Merge(res, resource.NewSchemaless(attribute.String("deployment.environment.name", cfg.Environment)))
	if err != nil {
		return nil, fmt.Errorf("merge telemetry resource: %w", err)
	}

	options := RuntimeConfig{
		Enabled:    true,
		Resource:   merged,
		Propagator: policy,
		Sampler:    sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		BatchOptions: []sdktrace.BatchSpanProcessorOption{
			sdktrace.WithBatchTimeout(100 * time.Millisecond),
			sdktrace.WithExportTimeout(time.Second),
		},
		ReaderOptions: []sdkmetric.PeriodicReaderOption{
			sdkmetric.WithInterval(time.Second),
			sdkmetric.WithTimeout(time.Second),
		},
	}
	options.NewSpanExporter = func(ctx context.Context) (sdktrace.SpanExporter, error) {
		var exporter sdktrace.SpanExporter
		var err error
		switch cfg.TraceProtocol {
		case "http":
			created, createErr := otlptracehttp.New(
				ctx,
				otlptracehttp.WithEndpointURL(scheme+"://"+cfg.Endpoint+"/v1/traces"),
				otlptracehttp.WithHTTPClient(client),
				otlptracehttp.WithTLSClientConfig(nil),
				otlptracehttp.WithHeaders(headers),
				otlptracehttp.WithCompression(otlptracehttp.NoCompression),
				otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
					Enabled: false,
				}),
			)
			if created != nil {
				exporter = created
			}
			err = createErr
		case "grpc":
			grpcOptions := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpointURL(scheme + "://" + cfg.Endpoint),
				otlptracegrpc.WithHeaders(headers),
				otlptracegrpc.WithCompressor(""),
				otlptracegrpc.WithTimeout(time.Second),
				otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
					Enabled: false,
				}),
				otlptracegrpc.WithDialOption(grpc.WithNoProxy(), grpc.WithDisableRetry()),
			}
			if cfg.LocalPlaintext {
				grpcOptions = append(grpcOptions, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
			} else {
				grpcOptions = append(grpcOptions, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
			}
			created, createErr := otlptracegrpc.New(ctx, grpcOptions...)
			if created != nil {
				exporter = created
			}
			err = createErr
		case "stdout":
			created, createErr := stdouttrace.New(stdouttrace.WithWriter(output))
			if created != nil {
				exporter = created
			}
			err = createErr
		}
		if err != nil && exporter != nil {
			err = errors.Join(err, exporter.Shutdown(context.WithoutCancel(ctx)))
		}
		return exporter, err
	}
	if cfg.MetricsMode == "prometheus" {
		options.PrometheusRegistry = prometheus.NewRegistry()
		if cfg.RuntimeMetrics {
			for _, collector := range []prometheus.Collector{
				collectors.NewGoCollector(),
				collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			} {
				if err := options.PrometheusRegistry.Register(collector); err != nil {
					if transport != nil {
						transport.CloseIdleConnections()
					}
					return nil, fmt.Errorf("register runtime collector: %w", err)
				}
			}
		}
	} else {
		endpoint := cfg.MetricsEndpoint
		if endpoint == "" {
			endpoint = cfg.Endpoint
		}
		options.NewMetricExporter = func(ctx context.Context) (sdkmetric.Exporter, error) {
			exporter, err := otlpmetrichttp.New(
				ctx,
				otlpmetrichttp.WithEndpointURL(scheme+"://"+endpoint+"/v1/metrics"),
				otlpmetrichttp.WithHTTPClient(client),
				otlpmetrichttp.WithTLSClientConfig(nil),
				otlpmetrichttp.WithHeaders(headers),
				otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
				otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
					Enabled: false,
				}),
			)
			if err != nil && exporter != nil {
				err = errors.Join(err, exporter.Shutdown(context.WithoutCancel(ctx)))
			}
			return exporter, err
		}
	}
	runtime, err = Open(ctx, options)
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	runtime.output = output
	runtime.transport = transport
	return runtime, nil
}

func servicePropagation(cfg ServiceConfig) (*Propagation, error) {
	policy := PropagationPolicy{
		Format: cfg.Propagation,
	}
	if cfg.AllowBaggage {
		policy.Baggage = []BaggageRule{
			{
				Key:    TrafficClassKey,
				Values: []string{TrafficClassInteractive, TrafficClassBatch},
			},
		}
	}
	return NewPropagation(policy)
}
