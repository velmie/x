package errorsx

import "errors"

// As attempts to find the first error in the errors chain that matches the type T and returns it.
// If no matching error is found, the zero value of type T is returned.
// An error is considered a match if its concrete value can be assigned to a variable
// of type T.
func As[T any](err error) (result T) {
	var ok bool
	for e := err; e != nil; e = errors.Unwrap(e) {
		if result, ok = e.(T); ok {
			return result
		}
	}
	return
}

// UnwrapF iterates through the error chain of err, invoking the provided function f
// for each error in the chain. If f returns for any error in the chain it stops iterating and returns true,
// Otherwise, it returns false.
func UnwrapF(err error, f func(target error) bool) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if f(e) {
			return true
		}
	}
	return false
}
