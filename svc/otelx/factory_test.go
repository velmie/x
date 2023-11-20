package otelx_test

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"

	. "github.com/velmie/x/svc/otelx"

	"github.com/stretchr/testify/assert"
)

func TestCreateExporter(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		exportMethod string
		wantType     reflect.Type
		wantErr      bool
	}{
		{"HTTP Exporter", ExportMethodHTTP, reflect.TypeOf(&otlptrace.Exporter{}), false},
		{"gRPC Exporter", ExportMethodGRPC, reflect.TypeOf(&otlptrace.Exporter{}), false},
		{"Stdout Exporter", ExportMethodStdout, reflect.TypeOf(&stdouttrace.Exporter{}), false},
		{"Unknown Exporter", "unknown", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Communication: CommunicationConfig{
					ExportMethod: tt.exportMethod,
				},
			}
			exporter, err := CreateExporter(ctx, c)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, exporter)
				assert.Equal(t, tt.wantType, reflect.TypeOf(exporter))
			}
		})
	}
}
