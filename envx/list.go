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
