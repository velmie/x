package otelx

import (
	"github.com/velmie/x/envx"
)

var env = envx.CreatePrototype().WithPrefix("TRACING_")

// ConfigFromEnv creates a new Config structure by reading environment variables.
// It sets up the various tracing configuration sections like Communication,
// Security, Resource, Sampling, and Propagation by reading their respective
// environment variables.
func ConfigFromEnv() (*Config, error) {
	c := &Config{}

	disabled := envx.Coalesce("OTEL_SDK_DISABLED", "TRACING_DISABLED")
	err := envx.Supply(
		envx.Set(&c.Disabled, envx.Default(DefaultConfig.Disabled, disabled, disabled.Boolean)),
		envx.Set(&c.Communication, communicationConfigFromEnv),
		envx.Set(&c.Security, securityConfigFromEnv),
		envx.Set(&c.Resource, resourceConfigFromEnv),
		envx.Set(&c.Sampling, samplingConfigFromEnv),
		envx.Set(&c.Propagation, propagationConfigFromEnv),
	)

	return c, err
}

// propagationConfigFromEnv creates a PropagationConfig by reading specific
// environment variables related to trace propagation settings.
func propagationConfigFromEnv() (PropagationConfig, error) {
	d := DefaultConfig.Propagation
	c := PropagationConfig{}

	baggage := env.Get("PROPAGATION_BAGGAGE")
	traceContext := env.Get("PROPAGATION_TRACE_CONTEXT")
	b3MultipleHeader := env.Get("PROPAGATION_B3_MULTIPLE_HEADER")
	b3SingleHeader := env.Get("PROPAGATION_B3_SINGLE_HEADER")

	err := envx.Supply(
		envx.Set(&c.Baggage, envx.Default(d.Baggage, baggage, baggage.Boolean)),
		envx.Set(&c.TraceContext, envx.Default(d.TraceContext, traceContext, traceContext.Boolean)),
		envx.Set(&c.B3MultipleHeader, envx.Default(d.B3MultipleHeader, b3MultipleHeader, b3MultipleHeader.Boolean)),
		envx.Set(&c.B3SingleHeader, envx.Default(d.B3SingleHeader, b3SingleHeader, b3SingleHeader.Boolean)),
	)

	return c, err
}

// samplingConfigFromEnv creates a SamplingConfig by reading the environment
// variable related to sampling configuration.
func samplingConfigFromEnv() (SamplingConfig, error) {
	d := DefaultConfig.Sampling
	c := SamplingConfig{}

	ratio := envx.Coalesce("OTEL_TRACES_SAMPLER_ARG", "TRACING_SAMPLING_RATIO")
	err := envx.Supply(
		envx.Set(&c.Ratio, envx.Default(d.Ratio, ratio, ratio.Float64)),
	)

	return c, err
}

// resourceConfigFromEnv creates a ResourceConfig by reading the environment
// variables related to resource configuration such as service name and attributes
func resourceConfigFromEnv() (ResourceConfig, error) {
	d := DefaultConfig.Resource
	c := ResourceConfig{}

	serviceName := envx.Coalesce("OTEL_SERVICE_NAME", "TRACING_RESOURCE_SERVICE_NAME").Default(c.ServiceName)
	attributes := envx.Coalesce("OTEL_RESOURCE_ATTRIBUTES", "TRACING_RESOURCE_ATTRIBUTES").Expand()
	deploymentEnvironment := env.Get("RESOURCE_DEPLOYMENT_ENVIRONMENT").Default(c.DeploymentEnvironment)
	detectors := env.Get("RESOURCE_DETECTORS")

	err := envx.Supply(
		envx.Set(&c.ServiceName, serviceName.String),
		envx.Set(&c.DeploymentEnvironment, deploymentEnvironment.String),
		envx.Set(&c.Attributes, envx.Default(d.Attributes, attributes, attributes.MapStringString)),
		envx.Set(&c.Detectors, envx.Default(
			d.Detectors,
			detectors,
			func() ([]string, error) { return detectors.StringSlice() },
		)),
	)

	return c, err
}

// securityConfigFromEnv creates a SecurityConfig by reading the environment
// variables related to security settings like authorization headers and
// insecure transport flags.
func securityConfigFromEnv() (SecurityConfig, error) {
	d := DefaultConfig.Security
	c := SecurityConfig{}

	authHeader := env.Get("SECURITY_AUTHORIZATION_HEADER").Default(d.AuthorizationHeader)
	insecure := env.Get("SECURITY_INSECURE")
	err := envx.Supply(
		envx.Set(&c.AuthorizationHeader, authHeader.String),
		envx.Set(&c.Insecure, envx.Default(d.Insecure, insecure, insecure.Boolean)),
	)

	return c, err
}

// communicationConfigFromEnv creates a CommunicationConfig by reading the
// environment variables related to tracing communication settings like
// endpoint and export method.
func communicationConfigFromEnv() (CommunicationConfig, error) {
	d := DefaultConfig.Communication
	c := CommunicationConfig{}

	endpoint := envx.Coalesce(
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"TRACING_COMMUNICATION_ENDPOINT",
	).Default(d.Endpoint)

	exportMethod := envx.Coalesce(
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"TRACING_COMMUNICATION_EXPORT_METHOD",
	).Default(d.ExportMethod)

	err := envx.Supply(
		envx.Set(&c.Endpoint, endpoint.String),
		envx.Set(&c.ExportMethod, exportMethod.String),
	)

	return c, err
}
