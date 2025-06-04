package otelx_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/velmie/x/svc/otelx"
)

func TestConfigFromEnv(t *testing.T) {
	type want struct {
		cfg *otelx.Config
		err bool
	}

	tests := []struct {
		name    string
		envVars map[string]string
		want    want
	}{
		{
			name:    "no environment variables",
			envVars: map[string]string{},
			want: want{
				cfg: &otelx.DefaultConfig,
				err: false,
			},
		},
		{
			name: "set all possible values",
			envVars: map[string]string{
				"SOME_TEST_VALUE":                         "123456789",
				"TRACING_DISABLED":                        "true",
				"TRACING_PROPAGATION_BAGGAGE":             "false",
				"TRACING_PROPAGATION_TRACE_CONTEXT":       "false",
				"TRACING_PROPAGATION_B3_MULTIPLE_HEADER":  "true",
				"TRACING_PROPAGATION_B3_SINGLE_HEADER":    "false",
				"TRACING_SAMPLING_RATIO":                  "0.5",
				"TRACING_RESOURCE_ATTRIBUTES":             "customAttr1=value,customAttr2=value2,expand=$SOME_TEST_VALUE",
				"TRACING_RESOURCE_SERVICE_NAME":           "test",
				"TRACING_RESOURCE_DEPLOYMENT_ENVIRONMENT": "development",
				"TRACING_RESOURCE_DETECTORS":              "detector1,ns.detector2",
				"TRACING_SECURITY_AUTHORIZATION_HEADER":   "secret",
				"TRACING_SECURITY_INSECURE":               "false",
				"TRACING_COMMUNICATION_ENDPOINT":          "host:4567",
				"TRACING_COMMUNICATION_EXPORT_METHOD":     "stdout",
			},
			want: want{
				cfg: &otelx.Config{
					Disabled: true,
					Communication: otelx.CommunicationConfig{
						Endpoint:     "host:4567",
						ExportMethod: "stdout",
					},
					Security: otelx.SecurityConfig{
						AuthorizationHeader: "secret",
						Insecure:            false,
					},
					Resource: otelx.ResourceConfig{
						ServiceName:           "test",
						DeploymentEnvironment: "development",
						Attributes: map[string]string{
							"customAttr1": "value",
							"customAttr2": "value2",
							"expand":      "123456789",
						},
						Detectors: []string{"detector1", "ns.detector2"},
					},
					Sampling: otelx.SamplingConfig{
						Ratio: 0.5,
					},
					Propagation: otelx.PropagationConfig{
						Baggage:          false,
						TraceContext:     false,
						B3SingleHeader:   false,
						B3MultipleHeader: true,
					},
				},
				err: false,
			},
		},
		{
			name: "invalid data",
			envVars: map[string]string{
				"TRACING_PROPAGATION_BAGGAGE": "not_expected",
			},
			want: want{
				err: true,
			},
		},
		{
			name: "OTEL standard variables fallback",
			envVars: map[string]string{
				"OTEL_SDK_DISABLED":           "true",
				"OTEL_SERVICE_NAME":           "otel-service",
				"OTEL_RESOURCE_ATTRIBUTES":    "key1=val1,key2=val2",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel-collector:4317",
				"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
				"OTEL_TRACES_SAMPLER_ARG":     "0.75",
			},
			want: want{
				cfg: &otelx.Config{
					Disabled: true,
					Communication: otelx.CommunicationConfig{
						Endpoint:     "http://otel-collector:4317",
						ExportMethod: "grpc",
					},
					Security: otelx.SecurityConfig{
						AuthorizationHeader: "",
						Insecure:            true,
					},
					Resource: otelx.ResourceConfig{
						ServiceName:           "otel-service",
						DeploymentEnvironment: "",
						Attributes: map[string]string{
							"key1": "val1",
							"key2": "val2",
						},
						Detectors: nil,
					},
					Sampling: otelx.SamplingConfig{
						Ratio: 0.75,
					},
					Propagation: otelx.PropagationConfig{
						Baggage:          true,
						TraceContext:     true,
						B3SingleHeader:   true,
						B3MultipleHeader: false,
					},
				},
				err: false,
			},
		},
		{
			name: "OTEL variables override legacy variables",
			envVars: map[string]string{
				// OTEL variables (should take precedence)
				"OTEL_SDK_DISABLED":           "true",
				"OTEL_SERVICE_NAME":           "otel-service",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel:4317",
				// Legacy variables (should be ignored)
				"TRACING_DISABLED":               "false",
				"TRACING_RESOURCE_SERVICE_NAME":  "legacy-service",
				"TRACING_COMMUNICATION_ENDPOINT": "http://legacy:4567",
			},
			want: want{
				cfg: &otelx.Config{
					Disabled: true,
					Communication: otelx.CommunicationConfig{
						Endpoint:     "http://otel:4317",
						ExportMethod: "grpc",
					},
					Security: otelx.SecurityConfig{
						AuthorizationHeader: "",
						Insecure:            true,
					},
					Resource: otelx.ResourceConfig{
						ServiceName:           "otel-service",
						DeploymentEnvironment: "",
						Attributes:            nil,
						Detectors:             nil,
					},
					Sampling: otelx.SamplingConfig{
						Ratio: 1,
					},
					Propagation: otelx.PropagationConfig{
						Baggage:          true,
						TraceContext:     true,
						B3SingleHeader:   true,
						B3MultipleHeader: false,
					},
				},
				err: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			got, err := otelx.ConfigFromEnv()

			for key := range tt.envVars {
				os.Unsetenv(key)
			}

			if (err != nil) != tt.want.err {
				t.Errorf("ConfigFromEnv() error = %+v, wantErr = %v", err, tt.want.err)
				return
			}

			if err == nil && !reflect.DeepEqual(got, tt.want.cfg) {
				t.Errorf("ConfigFromEnv() = %+v, want %+v", got, tt.want.cfg)
			}
		})
	}
}
