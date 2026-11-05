// Package health samples explicitly selected dependency probes and exposes
// readiness reports over HTTP. Probe causes remain available for diagnostics;
// public reports contain only application-owned names and safe reason codes.
package health

import (
	"context"
	"errors"
	"time"
)

const (
	// DefaultProbeTimeout bounds one cooperative probe.
	DefaultProbeTimeout = 500 * time.Millisecond
	// DefaultHandlerTimeout bounds the complete readiness sample.
	DefaultHandlerTimeout = 500 * time.Millisecond

	// ReasonUnavailable identifies an otherwise unclassified dependency failure.
	ReasonUnavailable = "dependency_unavailable"
	// ReasonDeadlineExceeded identifies an expired probe or request budget.
	ReasonDeadlineExceeded = "deadline_exceeded"
	// ReasonCanceled identifies cancellation without a deadline failure.
	ReasonCanceled = "canceled"
)

// Probe describes one explicitly selected dependency check. Name must be a
// unique, nonempty, stable, safe dependency name. Check is required and must
// return after its context ends, including finishing any cleanup it starts.
// Timeout must be nonnegative. Reason must return an application-owned safe,
// bounded code and must not derive it from an error message.
type Probe struct {
	// Name is a stable, safe dependency name, unique within a sample.
	Name string
	// Check returns nil when the dependency is currently ready.
	Check func(ctx context.Context) error
	// Timeout bounds this probe; zero selects DefaultProbeTimeout.
	Timeout time.Duration
	// Reason optionally maps a failure to an owned public code; empty uses the default.
	Reason func(err error) string
}

// Component reports one probe result and retains its diagnostic cause.
type Component struct {
	// Name is the safe, application-owned name of the probed dependency.
	Name string `json:"name"`
	// Ready reports whether the probe completed successfully within its budget.
	Ready bool `json:"ready"`
	// Reason is a safe failure code, or empty when Ready is true.
	Reason string `json:"reason,omitempty"`
	// Cause retains the original failure for diagnostics and is not serialized.
	Cause error `json:"-"`
}

// Report describes a current sample of the selected dependencies.
type Report struct {
	// Ready is true when every selected probe succeeds, including an empty set.
	Ready bool `json:"ready"`
	// Components contains results in probe order and is non-nil even when empty.
	Components []Component `json:"components"`
}

// Check samples probes synchronously in declaration order without logging.
// Each probe has its own timeout, capped by ctx. A failed probe does not skip
// later probes, but after ctx ends remaining probes are reported without I/O.
// Check waits for each executing probe to return, even after its deadline.
// A late success is a failure. Causes preserve both the probe's error and any
// independent context failure, including a cancellation cause, for errors.Is and
// errors.As. Reason precedence is
// deadline, cancellation, a nonempty custom reason, then ReasonUnavailable.
// An empty selection is ready and has an empty, non-nil Components slice.
func Check(ctx context.Context, probes ...Probe) Report {
	report := Report{
		Ready:      true,
		Components: make([]Component, 0, len(probes)),
	}
	for _, probe := range probes {
		component := checkProbe(ctx, probe)
		report.Components = append(report.Components, component)
		if !component.Ready {
			report.Ready = false
		}
	}
	return report
}

func checkProbe(ctx context.Context, probe Probe) Component {
	component := Component{
		Name: probe.Name,
	}
	cause := joinContextCause(nil, ctx)
	if cause == nil {
		timeout := probe.Timeout
		if timeout == 0 {
			timeout = DefaultProbeTimeout
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cause = probe.Check(probeCtx)
		cause = joinContextCause(cause, probeCtx)
		cause = joinContextCause(cause, ctx)
	}
	if cause == nil {
		component.Ready = true
		return component
	}
	component.Cause = cause
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		component.Reason = ReasonDeadlineExceeded
	case errors.Is(cause, context.Canceled):
		component.Reason = ReasonCanceled
	case probe.Reason != nil:
		component.Reason = probe.Reason(cause)
	}
	if component.Reason == "" {
		component.Reason = ReasonUnavailable
	}
	return component
}

func joinContextCause(cause error, ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		cause = joinCause(cause, err)
		return joinCause(cause, context.Cause(ctx))
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return joinCause(cause, context.DeadlineExceeded)
	}
	return cause
}

func joinCause(cause, contextErr error) error {
	if contextErr == nil || errors.Is(cause, contextErr) {
		return cause
	}
	if cause == nil {
		return contextErr
	}
	return errors.Join(cause, contextErr)
}
