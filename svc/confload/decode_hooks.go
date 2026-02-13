package confload

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

var (
	urlType               = reflect.TypeOf(&url.URL{})
	errInvalidAbsoluteURL = errors.New("invalid absolute URL (scheme and host required)")
	errInvalidBool        = errors.New("invalid boolean value")
	errInvalidNumber      = errors.New("invalid number value")
)

func stringToURLHook(_, to reflect.Type, data any) (any, error) {
	if to != urlType {
		return data, nil
	}

	str, ok := data.(string)
	if !ok {
		return data, nil
	}

	str = strings.TrimSpace(str)
	if str == "" {
		return (*url.URL)(nil), nil
	}

	parsed, err := url.Parse(str)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: %q", errInvalidAbsoluteURL, str)
	}

	return parsed, nil
}

func stringToBoolHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String || to.Kind() != reflect.Bool {
		return data, nil
	}

	raw, ok := data.(string)
	if !ok {
		return data, nil
	}

	str := strings.TrimSpace(strings.ToLower(raw))
	switch str {
	case "true", "1", "yes", "y", "on":
		return true, nil
	case "false", "0", "no", "n", "off":
		return false, nil
	}

	return nil, fmt.Errorf("%w: %q", errInvalidBool, raw)
}

func stringToNumberHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String {
		return data, nil
	}

	raw, ok := data.(string)
	if !ok {
		return data, nil
	}
	raw = strings.TrimSpace(raw)

	switch to.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if raw == "" {
			return 0, nil
		}
		val, err := strconv.ParseInt(raw, 10, to.Bits())
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidNumber, raw, err)
		}

		return val, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if raw == "" {
			return 0, nil
		}
		val, err := strconv.ParseUint(raw, 10, to.Bits())
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidNumber, raw, err)
		}

		return val, nil
	}

	return data, nil
}

func stringToFloatHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String {
		return data, nil
	}

	raw, ok := data.(string)
	if !ok {
		return data, nil
	}
	raw = strings.TrimSpace(raw)

	switch to.Kind() {
	case reflect.Float32, reflect.Float64:
		if raw == "" {
			return 0.0, nil
		}
		val, err := strconv.ParseFloat(raw, to.Bits())
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidNumber, raw, err)
		}

		return val, nil
	}

	return data, nil
}
