package confload_test

import (
	"fmt"
	"os"
	"strings"

	"github.com/velmie/x/svc/confload"
)

func ExampleWithSectionEnvPrefix() {
	type telemetry struct {
		Headers map[string]string `k:"headers"`
	}
	type config struct {
		Telemetry telemetry `k:"telemetry"`
	}
	os.Setenv("OTELX_HEADERS_AUTH", "environment")
	defer os.Unsetenv("OTELX_HEADERS_AUTH")
	loader := confload.New(
		confload.WithEnvPrefix("APP"),
		confload.WithSectionEnvPrefix("telemetry", "OTELX_"),
		confload.WithReader("config.json", strings.NewReader(`{"telemetry":{"headers":{"auth":"file"}}}`)),
	)
	cfg, err := confload.LoadInto[config](loader)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Telemetry.Headers["auth"])
	// Output: environment
}
