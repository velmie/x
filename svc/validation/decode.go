package validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

var (
	// ErrEmptyInput identifies absent input or input containing only JSON whitespace.
	ErrEmptyInput = errors.New("empty JSON input")
	// ErrInvalidUTF8 identifies invalid UTF-8 bytes in JSON input.
	ErrInvalidUTF8 = errors.New("JSON input is not valid UTF-8")
	// ErrObjectRequired identifies a non-object top-level JSON value.
	ErrObjectRequired = errors.New("JSON object required")
	// ErrTrailingData identifies non-whitespace data after the first JSON object.
	ErrTrailingData = errors.New("unexpected data after JSON object")
)

// DecodeJSON decodes one UTF-8 JSON object into dst and validates it with v.
// It rejects unknown fields and trailing data. A failure may partially populate dst.
func DecodeJSON(data []byte, dst any, v *validator.Validate) error {
	if !utf8.Valid(data) {
		return ErrInvalidUTF8
	}
	trimmed := bytes.Trim(data, " \t\r\n")
	if len(trimmed) == 0 {
		return ErrEmptyInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if trimmed[0] != '{' {
		var value any
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		return ErrObjectRequired
	}
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return errors.Join(ErrTrailingData, err)
		}
		return ErrTrailingData
	}
	return v.Struct(dst)
}
