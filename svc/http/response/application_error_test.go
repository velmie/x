package response_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/http/response"
)

func TestFromApplicationErrorCategories(t *testing.T) {
	tests := []struct {
		category       apperror.Category
		status         int
		internal       bool
		defaultMessage string
	}{
		{
			category:       apperror.Internal,
			status:         http.StatusInternalServerError,
			internal:       true,
			defaultMessage: "internal server error",
		},
		{
			category:       apperror.Invalid,
			status:         http.StatusBadRequest,
			defaultMessage: "invalid",
		},
		{
			category:       apperror.Forbidden,
			status:         http.StatusForbidden,
			defaultMessage: "forbidden",
		},
		{
			category:       apperror.NotFound,
			status:         http.StatusNotFound,
			defaultMessage: "not found",
		},
		{
			category:       apperror.Conflict,
			status:         http.StatusConflict,
			defaultMessage: "conflict",
		},
		{
			category:       apperror.Precondition,
			status:         http.StatusConflict,
			defaultMessage: "precondition",
		},
		{
			category:       apperror.Unavailable,
			status:         http.StatusServiceUnavailable,
			defaultMessage: "unavailable",
		},
		{
			category:       255,
			status:         http.StatusInternalServerError,
			internal:       true,
			defaultMessage: "internal server error",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.category.String(),
			func(t *testing.T) {
				err := &apperror.Error{
					Category: tt.category,
					Code:     "PUBLIC_CODE",
					Message:  "public message",
				}
				status, body := response.FromApplicationError(err, response.ErrorOptions{})
				if status != tt.status || len(body.Errors) != 1 || body.Errors[0].Target != response.TargetCommon {
					t.Fatalf("status %d, body %+v", status, body)
				}
				if tt.internal {
					if body.Errors[0].Code != response.ErrCodeInternalServerError || body.Errors[0].Title != "internal server error" {
						t.Fatalf("internal envelope: %+v", body.Errors[0])
					}
				} else if body.Errors[0].Code != "PUBLIC_CODE" || body.Errors[0].Title != "public message" {
					t.Fatalf("public envelope: %+v", body.Errors[0])
				}
				emptyMessage := &apperror.Error{
					Category: tt.category,
					Code:     "PUBLIC_CODE",
				}
				_, body = response.FromApplicationError(emptyMessage, response.ErrorOptions{})
				if got := body.Errors[0].Title; got != tt.defaultMessage {
					t.Fatalf("default title = %q, want %q", got, tt.defaultMessage)
				}
			},
		)
	}
}

func TestFromApplicationErrorTraversal(t *testing.T) {
	left := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "LEFT",
	}
	right := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "RIGHT",
	}
	unknown := &apperror.Error{
		Category: 255,
		Code:     "HIDDEN",
	}
	outer := &apperror.Error{
		Category: apperror.Conflict,
		Code:     "OUTER",
		Cause:    errors.Join(left, right),
	}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "outer",
			err:    outer,
			status: http.StatusConflict,
			code:   "OUTER",
		},
		{
			name:   "left first",
			err:    errors.Join(fmt.Errorf("wrapped: %w", left), right),
			status: http.StatusNotFound,
			code:   "LEFT",
		},
		{
			name:   "reverse",
			err:    errors.Join(right, left),
			status: http.StatusBadRequest,
			code:   "RIGHT",
		},
		{
			name:   "unknown first",
			err:    errors.Join(unknown, right),
			status: 500,
			code:   response.ErrCodeInternalServerError,
		},
		{
			name:   "unclassified",
			err:    errors.New("secret"),
			status: 500,
			code:   response.ErrCodeInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				status, body := response.FromApplicationError(tt.err, response.ErrorOptions{})
				if status != tt.status || len(body.Errors) != 1 || body.Errors[0].Code != tt.code {
					t.Fatalf("status %d, body %+v", status, body)
				}
			},
		)
	}
}

func TestFromApplicationErrorCommonTitle(t *testing.T) {
	err := &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
	}
	_, body := response.FromApplicationError(err, response.ErrorOptions{})
	if body.Errors[0].Title != "not found" {
		t.Fatalf("empty application message gave title %q", body.Errors[0].Title)
	}
	err = &apperror.Error{
		Category: apperror.NotFound,
		Code:     "BOOK_NOT_FOUND",
		Message:  "book not found",
	}
	_, body = response.FromApplicationError(err, response.ErrorOptions{})
	if body.Errors[0].Title != "book not found" {
		t.Fatalf("application message gave title %q", body.Errors[0].Title)
	}
	_, body = response.FromApplicationError(
		err,
		response.ErrorOptions{
			Message: "safe fixed message",
		},
	)
	if body.Errors[0].Title != "safe fixed message" {
		t.Fatalf("override gave title %q", body.Errors[0].Title)
	}
}

func TestFromApplicationErrorFieldEnvelope(t *testing.T) {
	err := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "NOT_IN_ENVELOPE",
		Message:  "not used with fields",
		Violations: []apperror.Violation{
			{
				Field: "lines[0].quantity",
				Rule:  apperror.RuleGTE,
				Param: "1",
			},
			{
				Field:   "name",
				Rule:    apperror.RuleRequired,
				Message: "name is required",
			},
		},
	}
	status, body := response.FromApplicationError(
		err,
		response.ErrorOptions{
			Message: "ignored for fields",
		},
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	if writeErr := response.WriteErrors(recorder, request, status, body); writeErr != nil {
		t.Fatal(writeErr)
	}
	want := `{"errors":[{"code":"GTE","source":"lines[0].quantity","meta":{"gte":"1"},"target":"field"},` +
		`{"code":"REQUIRED","title":"name is required","source":"name","target":"field"}]}` + "\n"
	if status != http.StatusBadRequest || recorder.Body.String() != want {
		t.Fatalf("status %d, body %s", status, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
}

func TestFromApplicationErrorSuppressesInternalData(t *testing.T) {
	for _, category := range []apperror.Category{apperror.Internal, 255} {
		err := &apperror.Error{
			Category: category,
			Code:     "CANARY_CODE",
			Message:  "CANARY_MESSAGE",
			Cause:    errors.New("CANARY_CAUSE"),
			Violations: []apperror.Violation{
				{
					Field:   "CANARY_FIELD",
					Rule:    "CANARY_RULE",
					Param:   "CANARY_PARAM",
					Message: "CANARY_VIOLATION",
				},
			},
		}
		defaultStatus, defaultBody := response.FromApplicationError(err, response.ErrorOptions{})
		defaultJSON, defaultErr := json.Marshal(defaultBody)
		if defaultErr != nil {
			t.Fatal(defaultErr)
		}
		if defaultStatus != 500 || strings.Contains(string(defaultJSON), "CANARY") || string(defaultJSON) !=
			`{"errors":[{"code":"INTERNAL_SERVER_ERROR","title":"internal server error","target":"common"}]}` {
			t.Fatalf("default status %d, body %s", defaultStatus, defaultJSON)
		}
		status, body := response.FromApplicationError(
			err,
			response.ErrorOptions{
				Message: "safe fixed message",
			},
		)
		encoded, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if status != 500 || strings.Contains(string(encoded), "CANARY") || string(encoded) !=
			`{"errors":[{"code":"INTERNAL_SERVER_ERROR","title":"safe fixed message","target":"common"}]}` {
			t.Fatalf("status %d, body %s", status, encoded)
		}
		if !strings.Contains(err.Error(), "CANARY_CAUSE") {
			t.Fatal("original cause lost")
		}
	}
}

func TestWriteErrorsHeadAndFailures(t *testing.T) {
	body := response.Error(&response.HTTPError{
		Code:   "TEST",
		Target: response.TargetCommon,
	})
	recorder := httptest.NewRecorder()
	if err := response.WriteErrors(recorder, httptest.NewRequest(http.MethodHead, "/", nil), 409, body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 409 || recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("HEAD response: %d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
	writeErr := errors.New("write failed")
	writer := &failingWriter{
		err: writeErr,
	}
	if err := response.WriteErrors(writer, httptest.NewRequest(http.MethodGet, "/", nil), 500, body); !errors.Is(err, writeErr) {
		t.Fatalf("write error = %v", err)
	}
	body.Errors[0].Meta = map[string]any{"invalid": make(chan int)}
	if err := response.WriteErrors(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), 500, body); err == nil {
		t.Fatal("expected encoding failure")
	}
}

func TestConcurrentApplicationConversion(t *testing.T) {
	cause := errors.New("diagnostic cause")
	err := &apperror.Error{
		Category: apperror.Invalid,
		Code:     "INVALID",
		Cause:    cause,
		Violations: []apperror.Violation{
			{
				Field: "amount",
				Rule:  apperror.RuleGTE,
				Param: "1",
			},
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, body := response.FromApplicationError(err, response.ErrorOptions{})
			if status != 400 || body.Errors[0].Meta["gte"] != "1" || !errors.Is(err, cause) {
				t.Error("conversion lost data")
			}
		}()
	}
	wg.Wait()
	if err.Violations[0].Param != "1" || err.Cause != cause {
		t.Fatal("conversion mutated original")
	}
}

type failingWriter struct {
	err error
}

func (w *failingWriter) Header() http.Header       { return make(http.Header) }
func (w *failingWriter) WriteHeader(int)           {}
func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }
