package mysql_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/velmie/x/svc/sqlconnection/mysql"
)

func TestConfigFromEnv(t *testing.T) {
	envs := []string{
		"DB_HOST",
		"DB_PORT",
		"DB_NAME",
		"DB_USER",
		"DB_PASS",
		"DB_MAX_OPEN_CONNECTIONS",
		"DB_MAX_IDLE_CONNECTIONS",
		"DB_CONNECTION_MAX_LIFETIME",
		"DB_CONNECTION_MAX_IDLE_TIME",
		"DB_UNSAFE_DISABLE_TLS",
		"DB_TLS_CERT_PATH",
	}

	tests := []struct {
		name         string
		envs         map[string]string
		expectsError bool
	}{
		{
			name: "Host is missed",
			envs: map[string]string{
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "Host is invalid",
			envs: map[string]string{
				"DB_HOST":                     "123%$#",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "Port is empty",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "Port is invalid",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "0",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "Port is not int",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "string",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_NAME is empty",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_USER is empty",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		}, {
			name: "DB_PASS is empty",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "",
				"DB_MAX_OPEN_CONNECTIONS":     "10",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_MAX_OPEN_CONNECTIONS is not int",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "string",
				"DB_MAX_IDLE_CONNECTIONS":     "1",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_MAX_IDLE_CONNECTIONS is not int",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "1",
				"DB_MAX_IDLE_CONNECTIONS":     "string",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_CONNECTION_MAX_LIFETIME is not valid duration",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "1",
				"DB_MAX_IDLE_CONNECTIONS":     "string",
				"DB_CONNECTION_MAX_LIFETIME":  "1mm",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_CONNECTION_MAX_IDLE_TIME is not valid duration",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "1",
				"DB_MAX_IDLE_CONNECTIONS":     "string",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10mm",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "DB_UNSAFE_DISABLE_TLS is not valid",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "1",
				"DB_MAX_IDLE_CONNECTIONS":     "string",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true123",
			},
			expectsError: true,
		},
		{
			name: "everything is ok: all vars",
			envs: map[string]string{
				"DB_HOST":                     "localhost",
				"DB_PORT":                     "3306",
				"DB_NAME":                     "db_name",
				"DB_USER":                     "db_user",
				"DB_PASS":                     "db_secret",
				"DB_MAX_OPEN_CONNECTIONS":     "1",
				"DB_MAX_IDLE_CONNECTIONS":     "string",
				"DB_CONNECTION_MAX_LIFETIME":  "1m",
				"DB_CONNECTION_MAX_IDLE_TIME": "10m",
				"DB_UNSAFE_DISABLE_TLS":       "true",
			},
			expectsError: true,
		},
		{
			name: "everything is ok: only required vars",
			envs: map[string]string{
				"DB_HOST":               "localhost",
				"DB_PORT":               "3306",
				"DB_NAME":               "db_name",
				"DB_USER":               "db_user",
				"DB_PASS":               "db_secret",
				"DB_UNSAFE_DISABLE_TLS": "true",
			},
			expectsError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			for _, i2 := range envs {
				_ = os.Unsetenv(i2)
			}

			for k, v := range tt.envs {
				require.NoError(t, os.Setenv(k, v))
			}

			_, err := mysql.ConfigFromEnv("")
			if tt.expectsError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
