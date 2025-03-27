package envx

import (
	"net/url"
	"regexp"
	"time"
)

type Variables []*Variable

func (v Variables) ValidIPAddress() Variables {
	v.appendRunners(IPAddress)
	return v
}

func (v Variables) ValidPortNumber() Variables {
	v.appendRunners(PortNumber)
	return v
}

func (v Variables) ValidDomainName() Variables {
	v.appendRunners(DomainName)
	return v
}

func (v Variables) ValidListenAddress() Variables {
	v.appendRunners(ListenAddress)
	return v
}

func (v Variables) ValidURL() Variables {
	v.appendRunners(URL)
	return v
}

func (v Variables) OneOf(values ...string) Variables {
	v.appendRunners(OneOf(values))
	return v
}

func (v Variables) ExactLength(length int) Variables {
	v.appendRunners(ExactLength(length))
	return v
}

func (v Variables) MinLength(min int) Variables {
	v.appendRunners(MinLength(min))
	return v
}

func (v Variables) MaxLength(max int) Variables {
	v.appendRunners(MaxLength(max))
	return v
}

func (v Variables) MinInt(min int64) Variables {
	v.appendRunners(MinInt(min))
	return v
}

func (v Variables) MaxInt(max int64) Variables {
	v.appendRunners(MaxInt(max))
	return v
}

func (v Variables) IntRange(min, max int64) Variables {
	v.appendRunners(MinInt(min), MaxInt(max))
	return v
}

func (v Variables) MinUint(min uint64) Variables {
	v.appendRunners(MinUint(min))
	return v
}

func (v Variables) MaxUint(max uint64) Variables {
	v.appendRunners(MaxUint(max))
	return v
}

func (v Variables) UintRange(min, max uint64) Variables {
	v.appendRunners(MinUint(min), MaxUint(max))
	return v
}

func (v Variables) MinFloat(min float64) Variables {
	v.appendRunners(MinFloat(min))
	return v
}

func (v Variables) MaxFloat(max float64) Variables {
	v.appendRunners(MaxFloat(max))
	return v
}

func (v Variables) FloatRange(min, max float64) Variables {
	v.appendRunners(MinFloat(min), MaxFloat(max))
	return v
}

func (v Variables) Expand() Variables {
	v.appendRunners(Expand)
	return v
}

func (v Variables) Or(c1, c2 Runner) Variables {
	v.appendRunners(OR(c1, c2))
	return v
}

func (v Variables) MatchRegexp(expr *regexp.Regexp) Variables {
	v.appendRunners(MatchRegexp(expr))
	return v
}

func (v Variables) WithRunners(runners ...Runner) Variables {
	v.appendRunners(runners...)
	return v
}

func (v Variables) StringSlice() ([]string, error) {
	r, err := varsToSliceOf(v, (*Variable).String)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (v Variables) Int64Slice() ([]int64, error) {
	return varsToSliceOf(v, (*Variable).Int64)
}

func (v Variables) URLSlice() ([]*url.URL, error) {
	return varsToSliceOf(v, (*Variable).URL)
}

func (v Variables) IntSlice() ([]int, error) {
	return varsToSliceOf(v, (*Variable).Int)
}

func (v Variables) DurationSlice() ([]time.Duration, error) {
	return varsToSliceOf(v, (*Variable).Duration)
}

func (v Variables) BooleanSlice() ([]bool, error) {
	return varsToSliceOf(v, (*Variable).Boolean)
}

func (v Variables) Float32Slice() ([]float32, error) {
	return varsToSliceOf(v, (*Variable).Float32)
}

func (v Variables) Float64Slice() ([]float64, error) {
	return varsToSliceOf(v, (*Variable).Float64)
}

func (v Variables) UintSlice() ([]uint, error) {
	return varsToSliceOf(v, (*Variable).Uint)
}

func (v Variables) Uint8Slice() ([]uint8, error) {
	return varsToSliceOf(v, (*Variable).Uint8)
}

func (v Variables) Uint16Slice() ([]uint16, error) {
	return varsToSliceOf(v, (*Variable).Uint16)
}

func (v Variables) Uint32Slice() ([]uint32, error) {
	return varsToSliceOf(v, (*Variable).Uint32)
}

func (v Variables) Uint64Slice() ([]uint64, error) {
	return varsToSliceOf(v, (*Variable).Uint64)
}

func (v Variables) TimeSlice(layout string) ([]time.Time, error) {
	converter := func(variable *Variable) (time.Time, error) {
		return variable.Time(layout)
	}
	return varsToSliceOf(v, converter)
}

func (v Variables) appendRunners(runners ...Runner) {
	for _, vv := range v {
		vv.runners = append(vv.runners, runners...)
	}
}

func varsToSliceOf[T any](vars Variables, f func(variable *Variable) (T, error)) ([]T, error) {
	result := make([]T, len(vars))
	for i, vv := range vars {
		val, err := f(vv)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}
