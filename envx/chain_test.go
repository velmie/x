package envx_test

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	. "github.com/velmie/x/envx"
)

func Test_Chain(t *testing.T) {
	tests := []struct {
		env         string
		v           string
		expected    interface{}
		skipSetting bool
		err         error
		run         func(env string) (interface{}, error)
	}{
		{
			env:      "JUST_EMPTY_STRING",
			v:        "",
			expected: "",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).String()
			},
		},
		{
			env:      "SOME_PREFIX_PREFIXED_GET",
			v:        "test",
			expected: "test",
			err:      nil,
			run: func(_ string) (interface{}, error) {
				return Prefixed("SOME_PREFIX_").Get("PREFIXED_GET").String()
			},
		},
		{
			env:      "SOME_PREFIX_PREFIXED_COALESCE",
			v:        "test",
			expected: "test",
			err:      nil,
			run: func(_ string) (interface{}, error) {
				return Prefixed("SOME_PREFIX_").Coalesce("VAR1", "PREFIXED_COALESCE").String()
			},
		},
		{
			env:         "REQUIRED_STRING",
			v:           "",
			expected:    "",
			skipSetting: true,
			err:         ErrRequired,
			run: func(env string) (interface{}, error) {
				return Get(env).Required().String()
			},
		},
		{
			env:         "REQUIRED_IF_TRUE",
			v:           "",
			expected:    "",
			skipSetting: true,
			err:         ErrRequired,
			run: func(env string) (interface{}, error) {
				return Get(env).RequiredIf(true).String()
			},
		},
		{
			env:      "DOMAIN_OR_IP1",
			v:        "example.com",
			expected: "example.com",
			run: func(env string) (interface{}, error) {
				return Get(env).Or(DomainName, IPAddress).String()
			},
		},
		{
			env:      "DOMAIN_OR_IP2",
			v:        "192.168.1.1",
			expected: "192.168.1.1",
			run: func(env string) (interface{}, error) {
				return Get(env).Or(DomainName, IPAddress).String()
			},
		},
		{
			env:      "DOMAIN_OR_IP_INVALID",
			v:        "regular string",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).Or(DomainName, IPAddress).String()
			},
		},
		{
			env:         "REQUIRED_IF_FALSE",
			v:           "",
			expected:    "",
			skipSetting: true,
			err:         nil,
			run: func(env string) (interface{}, error) {
				return Get(env).RequiredIf(false).String()
			},
		},
		{
			env:      "EMPTY_STRING",
			v:        "",
			expected: "",
			err:      ErrEmpty,
			run: func(env string) (interface{}, error) {
				return Get(env).NotEmpty().String()
			},
		},
		{
			env:      "NOT_EMPTY_IF_TRUE",
			v:        "",
			expected: "",
			err:      ErrEmpty,
			run: func(env string) (interface{}, error) {
				return Get(env).NotEmptyIf(true).String()
			},
		},
		{
			env:      "NOT_EMPTY_IF_FALSE",
			v:        "",
			expected: "",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).NotEmptyIf(false).String()
			},
		},
		{
			env:      "NOT_EMPTY_STRING",
			v:        "anything",
			expected: "anything",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).NotEmpty().String()
			},
		},
		{
			env:         "DEFAULT_WHEN_NOT_SET",
			v:           "",
			expected:    "default value",
			err:         nil,
			skipSetting: true,
			run: func(env string) (interface{}, error) {
				return Get(env).Default("default value").String()
			},
		},
		{
			env:      "DEFAULT_WHEN_SET_EMPTY",
			v:        "",
			expected: "",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Default("default value").String()
			},
		},
		{
			env:      "VALID_TRUE",
			v:        "true",
			expected: true,
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Boolean()
			},
		},
		{
			env:      "INVALID_BOOLEAN",
			v:        "not a boolean",
			expected: false,
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).Boolean()
			},
		},
		{
			env:      "EMPTY_AS_FALSE",
			v:        "",
			expected: false,
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Boolean()
			},
		},
		{
			env:      "VALID_FALSE",
			v:        "false",
			expected: false,
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Boolean()
			},
		},
		{
			env:      "VALID_INT",
			v:        "123",
			expected: 123,
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Int()
			},
		},
		{
			env:      "INVALID_INT",
			v:        "123!",
			expected: 0,
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).Int()
			},
		},
		{
			env:      "VALID_INT64",
			v:        "123",
			expected: int64(123),
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Int64()
			},
		},
		{
			env:      "INVALID_INT64",
			v:        "123!",
			expected: int64(0),
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).Int64()
			},
		},
		{
			env:      "VALID_DURATION",
			v:        "5m45s",
			expected: 5*time.Minute + 45*time.Second,
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).Duration()
			},
		},
		{
			env:      "INVALID_DURATION",
			v:        "999",
			expected: time.Duration(0),
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).Duration()
			},
		},
		{
			env:      "VALID_URL",
			v:        "tcp://192.168.0.1:19878",
			expected: mustParseURL("tcp://192.168.0.1:19878"),
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).URL()
			},
		},
		{
			env:      "INVALID_URL",
			v:        "192.168.0.1",
			expected: (*url.URL)(nil),
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).URL()
			},
		},
		{
			env:      "VALID_PORT_NUMBER",
			v:        "8080",
			expected: "8080",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidPortNumber().String()
			},
		},
		{
			env:      "INVALID_PORT_NUMBER_OUT_OF_RANGE",
			v:        "99999",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidPortNumber().String()
			},
		},
		{
			env:      "INVALID_PORT_NUMBER_NAN",
			v:        "not_a_number",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidPortNumber().String()
			},
		},
		{
			env:      "VALID_IPV4",
			v:        "192.168.0.1",
			expected: "192.168.0.1",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidIPAddress().String()
			},
		},
		{
			env:      "VALID_IPV6",
			v:        "2001:0000:130F:0000:0000:09C0:876A:130B",
			expected: "2001:0000:130F:0000:0000:09C0:876A:130B",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidIPAddress().String()
			},
		},
		{
			env:      "VALID_DOMAIN_NAME",
			v:        "example.com",
			expected: "example.com",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidDomainName().String()
			},
		},
		{
			env:      "VALID_LISTEN_ADDRESS",
			v:        "example.com:8080",
			expected: "example.com:8080",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidListenAddress().String()
			},
		},
		{
			env:      "VALID_LISTEN_ADDRESS_IP",
			v:        "192.168.1.1:8080",
			expected: "192.168.1.1:8080",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidListenAddress().String()
			},
		},
		{
			env:      "VALID_LISTEN_ADDRESS_NO_HOST",
			v:        ":8080",
			expected: ":8080",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidListenAddress().String()
			},
		},
		{
			env:      "INVALID_LISTEN_ADDRESS_MISSING_PORT",
			v:        "192.168.1.1",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidListenAddress().String()
			},
		},
		{
			env:      "VALID_DOMAIN_NAME_LOCALHOST",
			v:        "localhost",
			expected: "localhost",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidDomainName().String()
			},
		},
		{
			env:      "INVALID_DOMAIN_NAME_EMPTY_LABEL",
			v:        "..example.com",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidDomainName().String()
			},
		},
		{
			env:      "INVALID_DOMAIN_NAME_TOO_LONG_LABEL",
			v:        "abcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabc.example.com",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidDomainName().String()
			},
		},
		{
			env:      "INVALID_DOMAIN_NAME_INVALID_CHARS",
			v:        "@#$%.example.com",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidDomainName().String()
			},
		},
		{
			env:      "INVALID_IP",
			v:        "999.99.99.99",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).ValidIPAddress().String()
			},
		},
		{
			env:         "WITH_RUNNERS",
			v:           "test-valuye",
			expected:    "test-value",
			skipSetting: true,
			run: func(env string) (interface{}, error) {
				return Get(env).WithRunners(DefaultVal("test-value")).String()
			},
		},
		{
			env:      "EXPAND_VAR",
			v:        "Hello ${VAR}!",
			expected: "Hello World!",
			err:      nil,
			run: func(env string) (interface{}, error) {
				_ = os.Setenv("VAR", "World")
				return Get(env).Expand().String()
			},
		},
		{
			env:      "ONEOF_INVALID",
			v:        "not-expected",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).OneOf("v1", "v2").String()
			},
		},
		{
			env:      "ONEOF_VALID_V1",
			v:        "v1",
			expected: "v1",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).OneOf("v1", "v2").String()
			},
		},
		{
			env:      "ONEOF_VALID_V2",
			v:        "v2",
			expected: "v2",
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).OneOf("v1", "v2").String()
			},
		},
		{
			env:      "STR_SLICE",
			v:        "val1,val2,val3,etc,etc,",
			expected: []string{"val1", "val2", "val3", "etc", "etc", ""},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).StringSlice()
			},
		},
		{
			env:      "STR_SLICE_CUSTOM_DELIMITER",
			v:        "v1with, comma;val2;val3",
			expected: []string{"v1with, comma", "val2", "val3"},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).StringSlice(";")
			},
		},
		{
			env:      "STR_SLICE_EMPTY",
			v:        "",
			expected: []string{},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).StringSlice()
			},
		},
		{
			env:      "STR_SLICE_RUNNER_ERR",
			v:        "anything",
			expected: []string(nil),
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).OneOf("nothing").StringSlice()
			},
		},
		{
			env:      "STR_SLICE_UNIQUE",
			v:        "v1,v2,v1,v3,v2,v4",
			expected: []string{"v1", "v2", "v3", "v4"},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).UniqueStringSlice()
			},
		},
		{
			env:      "STR_SLICE_UNIQUE2",
			v:        "v1",
			expected: []string{"v1"},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).UniqueStringSlice()
			},
		},
		{
			env:      "STR_SLICE_UNIQUE_CUSTOM_DELIMITER",
			v:        "v1;v2;v3;v1;v4;",
			expected: []string{"v1", "v2", "v3", "v4", ""},
			err:      nil,
			run: func(env string) (interface{}, error) {
				return Get(env).UniqueStringSlice(";")
			},
		},
		{
			env:      "REGEXP_NOT_MATCH",
			v:        "Hello",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).
					MatchRegexp(regexp.MustCompile("World")).
					String()
			},
		},
		{
			env:      "REGEXP_MATCH",
			v:        "Hello",
			expected: "",
			err:      ErrInvalidValue,
			run: func(env string) (interface{}, error) {
				return Get(env).
					MatchRegexp(regexp.MustCompile("/.*/")).
					String()
			},
		},
	}

	os.Clearenv()
	for i, test := range tests {
		t.Run(test.env, func(t *testing.T) {
			meta := fmt.Sprintf("test #%d  %s=%s", i, test.env, test.v)
			if !test.skipSetting {
				_ = os.Setenv(test.env, test.v)
			}
			result, err := test.run(test.env)
			if err != nil {
				meta += " error: " + err.Error()
			}
			require.True(t, errors.Is(err, test.err), meta)
			require.Equal(t, test.expected, result, meta)
		})
	}
}

func Test_Coalesce(t *testing.T) {
	tests := []struct {
		name      string
		envSetup  map[string]string
		input     []string
		wantValue string
	}{
		{
			name: "First variable set",
			envSetup: map[string]string{
				"VAR1": "value1",
			},
			input:     []string{"VAR1", "VAR2", "VAR3"},
			wantValue: "value1",
		},
		{
			name: "Second variable set",
			envSetup: map[string]string{
				"VAR2": "value2",
			},
			input:     []string{"VAR1", "VAR2", "VAR3"},
			wantValue: "value2",
		},
		{
			name:      "No variables set",
			envSetup:  map[string]string{},
			input:     []string{"VAR1", "VAR2", "VAR3"},
			wantValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, val := range tt.envSetup {
				os.Setenv(key, val)
			}

			got, _ := Coalesce(tt.input...).String()

			if got != tt.wantValue {
				t.Errorf("Expected value %s, but got %s", tt.wantValue, got)
			}

			for key := range tt.envSetup {
				os.Unsetenv(key)
			}
		})
	}
}

func mustParseURL(v string) *url.URL {
	result, err := url.Parse(v)
	if err != nil {
		panic("failed to parse url " + err.Error())
	}
	return result
}
