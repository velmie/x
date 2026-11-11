package otelx_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestProductionImportsExcludeBrokerAndOutbox(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, "./...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list production imports: %v\n%s", err, output)
	}
	for _, path := range strings.Fields(string(output)) {
		for _, module := range []string{"github.com/velmie/broker", "github.com/velmie/outbox"} {
			if path == module || strings.HasPrefix(path, module+"/") {
				t.Errorf("production import %q crosses telemetry boundary", path)
			}
		}
	}
}
