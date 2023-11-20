package otelx

const (
	// ExportMethodHTTP indicates that the tracer should use an HTTP client for exporting traces.
	ExportMethodHTTP = "http"

	// ExportMethodGRPC indicates that the tracer should use a gRPC client for exporting traces.
	ExportMethodGRPC = "grpc"

	// ExportMethodStdout indicates that the tracer should output data to the standard output (stdout).
	// Useful for debugging purpose.
	ExportMethodStdout = "stdout"
)

var DefaultConfig = Config{
	Communication: CommunicationConfig{
		ExportMethod: ExportMethodGRPC,
	},
	Security: SecurityConfig{
		Insecure: true,
	},
	Resource: ResourceConfig{},
	Sampling: SamplingConfig{
		Ratio: 1,
	},
	Propagation: PropagationConfig{
		Baggage:          true,
		TraceContext:     true,
		B3SingleHeader:   true,
		B3MultipleHeader: false,
	},
}

// CommunicationConfig holds settings related to the communication method and protocol for tracing data.
type CommunicationConfig struct {
	// Endpoint specifies the URL where the tracer will send the tracing data.
	Endpoint string `json:"endpoint" mapstructure:"endpoint"`
	// ExportMethod defines the protocol used to send the tracing data (e.g., 'http' or 'grpc').
	ExportMethod string `json:"exportMethod" mapstructure:"exportMethod"`
}

// SecurityConfig groups settings related to security and authentication for the tracer.
type SecurityConfig struct {
	// AuthorizationHeader is the header used for authenticating requests made by the tracer.
	AuthorizationHeader string `json:"authorizationHeader" mapstructure:"authorizationHeader"`
	// Insecure, when set to true, disables TLS for the communication.
	// Useful for debugging or environments where security is handled elsewhere.
	Insecure bool `json:"insecure" mapstructure:"insecure"`
}

// ResourceConfig contains settings related to the service resource information used by the tracer.
type ResourceConfig struct {
	// ServiceName is the name of the service attached to all spans created by the tracer.
	ServiceName string `json:"serviceName" mapstructure:"serviceName"`
	// DeploymentEnvironment specifies the environment where the service is running (e.g., 'staging', 'production').
	DeploymentEnvironment string `json:"deploymentEnvironment" mapstructure:"deploymentEnvironment"`
	// Attributes is a collection of key-value pairs providing additional
	// information about the service (e.g., version, hostname).
	Attributes map[string]string `json:"attributes" mapstructure:"attributes"`
	// Detectors is a list of detector names
	Detectors []string `json:"detectors" mapstructure:"detectors"`
}

// SamplingConfig deals with settings related to trace sampling.
type SamplingConfig struct {
	// Ratio determines the fraction of traces that will be collected.
	// A ratio of 1 means all traces are collected, while 0 means none.
	Ratio float64 `json:"ratio" mapstructure:"ratio"`
}

// PropagationConfig contains settings related to trace context propagation.
type PropagationConfig struct {
	// Baggage, when true, enables the propagation of OpenTelemetry baggage items.
	Baggage bool `json:"baggage" mapstructure:"baggage"`
	// TraceContext enables the W3C TraceContext propagation format.
	TraceContext bool `json:"traceContext" mapstructure:"traceContext"`
	// B3SingleHeader enables B3 single header propagation format. Used for compatibility with Zipkin.
	B3SingleHeader bool `json:"b3SingleHeader" mapstructure:"b3SingleHeader"`
	// B3MultipleHeader enables B3 multiple headers propagation format.
	// Also for compatibility with Zipkin-like systems.
	B3MultipleHeader bool `json:"b3MultipleHeader" mapstructure:"b3MultipleHeader"`
}

// Config aggregates all the configuration sub-structures.
type Config struct {
	Disabled      bool                `json:"disabled" mapstructure:"disabled"`
	Communication CommunicationConfig `json:"communication" mapstructure:"communication"`
	Security      SecurityConfig      `json:"security" mapstructure:"security"`
	Resource      ResourceConfig      `json:"resource" mapstructure:"resource"`
	Sampling      SamplingConfig      `json:"sampling" mapstructure:"sampling"`
	Propagation   PropagationConfig   `json:"propagation" mapstructure:"propagation"`
}
