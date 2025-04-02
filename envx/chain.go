package envx

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Variable struct {
	Name     string
	Val      string
	Exist    bool
	AllNames []string // List of all variable names that were tried during lookup

	runners []Runner
}

type Prefixed string

func (p Prefixed) Get(name string) *Variable {
	v, _ := DefaultResolver.Get(string(p) + name)
	// Adjust the AllNames field to include both the prefixed form (which we already got)
	// and the unprefixed form (which we may want to include for clarity)
	v.AllNames = []string{string(p) + name, name}
	return v
}

func (p Prefixed) Coalesce(name ...string) *Variable {
	if len(name) == 0 {
		return &Variable{}
	}

	// Create a list of prefixed names to try
	prefixedNames := make([]string, len(name))
	allNames := make([]string, 0, len(name)*2)

	for i, n := range name {
		prefixedName := string(p) + n
		prefixedNames[i] = prefixedName
		allNames = append(allNames, prefixedName, n)
	}

	// Try the prefixed names using DefaultResolver
	v, _ := DefaultResolver.Coalesce(prefixedNames...)

	// Preserve all the names we conceptually tried (both prefixed and unprefixed)
	v.AllNames = allNames

	// Set primary name (for error messages) to the first name in the list
	v.Name = name[0]

	return v
}

func (v *Variable) Default(val string) *Variable {
	v.runners = append(v.runners, DefaultVal(val))
	return v
}

func (v *Variable) ExactLength(val int) *Variable {
	v.runners = append(v.runners, ExactLength(val))
	return v
}

func (v *Variable) MinLength(min int) *Variable {
	v.runners = append(v.runners, MinLength(min))
	return v
}

func (v *Variable) MaxLength(max int) *Variable {
	v.runners = append(v.runners, MaxLength(max))
	return v
}

// Min applies minimum value validation based on the underlying type.
// This is a universal method that replaces type-specific min methods.
func (v *Variable) Min(minVal interface{}) *Variable {
	switch m := minVal.(type) {
	case int:
		v.runners = append(v.runners, MinInt(int64(m)))
	case int8:
		v.runners = append(v.runners, MinInt(int64(m)))
	case int16:
		v.runners = append(v.runners, MinInt(int64(m)))
	case int32:
		v.runners = append(v.runners, MinInt(int64(m)))
	case int64:
		v.runners = append(v.runners, MinInt(m))
	case uint:
		v.runners = append(v.runners, MinUint(uint64(m)))
	case uint8:
		v.runners = append(v.runners, MinUint(uint64(m)))
	case uint16:
		v.runners = append(v.runners, MinUint(uint64(m)))
	case uint32:
		v.runners = append(v.runners, MinUint(uint64(m)))
	case uint64:
		v.runners = append(v.runners, MinUint(m))
	case float32:
		v.runners = append(v.runners, MinFloat(float64(m)))
	case float64:
		v.runners = append(v.runners, MinFloat(m))
	}
	return v
}

// Max applies maximum value validation based on the underlying type.
// This is a universal method that replaces type-specific max methods.
func (v *Variable) Max(maxVal interface{}) *Variable {
	switch m := maxVal.(type) {
	case int:
		v.runners = append(v.runners, MaxInt(int64(m)))
	case int8:
		v.runners = append(v.runners, MaxInt(int64(m)))
	case int16:
		v.runners = append(v.runners, MaxInt(int64(m)))
	case int32:
		v.runners = append(v.runners, MaxInt(int64(m)))
	case int64:
		v.runners = append(v.runners, MaxInt(m))
	case uint:
		v.runners = append(v.runners, MaxUint(uint64(m)))
	case uint8:
		v.runners = append(v.runners, MaxUint(uint64(m)))
	case uint16:
		v.runners = append(v.runners, MaxUint(uint64(m)))
	case uint32:
		v.runners = append(v.runners, MaxUint(uint64(m)))
	case uint64:
		v.runners = append(v.runners, MaxUint(m))
	case float32:
		v.runners = append(v.runners, MaxFloat(float64(m)))
	case float64:
		v.runners = append(v.runners, MaxFloat(m))
	}
	return v
}

// Range applies minimum and maximum value validation based on the underlying type.
// This is a universal method that replaces type-specific range methods.
func (v *Variable) Range(minVal, maxVal interface{}) *Variable {
	v.Min(minVal)
	v.Max(maxVal)
	return v
}

// Type-specific range methods for internal use

func (v *Variable) MinInt(mn int64) *Variable {
	v.runners = append(v.runners, MinInt(mn))
	return v
}

func (v *Variable) MaxInt(mx int64) *Variable {
	v.runners = append(v.runners, MaxInt(mx))
	return v
}

func (v *Variable) IntRange(mn, mx int64) *Variable {
	v.runners = append(v.runners, MinInt(mn), MaxInt(mx))
	return v
}

func (v *Variable) MinUint(mn uint64) *Variable {
	v.runners = append(v.runners, MinUint(mn))
	return v
}

func (v *Variable) MaxUint(mx uint64) *Variable {
	v.runners = append(v.runners, MaxUint(mx))
	return v
}

func (v *Variable) UintRange(mn, mx uint64) *Variable {
	v.runners = append(v.runners, MinUint(mn), MaxUint(mx))
	return v
}

func (v *Variable) MinFloat(mn float64) *Variable {
	v.runners = append(v.runners, MinFloat(mn))
	return v
}

func (v *Variable) MaxFloat(mx float64) *Variable {
	v.runners = append(v.runners, MaxFloat(mx))
	return v
}

func (v *Variable) FloatRange(mn, mx float64) *Variable {
	v.runners = append(v.runners, MinFloat(mn), MaxFloat(mx))
	return v
}

func (v *Variable) WithRunners(runners ...Runner) *Variable {
	v.runners = append(v.runners, runners...)
	return v
}

func (v *Variable) Required() *Variable {
	v.runners = append(v.runners, Required)
	return v
}

func (v *Variable) RequiredIf(cond bool) *Variable {
	if cond {
		v.runners = append(v.runners, Required)
	}
	return v
}

func (v *Variable) MatchRegexp(expr *regexp.Regexp) *Variable {
	v.runners = append(v.runners, MatchRegexp(expr))
	return v
}

func (v *Variable) NotEmpty() *Variable {
	v.runners = append(v.runners, NotEmpty)
	return v
}

func (v *Variable) NotEmptyIf(cond bool) *Variable {
	if cond {
		v.runners = append(v.runners, NotEmpty)
	}
	return v
}

func (v *Variable) ValidIPAddress() *Variable {
	v.runners = append(v.runners, IPAddress)
	return v
}

func (v *Variable) ValidPortNumber() *Variable {
	v.runners = append(v.runners, PortNumber)
	return v
}

func (v *Variable) ValidDomainName() *Variable {
	v.runners = append(v.runners, DomainName)
	return v
}

func (v *Variable) ValidListenAddress() *Variable {
	v.runners = append(v.runners, ListenAddress)
	return v
}

func (v *Variable) ValidURL() *Variable {
	v.runners = append(v.runners, URL)
	return v
}

func (v *Variable) OneOf(values ...string) *Variable {
	v.runners = append(v.runners, OneOf(values))
	return v
}

func (v *Variable) Expand() *Variable {
	v.runners = append(v.runners, Expand)
	return v
}

func (v *Variable) Or(c1, c2 Runner) *Variable {
	v.runners = append(v.runners, OR(c1, c2))
	return v
}

func (v *Variable) String() (string, error) {
	if err := doRun(v.runners, v); err != nil {
		return "", err
	}
	return v.Val, nil
}

func (v *Variable) StringSlice(delimiter ...string) ([]string, error) {
	delim := ","
	if len(delimiter) > 0 {
		delim = delimiter[0]
	}
	if err := doRun(v.runners, v); err != nil {
		return nil, err
	}
	if v.Val == "" {
		return []string{}, nil
	}
	return strings.Split(v.Val, delim), nil
}

func (v *Variable) MapStringString() (map[string]string, error) {
	const (
		pairSep = ","
		kvSep   = "="
	)
	if err := doRun(v.runners, v); err != nil {
		return nil, err
	}
	if v.Val == "" {
		return map[string]string{}, nil
	}
	result := make(map[string]string)
	pairs := strings.Split(v.Val, pairSep)

	for _, pair := range pairs {
		kv := strings.SplitN(strings.TrimSpace(pair), kvSep, 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			result[key] = value
		}
	}

	return result, nil
}

func (v *Variable) UniqueStringSlice(delimiter ...string) ([]string, error) {
	result, err := v.StringSlice(delimiter...)
	if err != nil {
		return result, err
	}
	//nolint:gomnd // if length 0 or 1 then slice contains only unique values
	if len(result) < 2 {
		return result, nil
	}
	set := map[string]struct{}{}
	unique := make([]string, 0, len(result))
	for _, val := range result {
		if _, ok := set[val]; ok {
			continue
		}
		set[val] = struct{}{}
		unique = append(unique, val)
	}
	return unique, nil
}

func (v *Variable) Boolean() (bool, error) {
	if err := doRun(v.runners, v); err != nil {
		return false, err
	}
	if v.Val == "" {
		return false, nil
	}
	result, err := strconv.ParseBool(v.Val)
	if err != nil {
		return false, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid boolean value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) Duration() (time.Duration, error) {
	if err := doRun(v.runners, v); err != nil {
		return 0, err
	}
	if v.Val == "" {
		return 0, nil
	}
	result, err := time.ParseDuration(v.Val)
	if err != nil {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid time duration value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) Int() (int, error) {
	result, err := v.Int64()
	return int(result), err
}

func (v *Variable) Int64() (int64, error) {
	if err := doRun(v.runners, v); err != nil {
		return 0, err
	}
	if v.Val == "" {
		return 0, nil
	}
	result, err := strconv.ParseInt(v.Val, 10, 64)
	if err != nil {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid integer value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) Float64() (float64, error) {
	if err := doRun(v.runners, v); err != nil {
		return 0, err
	}
	if v.Val == "" {
		return 0, nil
	}
	result, err := strconv.ParseFloat(v.Val, 64)
	if err != nil {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid float value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) Float32() (float32, error) {
	result, err := v.Float64()
	if err != nil {
		return 0, err
	}
	return float32(result), nil
}

func (v *Variable) Uint() (uint, error) {
	result, err := v.Uint64()
	if err != nil {
		return 0, err
	}
	return uint(result), nil
}

func (v *Variable) Uint8() (uint8, error) {
	result, err := v.Uint64()
	if err != nil {
		return 0, err
	}
	if result > 255 {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("value %d exceeds uint8 maximum (255)", result),
			Cause:   ErrInvalidValue,
		}
	}
	return uint8(result), nil
}

func (v *Variable) Uint16() (uint16, error) {
	result, err := v.Uint64()
	if err != nil {
		return 0, err
	}
	if result > 65535 {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("value %d exceeds uint16 maximum (65535)", result),
			Cause:   ErrInvalidValue,
		}
	}
	return uint16(result), nil
}

func (v *Variable) Uint32() (uint32, error) {
	result, err := v.Uint64()
	if err != nil {
		return 0, err
	}
	if result > 4294967295 {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("value %d exceeds uint32 maximum (4294967295)", result),
			Cause:   ErrInvalidValue,
		}
	}
	return uint32(result), nil
}

func (v *Variable) Uint64() (uint64, error) {
	if err := doRun(v.runners, v); err != nil {
		return 0, err
	}
	if v.Val == "" {
		return 0, nil
	}
	result, err := strconv.ParseUint(v.Val, 10, 64)
	if err != nil {
		return 0, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid unsigned integer value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) Time(layout string) (time.Time, error) {
	if err := doRun(v.runners, v); err != nil {
		return time.Time{}, err
	}
	if v.Val == "" {
		return time.Time{}, nil
	}
	result, err := time.Parse(layout, v.Val)
	if err != nil {
		return time.Time{}, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid time in format '%s', got '%s'", layout, v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

func (v *Variable) URL() (*url.URL, error) {
	if err := doRun(v.runners, v); err != nil {
		return nil, err
	}
	if v.Val == "" {
		return &url.URL{}, nil
	}
	result, err := url.ParseRequestURI(v.Val)
	if err != nil {
		return nil, Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("must be a valid URL value, got '%s'", v.Val),
			Cause:   ErrInvalidValue,
		}
	}
	return result, nil
}

// Each converts a variable into a list of variables where each list item is obtained by splitting the original value
// by a delimiter.
// By default, the delimiter is a comma ",", but it accepts any string as a delimiter.
// Converting to a list of variables can be useful if there is a need to validate each item independently.
func (v *Variable) Each(delimiter ...string) Variables {
	delim := ","
	if len(delimiter) > 0 {
		delim = delimiter[0]
	}
	values := strings.Split(v.Val, delim)
	vars := make(Variables, len(values))
	for i, val := range values {
		runners := make([]Runner, len(v.runners))
		copy(runners, v.runners)
		// Copy AllNames slice to preserve all attempted variable names
		allNames := make([]string, len(v.AllNames))
		copy(allNames, v.AllNames)
		vars[i] = &Variable{
			Name:     v.Name,
			Val:      val,
			Exist:    v.Exist,
			AllNames: allNames,
			runners:  runners,
		}
	}
	return vars
}

type Runner func(f *Variable) error

func DefaultVal(val string) Runner {
	return func(f *Variable) error {
		if !f.Exist {
			f.Val = val
		}
		return nil
	}
}

func MatchRegexp(expr *regexp.Regexp) Runner {
	return func(f *Variable) error {
		if !expr.MatchString(f.Val) {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("value '%s' does not match regular expression '%s'", f.Val, expr.String()),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func Required(v *Variable) error {
	if !v.Exist {
		// Use the first name (highest priority) for the error message
		varName := v.Name
		if len(v.AllNames) > 0 {
			varName = v.AllNames[0]
		}

		return Error{
			VarName: varName,
			Reason:  "is not set",
			Cause:   ErrRequired,
		}
	}
	return nil
}

func Expand(v *Variable) error {
	v.Val = os.ExpandEnv(v.Val)
	return nil
}

func NotEmpty(v *Variable) error {
	if v.Val == "" {
		return Error{
			VarName: v.Name,
			Reason:  "has empty value",
			Cause:   ErrEmpty,
		}
	}
	return nil
}

func validateIPAddress(ip string) error {
	if net.ParseIP(ip) != nil {
		return nil
	}
	return errors.New("not valid IP address")
}

func IPAddress(v *Variable) error {
	if v.Val == "" {
		return nil
	}

	if err := validateIPAddress(v.Val); err != nil {
		return Error{
			VarName: v.Name,
			Reason:  err.Error(),
			Cause:   ErrInvalidValue,
		}
	}
	return nil
}

func validatePortNumber(port string) error {
	val, err := strconv.ParseInt(port, 10, 32)
	if err != nil {
		return errors.New("not valid number")
	}
	if val < 1 || val > 65535 {
		return errors.New("out of port range")
	}
	return nil
}

func PortNumber(v *Variable) error {
	if v.Val == "" {
		return nil
	}

	if err := validatePortNumber(v.Val); err != nil {
		return Error{
			VarName: v.Name,
			Reason:  err.Error(),
			Cause:   ErrInvalidValue,
		}
	}
	return nil
}

func URL(v *Variable) error {
	if v.Val == "" {
		return nil
	}
	if _, err := url.ParseRequestURI(v.Val); err != nil {
		return Error{
			VarName: v.Name,
			Reason:  "must be a valid URL value, got '" + v.Val + "'",
			Cause:   ErrInvalidValue,
		}
	}
	return nil
}

func OneOf(values []string) Runner {
	return func(v *Variable) error {
		for _, value := range values {
			if value == v.Val {
				return nil
			}
		}
		return Error{
			VarName: v.Name,
			Reason: fmt.Sprintf(
				"must be one of the following values '%s'; got '%s'",
				strings.Join(values, "', '"),
				v.Val,
			),
			Cause: ErrInvalidValue,
		}
	}
}

func OR(c1, c2 Runner) Runner {
	return func(v *Variable) error {
		if err := c1(v); err == nil {
			return nil

		}
		return c2(v)
	}
}

func validateDomainName(host string) error {
	if len(host) > 253 {
		return errors.New("host Name must not exceed 253 characters length")
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 {
			return errors.New("label cannot be empty and must not exceed 63 characters length")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return errors.New("label cannot start or end with the '-' character")
		}
		onlyDigits := true
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return errors.New("label contains invalid characters")
			}
			if !(r >= '0' && r <= '9') {
				onlyDigits = false
			}
		}
		if onlyDigits && len(labels) > 1 {
			return errors.New("domain cannot contain only digits")
		}
	}
	return nil
}

func DomainName(v *Variable) error {
	if v.Val == "" {
		return nil
	}

	if err := validateDomainName(v.Val); err != nil {
		return Error{
			VarName: v.Name,
			Reason:  err.Error(),
			Cause:   ErrInvalidValue,
		}
	}
	return nil
}

func ListenAddress(v *Variable) error {
	if v.Val == "" {
		return nil
	}
	address := v.Val
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			return Error{
				VarName: v.Name,
				Reason:  "is not a valid listen address: missing port",
				Cause:   ErrInvalidValue,
			}
		} else {
			return Error{
				VarName: v.Name,
				Reason:  fmt.Sprintf("is not a valid listen address: %s", err.Error()),
				Cause:   ErrInvalidValue,
			}
		}
	}

	if host != "" && validateIPAddress(host) != nil && validateDomainName(host) != nil {
		return Error{
			VarName: v.Name,
			Reason:  fmt.Sprintf("%q is not valid host", host),
			Cause:   ErrInvalidValue,
		}
	}

	if err = validatePortNumber(port); err != nil {
		return Error{
			VarName: v.Name,
			Reason:  err.Error(),
			Cause:   ErrInvalidValue,
		}
	}

	return nil
}

func ExactLength(val int) Runner {
	return func(f *Variable) error {
		if len(f.Val) != val {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be %d characters long", val),
				Cause:   ErrInvalidValue,
			}
		}

		return nil
	}
}

func MinLength(min int) Runner {
	return func(f *Variable) error {
		if len(f.Val) < min {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be at least %d characters long", min),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MaxLength(max int) Runner {
	return func(f *Variable) error {
		if len(f.Val) > max {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be no more than %d characters long", max),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MinInt(min int64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseInt(f.Val, 10, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid integer value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val < min {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be greater than or equal to %d", min),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MaxInt(max int64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseInt(f.Val, 10, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid integer value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val > max {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be less than or equal to %d", max),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MinUint(min uint64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseUint(f.Val, 10, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid unsigned integer value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val < min {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be greater than or equal to %d", min),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MaxUint(max uint64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseUint(f.Val, 10, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid unsigned integer value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val > max {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be less than or equal to %d", max),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MinFloat(min float64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseFloat(f.Val, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid float value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val < min {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be greater than or equal to %f", min),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func MaxFloat(max float64) Runner {
	return func(f *Variable) error {
		if f.Val == "" {
			return nil
		}

		val, err := strconv.ParseFloat(f.Val, 64)
		if err != nil {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be a valid float value, got '%s'", f.Val),
				Cause:   ErrInvalidValue,
			}
		}

		if val > max {
			return Error{
				VarName: f.Name,
				Reason:  fmt.Sprintf("must be less than or equal to %f", max),
				Cause:   ErrInvalidValue,
			}
		}
		return nil
	}
}

func doRun(runners []Runner, v *Variable) error {
	for _, c := range runners {
		if err := c(v); err != nil {
			return err
		}
	}
	return nil
}
