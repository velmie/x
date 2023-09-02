package response_test

import (
	"encoding/json"
	"testing"

	. "github.com/velmie/x/svc/http/response"
)

func TestError(t *testing.T) {
	testCases := []struct {
		desc     string
		input    []*HTTPError
		expected string
	}{
		{
			desc: "Single error",
			input: []*HTTPError{
				{
					Code:   ErrCodeInvalidRequestParameter,
					Title:  "id is not valid",
					Source: "id",
					Meta:   map[string]any{"format": "[0-9]*"},
					Target: TargetField,
				},
			},
			expected: `{"errors":[{"code":"INVALID_REQUEST_PARAMETER","title":"id is not valid","source":"id","meta":{"format":"[0-9]*"},"target":"field"}]}`,
		},
		{
			desc: "Multiple errors",
			input: []*HTTPError{
				{
					Code:       "404",
					Title:      "Not Found",
					Target:     TargetCommon,
					StatusCode: 404,
				},
				{
					Code:       "400",
					Title:      "Bad Request",
					Source:     "/users",
					Target:     TargetCommon,
					StatusCode: 400,
				},
			},
			expected: `{"errors":[{"code":"404","title":"Not Found","target":"common"},{"code":"400","title":"Bad Request","source":"/users","target":"common"}]}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Generate Errors object
			errors := Error(tc.input...)

			// Marshal to JSON
			actualBytes, err := json.Marshal(errors)
			if err != nil {
				t.Fatalf("Unexpected error while marshaling: %v", err)
			}

			// Convert JSON bytes to string
			actual := string(actualBytes)

			// Compare with the expected output
			if actual != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, actual)
			}
		})
	}
}
