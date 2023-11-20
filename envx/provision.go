package envx

import "errors"

// Getter represents a function that retrieves a value and possibly returns an error
type Getter[T any] func() (T, error)

// Default is a helper function which helps to provision default values
func Default[T any](defaultVal T, v *Variable, g Getter[T]) Getter[T] {
	return func() (T, error) {
		gotVal, err := g()
		if err != nil {
			return gotVal, err
		}
		if !v.Exist {
			return defaultVal, nil
		}

		return gotVal, nil
	}
}

// Setter represents a function that sets a value and possibly returns an error
type Setter func() error

// Set creates a setter for a target from a getter.
// It sets the value from the getter to the target.
func Set[T any](target *T, g Getter[T]) Setter {
	return func() error {
		val, err := g()
		if err != nil {
			return err
		}
		*target = val
		return nil
	}
}

// Supply executes setter in order.
// It collects errors from each setter and returns a combined error if any setter fails.
func Supply(setters ...Setter) error {
	var errs []error
	for _, s := range setters {
		if err := s(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Prototype represents an environment variable prototype that can be customized with runners and a prefix
type Prototype struct {
	prefix  Prefixed
	runners []Runner
}

// CreatePrototype returns a new instance of Prototype
func CreatePrototype() *Prototype {
	return &Prototype{}
}

// WithPrefix sets a prefix for the Prototype
func (p *Prototype) WithPrefix(prefix string) *Prototype {
	p.prefix = Prefixed(prefix)
	return p
}

// WithRunners appends the provided runners to the prototype
func (p *Prototype) WithRunners(runners ...Runner) *Prototype {
	p.runners = append(p.runners, runners...)
	return p
}

// Get retrieves an environment variable by Name based on the prototype configuration
func (p *Prototype) Get(name string) *Variable {
	v := p.prefix.Get(name)
	return p.copyRunners(v)
}

// Coalesce retrieves the first available environment variable from the
// given names based on the prototype configuration
func (p *Prototype) Coalesce(name ...string) *Variable {
	v := p.prefix.Coalesce(name...)
	return p.copyRunners(v)
}

// copyRunners copies runners from a prototype to a variable
func (p *Prototype) copyRunners(v *Variable) *Variable {
	if v == nil {
		return v
	}
	v.runners = make([]Runner, len(p.runners))
	copy(v.runners, p.runners)

	return v
}
