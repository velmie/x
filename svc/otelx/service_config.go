package otelx

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/validation"
)

// ServiceConfig selects the standard service telemetry profile.
// Its zero value disables telemetry. confload applies tagged defaults.
type ServiceConfig struct {
	Enabled         bool              `k:"enabled" default:"false" hint:"Enable traces and metrics"`
	TraceProtocol   string            `k:"trace_protocol" default:"http" hint:"Trace exporter" validate:"omitempty,oneof=http grpc stdout"`
	Endpoint        string            `k:"endpoint" default:"127.0.0.1:4318" hint:"Trace host:port without scheme or path"`
	MetricsEndpoint string            `k:"metrics_endpoint" hint:"HTTP metrics host:port; required with gRPC traces"`
	CAFile          string            `k:"ca_file" hint:"PEM trust roots for collector TLS"`
	Headers         map[string]string `k:"headers" hint:"Collector authentication headers" sensitive:"true"`
	SampleRatio     float64           `k:"sample_ratio" default:"1" hint:"Root trace sampling ratio" validate:"gte=0,lte=1"`
	Environment     string            `k:"environment" default:"local" hint:"Non-empty deployment environment name" validate:"required"`
	Propagation     PropagationFormat `k:"propagation" default:"w3c" hint:"Peer encoding" validate:"omitempty,oneof=w3c b3-single b3-multi"`
	AllowBaggage    bool              `k:"allow_baggage" default:"false" hint:"Permit interactive or batch traffic.class"`
	StdoutFile      string            `k:"stdout_file" hint:"New exclusive file for stdout trace export"`
	LocalPlaintext  bool              `k:"local_plaintext" default:"false" hint:"Permit plaintext to literal loopback addresses"`
	MetricsMode     string            `k:"metrics_mode" default:"otlp" hint:"Metric collection mode" validate:"omitempty,oneof=otlp prometheus"`
	BatchTimeout    time.Duration     `k:"batch_timeout" default:"100ms" hint:"Span batch delay" validate:"gte=0"`
	MetricInterval  time.Duration     `k:"metric_interval" default:"1s" hint:"OTLP metric export interval; unused in Prometheus mode" validate:"gte=0"`
	ExportTimeout   time.Duration     `k:"export_timeout" default:"1s" hint:"Timeout for each telemetry export" validate:"gte=0"`
	RuntimeMetrics  bool              `k:"runtime_metrics" default:"false" hint:"Go and process collectors in Prometheus mode"`
}

const (
	resourceAttributesEnv = "OTEL_RESOURCE_ATTRIBUTES"
	configRuleFormat      = "format"
	configRuleValue       = "value"
)

// ValidateServiceConfig checks the fields of ServiceConfig only.
// It opens no files, connections, listeners, exporters, or workers.
// Configuration violations use relative k paths.
func ValidateServiceConfig(cfg ServiceConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if err := validation.FromStruct(cfg, configValidator.Struct(cfg)); err != nil {
		var failure *validation.Error
		if errors.As(err, &failure) {
			failure.Cause = errors.Join(failure.Cause, ErrInvalidConfig)
		}
		return err
	}
	if cfg.TraceProtocol == "" {
		cfg.TraceProtocol = "http"
	}
	if cfg.MetricsMode == "" {
		cfg.MetricsMode = "otlp"
	}
	if cfg.RuntimeMetrics && cfg.MetricsMode != "prometheus" {
		return configFailure("runtime_metrics", configRuleValue, "runtime metrics require Prometheus mode", nil)
	}
	if cfg.TraceProtocol == "stdout" && (cfg.StdoutFile == "" || len(cfg.StdoutFile) > 4096 || strings.ContainsAny(cfg.StdoutFile, "\x00\r\n")) {
		return configFailure("stdout_file", configRuleFormat, "nonempty file path of at most 4096 bytes", nil)
	}
	if !networkSelected(cfg) {
		return nil
	}
	if cfg.TraceProtocol != "stdout" {
		if err := validateEndpoint(cfg.Endpoint, cfg.LocalPlaintext); err != nil {
			return configFailure("endpoint", configRuleFormat, "host:port with numeric port from 1 to 65535 and permitted TLS mode", err)
		}
	}
	if cfg.MetricsMode != "prometheus" {
		endpoint := cfg.MetricsEndpoint
		if endpoint == "" {
			if cfg.TraceProtocol == "grpc" {
				return configFailure("metrics_endpoint", apperror.RuleRequired, "metrics endpoint when trace protocol is grpc", nil)
			}
			endpoint = cfg.Endpoint
		}
		if err := validateEndpoint(endpoint, cfg.LocalPlaintext); err != nil {
			field := "metrics_endpoint"
			if cfg.MetricsEndpoint == "" {
				field = "endpoint"
			}
			return configFailure(field, configRuleFormat, "host:port with numeric port from 1 to 65535 and permitted TLS mode", err)
		}
	}
	if len(cfg.Headers) > 8 {
		return configFailure("headers", apperror.RuleMax, "8", nil)
	}
	for key, value := range cfg.Headers {
		if len(key) < 1 || len(key) > 64 || len(value) < 1 || len(value) > 4096 || strings.HasPrefix(key, "grpc-") {
			return configFailure("headers", configRuleFormat, "lowercase header names and bounded printable values", nil)
		}
		for _, c := range key {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return configFailure("headers", configRuleFormat, "lowercase header names and bounded printable values", nil)
			}
		}
		for _, c := range value {
			if c < 32 || c > 126 {
				return configFailure("headers", configRuleFormat, "lowercase header names and bounded printable values", nil)
			}
		}
	}
	return nil
}

var configValidator = newConfigValidator()

func newConfigValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	return v
}
func configFailure(field, rule, param string, cause error) error {
	return validation.Invalid(field, rule, param, errors.Join(ErrInvalidConfig, cause))
}
func networkSelected(cfg ServiceConfig) bool {
	return cfg.TraceProtocol != "stdout" || cfg.MetricsMode != "prometheus"
}

func validateEndpoint(endpoint string, localPlaintext bool) error {
	invalid := ErrInvalidConfig
	if len(endpoint) > 253 || strings.ContainsAny(endpoint, "/@?#\\\r\n\t %") {
		return invalid
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return invalid
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return invalid
	}
	for _, char := range port {
		if char < '0' || char > '9' {
			return invalid
		}
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" || localPlaintext && !address.Unmap().IsLoopback() {
			return invalid
		}
	} else {
		if localPlaintext {
			return invalid
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return invalid
			}
			for _, char := range label {
				if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
					return invalid
				}
			}
		}
	}
	return nil
}
