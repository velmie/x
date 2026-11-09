package mysql_test

import (
	"crypto/tls"
	"errors"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/validation"

	"github.com/velmie/x/svc/sqlconnection/mysql"
)

func TestValidateReportsConfigFieldNames(t *testing.T) {
	cfg := mysql.Config{
		Port: 65536,
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "port: lte=65535") || !strings.Contains(err.Error(), "host: required") {
		t.Fatalf("missing configuration field diagnostics: %v", err)
	}
}

func TestValidateRetainsFieldFailuresAndTLSCause(t *testing.T) {
	cfg := mysql.Config{
		Port: 65536,
	}
	err := cfg.Validate()
	var failure *validation.Error
	var native validator.ValidationErrors
	if !errors.As(err, &failure) || !errors.As(err, &native) {
		t.Fatalf("missing typed validation error or cause: %v", err)
	}
	cfg.UnsafeDisableTLS = true
	cfg.TLSCertPath = "ca.pem"
	cfg.TLSServerName = "db.example.test"
	cfg.TLSConfig = &tls.Config{}
	err = cfg.Validate()
	if !errors.As(err, &failure) || failure.Cause == nil || !strings.Contains(err.Error(), failure.Cause.Error()) {
		t.Fatalf("missing TLS cause: %v", err)
	}
	want := []string{"unsafe_disable_tls", "tls_cert_path", "tls_server_name"}
	if len(failure.Violations) != len(want) {
		t.Fatalf("violations: %+v", failure.Violations)
	}
	for i, field := range want {
		if failure.Violations[i].Field != field || failure.Violations[i].Rule != apperror.RuleEQ {
			t.Fatalf("violation: %+v", failure.Violations[i])
		}
	}
}
