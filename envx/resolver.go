package envx

// Resolver defines methods that any resolver must implement.
type Resolver interface {
	// Get looks up a variable by name.
	Get(name string) (*Variable, error)

	// Coalesce tries a list of variable names and returns the first one found.
	Coalesce(names ...string) (*Variable, error)

	// AddSource adds a new source to the resolver.
	AddSource(src Source)
}

// ErrorHandler defines how errors from sources should be handled
type ErrorHandler func(err error, sourceName string) (bool, error)

// ContinueOnError is the default error handler that ignores errors and continues to next source
func ContinueOnError(err error, sourceName string) (bool, error) {
	return true, nil
}

// BreakOnError is an error handler that stops resolution on first error
func BreakOnError(err error, sourceName string) (bool, error) {
	return false, err
}

// StandardResolver implements Resolver interface and manages multiple sources,
// looking up values from them sequentially.
type StandardResolver struct {
	sources      []Source
	errorHandler ErrorHandler
}

// NewResolver creates a new StandardResolver with the given sources.
// Sources will be queried in the order they are provided.
// By default, uses BreakOnError as the error handler.
func NewResolver(sources ...Source) *StandardResolver {
	return &StandardResolver{
		sources:      sources,
		errorHandler: BreakOnError,
	}
}

// WithErrorHandler sets a custom error handler and returns the resolver for chaining.
func (r *StandardResolver) WithErrorHandler(handler ErrorHandler) *StandardResolver {
	r.errorHandler = handler
	return r
}

// AddSource adds a new source to the resolver.
// The new source is added to the end of the source list (lowest priority).
func (r *StandardResolver) AddSource(src Source) {
	r.sources = append(r.sources, src)
}

// Get looks up a variable by name from all registered sources.
// Returns the first value found or an empty Variable if not found in any source.
// Returns error if a source returns an error and the error handler decides to break.
func (r *StandardResolver) Get(name string) (*Variable, error) {
	for _, src := range r.sources {
		val, exist, err := src.Lookup(name)
		if err != nil {
			if r.errorHandler == nil {
				return nil, err
			}

			continueResolution, handlerErr := r.errorHandler(err, src.Name())
			if !continueResolution {
				return nil, handlerErr
			}
			// Continue to next source if handler allows
			continue
		}
		if exist {
			return &Variable{
				Name:     name,
				Val:      val,
				Exist:    true,
				AllNames: []string{name},
			}, nil
		}
	}

	// No value found in any source
	return &Variable{
		Name:     name,
		Exist:    false,
		AllNames: []string{name},
	}, nil
}

// Coalesce tries a list of variable names and returns the first one found.
// It tries each name in all sources before moving to the next name.
// Returns error if a source returns an error and the error handler decides to break.
func (r *StandardResolver) Coalesce(names ...string) (*Variable, error) {
	if len(names) == 0 {
		return &Variable{}, nil
	}

	// Store all names to try in the correct order
	allNames := make([]string, len(names))
	copy(allNames, names)

	// Try each name in order across all sources
	for _, name := range names {
		for _, src := range r.sources {
			val, exist, err := src.Lookup(name)
			if err != nil {
				if r.errorHandler == nil {
					return nil, err
				}

				continueResolution, handlerErr := r.errorHandler(err, src.Name())
				if !continueResolution {
					return nil, handlerErr
				}
				// Continue to next source if handler allows
				continue
			}
			if exist && val != "" {
				return &Variable{
					Name:     names[0], // Primary name (for error messages) is the first name
					Val:      val,
					Exist:    true,
					AllNames: allNames,
				}, nil
			}
		}
	}

	// No value found - return variable with first name but keep record of all tried names
	return &Variable{
		Name:     names[0],
		Exist:    false,
		AllNames: allNames,
	}, nil
}
