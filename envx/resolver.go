package envx

import (
	"strings"
)

// SourceOption represents a functional option for configuring a source.
type SourceOption func(*sourceConfig)

// sourceConfig holds the configuration options for a source.
type sourceConfig struct {
	labels       []string
	explicitOnly bool
}

// WithLabels returns a SourceOption that sets the labels for a source.
// Labels are used to identify and filter sources for variable resolution.
// Multiple labels can be assigned to a single source.
func WithLabels(labels ...string) SourceOption {
	return func(config *sourceConfig) {
		config.labels = append(config.labels, labels...)
	}
}

// IsExplicitOnly returns a SourceOption that marks a source as explicit-only.
// Explicit-only sources are only used when explicitly referenced in variable tags
// using the [label1,label2] syntax and will not be included in the default search.
func IsExplicitOnly() SourceOption {
	return func(config *sourceConfig) {
		config.explicitOnly = true
	}
}

// applyOptions processes source options and returns the resulting configuration.
func applyOptions(opts ...SourceOption) *sourceConfig {
	config := &sourceConfig{
		labels:       []string{},
		explicitOnly: false,
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

// labeledSource represents a source with associated labels and explicitOnly flag.
type labeledSource struct {
	source       Source
	labels       []string
	explicitOnly bool
}

// hasLabel checks if the source has the specified label.
func (ls *labeledSource) hasLabel(label string) bool {
	if len(ls.labels) == 0 {
		return false
	}

	for _, l := range ls.labels {
		if l == label {
			return true
		}
	}
	return false
}

// hasAnyLabel checks if the source has any of the specified labels.
func (ls *labeledSource) hasAnyLabel(labels []string) bool {
	if len(labels) == 0 || len(ls.labels) == 0 {
		return false
	}

	for _, targetLabel := range labels {
		if ls.hasLabel(targetLabel) {
			return true
		}
	}
	return false
}

// SearchStep represents a single step in a search plan, containing a variable
// name and optional source labels to search.
type SearchStep struct {
	Name     string
	Labels   []string
	IsQuoted bool
}

// SearchPlan represents a complete plan for searching for variables across sources.
// It consists of multiple SearchSteps in the order of priority.
type SearchPlan struct {
	Steps []SearchStep
}

// Resolver defines methods that any resolver must implement.
type Resolver interface {
	// Get looks up a variable by name.
	Get(name string) (*Variable, error)

	// Coalesce tries a list of variable names and returns the first one found.
	Coalesce(names ...string) (*Variable, error)

	// AddSource adds a new source to the resolver.
	AddSource(src Source, opts ...SourceOption)

	// ResolvePlan executes a search plan and returns the first value found.
	ResolvePlan(plan SearchPlan) (*Variable, error)
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

// Logger for warnings and errors
type Logger interface {
	Warn(msg string, args ...any)
}

// NoopLogger does nothing
type NoopLogger struct{}

func (NoopLogger) Warn(string, ...any) {}

// StandardResolver implements Resolver interface and manages multiple sources.
type StandardResolver struct {
	sources      []labeledSource
	errorHandler ErrorHandler
	logger       Logger
}

// NewResolver creates a new StandardResolver with the given sources.
func NewResolver(sources ...Source) *StandardResolver {
	r := &StandardResolver{
		sources:      make([]labeledSource, 0, len(sources)),
		errorHandler: BreakOnError,
		logger:       NoopLogger{},
	}

	for _, src := range sources {
		r.AddSource(src)
	}

	return r
}

// WithErrorHandler sets a custom error handler and returns the resolver for chaining.
func (r *StandardResolver) WithErrorHandler(handler ErrorHandler) *StandardResolver {
	r.errorHandler = handler
	return r
}

// WithLogger sets a custom logger
func (r *StandardResolver) WithLogger(logger Logger) *StandardResolver {
	r.logger = logger
	return r
}

// AddSource adds a new source to the resolver with optional configuration.
// The new source is added to the end of the source list (lowest priority).
// For backward compatibility, if no options are provided, the source is added
// with no labels and explicitOnly=false.
func (r *StandardResolver) AddSource(src Source, opts ...SourceOption) {
	cfg := applyOptions(opts...)

	r.sources = append(r.sources, labeledSource{
		source:       src,
		labels:       cfg.labels,
		explicitOnly: cfg.explicitOnly,
	})
}

// Get looks up a variable by name from all registered sources.
// Returns the first value found or an empty Variable if not found in any source.
// Returns error if a source returns an error and the error handler decides to break.
// Only uses sources that are not marked as explicitOnly.
func (r *StandardResolver) Get(name string) (*Variable, error) {
	// Filter sources to exclude explicitOnly ones
	filtered := r.getFilteredSources(nil)

	for _, src := range filtered {
		val, exist, err := src.source.Lookup(name)
		if err != nil {
			if r.errorHandler == nil {
				return nil, err
			}

			continueResolution, handlerErr := r.errorHandler(err, src.source.Name())
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
// Only uses sources that are not marked as explicitOnly.
func (r *StandardResolver) Coalesce(names ...string) (*Variable, error) {
	if len(names) == 0 {
		return &Variable{}, nil
	}

	// Store all names to try in the correct order
	allNames := make([]string, len(names))
	copy(allNames, names)

	// Create a search plan for backward compatibility
	var plan SearchPlan
	for _, name := range names {
		plan.Steps = append(plan.Steps, SearchStep{
			Name:   name,
			Labels: nil, // nil means use default (non-explicitOnly) sources
		})
	}

	// Use the search plan to resolve
	v, err := r.ResolvePlan(plan)
	if err != nil {
		return nil, err
	}

	// Ensure proper naming for backward compatibility
	v.Name = names[0]
	v.AllNames = allNames

	return v, nil
}

// ResolvePlan executes a search plan and returns the first value found.
// It follows the order of steps in the plan, filtering sources for each step
// based on the labels specified.
func (r *StandardResolver) ResolvePlan(plan SearchPlan) (*Variable, error) {
	if len(plan.Steps) == 0 {
		return &Variable{AllNames: []string{}}, nil
	}

	allNames := make([]string, len(plan.Steps))
	for i, step := range plan.Steps {
		allNames[i] = step.Name
	}

	for _, step := range plan.Steps {
		sources := r.getFilteredSourcesForStep(step)
		for _, src := range sources {
			val, exist, err := src.source.Lookup(step.Name)
			if err != nil {
				if r.errorHandler == nil {
					return nil, err
				}

				continueResolution, handlerErr := r.errorHandler(err, src.source.Name())
				if !continueResolution {
					return nil, handlerErr
				}
				continue
			}
			if exist && val != "" {
				return &Variable{
					Name:     plan.Steps[0].Name, // Primary name for error messages
					Val:      val,
					Exist:    true,
					AllNames: allNames,
				}, nil
			}
		}
	}

	// No value found - return variable with the first name but keep record of all tried names
	return &Variable{
		Name:     plan.Steps[0].Name,
		Exist:    false,
		AllNames: allNames,
	}, nil
}

// getFilteredSources returns sources that should be used for standard lookups.
func (r *StandardResolver) getFilteredSources(labels []string) []labeledSource {
	if len(labels) == 0 {
		// Use all non-explicitOnly sources
		filtered := make([]labeledSource, 0, len(r.sources))
		for _, src := range r.sources {
			if !src.explicitOnly {
				filtered = append(filtered, src)
			}
		}
		return filtered
	}

	// Use only sources with matching labels
	filtered := make([]labeledSource, 0)
	for _, src := range r.sources {
		if src.hasAnyLabel(labels) {
			filtered = append(filtered, src)
		}
	}

	// Log warning if no sources matched
	if len(filtered) == 0 {
		r.logger.Warn("no sources matched labels", "["+strings.Join(labels, " ")+"]")
	}

	return filtered
}

// getFilteredSourcesForStep returns sources that match the criteria for a search step.
// If the step has labels, only sources with those labels are returned.
// If the step has no labels, all non-explicitOnly sources are returned.
func (r *StandardResolver) getFilteredSourcesForStep(step SearchStep) []labeledSource {
	return r.getFilteredSources(step.Labels)
}
