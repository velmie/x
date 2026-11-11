package otelx

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultMaxTraceHeaderBytes   = 512
	defaultMaxBaggageHeaderBytes = 1024
	defaultMaxBaggageMembers     = 16
)

// PropagationFormat selects a single trace-context wire encoding.
type PropagationFormat string

const (
	// FormatW3C selects W3C traceparent propagation. An empty format also selects it.
	FormatW3C PropagationFormat = "w3c"
	// FormatB3Single selects the single-header B3 encoding.
	FormatB3Single PropagationFormat = "b3-single"
	// FormatB3Multi selects the multiple-header B3 encoding.
	FormatB3Multi PropagationFormat = "b3-multi"
)

// BaggageRule permits one application-owned key and its finite set of values.
type BaggageRule struct {
	// Key is a nonempty W3C baggage key.
	Key string
	// Values are nonempty decoded values permitted for this key.
	Values []string
}

// PropagationPolicy bounds metadata accepted and emitted at a boundary.
type PropagationPolicy struct {
	// Format selects one encoding. Empty selects FormatW3C.
	Format PropagationFormat
	// Baggage permits only explicitly listed keys and values. Empty disables baggage.
	Baggage []BaggageRule
	// MaxTraceHeaderBytes limits each trace header value. Zero selects 512.
	MaxTraceHeaderBytes int
	// MaxBaggageHeaderBytes limits the entire baggage value. Zero selects 1024.
	MaxBaggageHeaderBytes int
	// MaxBaggageMembers limits the number of incoming and permitted members. Zero selects 16.
	MaxBaggageMembers int
}

// Propagation implements the native TextMapPropagator with an immutable policy.
// Extraction rejects ambiguous or mixed trace formats and handles baggage
// independently. Vendor tracestate and B3 private debug state are not propagated.
type Propagation struct {
	format          PropagationFormat
	wire            propagation.TextMapPropagator
	allowed         map[string]map[string]struct{}
	maxTraceBytes   int
	maxBaggageBytes int
	maxMembers      int
}

// NewPropagation validates and copies a bounded propagation policy. Invalid
// formats, limits, baggage keys or values return an error wrapping ErrInvalidConfig.
// Every combination of allowed values must fit the configured baggage bounds.
func NewPropagation(policy PropagationPolicy) (*Propagation, error) {
	p := newDefaultPropagation()
	switch policy.Format {
	case "", FormatW3C:
	case FormatB3Single:
		p.format = FormatB3Single
		p.wire = b3.New(b3.WithInjectEncoding(b3.B3SingleHeader))
	case FormatB3Multi:
		p.format = FormatB3Multi
		p.wire = b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader))
	default:
		return nil, fmt.Errorf("Format must be w3c, b3-single or b3-multi: %w", ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	}
	if policy.MaxTraceHeaderBytes < 0 || policy.MaxBaggageHeaderBytes < 0 || policy.MaxBaggageMembers < 0 {
		return nil, fmt.Errorf("propagation limits must be nonnegative: %w", ErrInvalidConfig)
	}
	if policy.MaxTraceHeaderBytes > 0 {
		p.maxTraceBytes = policy.MaxTraceHeaderBytes
	}
	if policy.MaxBaggageHeaderBytes > 0 {
		p.maxBaggageBytes = policy.MaxBaggageHeaderBytes
	}
	if policy.MaxBaggageMembers > 0 {
		p.maxMembers = policy.MaxBaggageMembers
	}
	if len(policy.Baggage) > p.maxMembers {
		return nil, fmt.Errorf("Baggage exceeds MaxBaggageMembers: %w", ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	}
	longest := make([]baggage.Member, 0, len(policy.Baggage))
	maxBytes := 0
	for i, rule := range policy.Baggage {
		if _, exists := p.allowed[rule.Key]; exists {
			return nil, fmt.Errorf("Baggage[%d].Key is duplicated: %w", i, ErrInvalidConfig)
		}
		if len(rule.Values) == 0 {
			return nil, fmt.Errorf("Baggage[%d].Values must be nonempty: %w", i, ErrInvalidConfig)
		}
		allowed := make(map[string]struct{}, len(rule.Values))
		var longestMember baggage.Member
		maxMemberBytes := 0
		for _, value := range rule.Values {
			member, err := baggage.NewMemberRaw(rule.Key, value)
			encoded := member.String()
			if err != nil || value == "" || encoded == "" {
				return nil, fmt.Errorf("Baggage[%d] requires a valid wire key and nonempty values: %w", i, ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
			}
			allowed[value] = struct{}{}
			if len(encoded) > maxMemberBytes {
				maxMemberBytes = len(encoded)
				longestMember = member
			}
		}
		p.allowed[rule.Key] = allowed
		longest = append(longest, longestMember)
		maxBytes += maxMemberBytes
	}
	if len(longest) > 0 {
		maxBytes += len(longest) - 1
	}
	if maxBytes > p.maxBaggageBytes {
		return nil, fmt.Errorf("Baggage can exceed MaxBaggageHeaderBytes: %w", ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	}
	if _, err := baggage.New(longest...); err != nil {
		return nil, fmt.Errorf("Baggage exceeds native SDK limits: %w", ErrInvalidConfig) //nolint:staticcheck // Preserve the existing diagnostic.
	}
	return p, nil
}

// Extract replaces inherited span context and baggage while preserving the
// caller's cancellation, deadline and ordinary context values. Trace and
// baggage validation are independent. Repeated values, aliases or mixed trace
// encodings invalidate the parent; invalid baggage is discarded separately.
func (p *Propagation) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	ctx = trace.ContextWithSpanContext(baggage.ContextWithoutBaggage(ctx), trace.SpanContext{})
	selected := propagation.MapCarrier{}
	traceValid := true
	var bagValue string
	bagSeen := false
	bagValid := true
	for _, key := range carrier.Keys() {
		lower := strings.ToLower(key)
		switch {
		case isTraceHeader(lower):
			values := carrierValues(carrier, key)
			_, exists := selected[lower]
			if !p.accepts(lower) || exists || len(values) != 1 || len(values[0]) > p.maxTraceBytes {
				traceValid = false
				continue
			}
			selected[lower] = values[0]
		case lower == "baggage":
			values := carrierValues(carrier, key)
			if bagSeen || len(values) != 1 || len(values[0]) > p.maxBaggageBytes {
				bagValid = false
				continue
			}
			bagSeen = true
			bagValue = values[0]
		}
	}
	if traceValid {
		if p.format == FormatW3C {
			if normalized, ok := normalizedTraceparent(selected["traceparent"]); ok {
				selected["traceparent"] = normalized
			} else {
				traceValid = false
			}
		} else if p.format == FormatB3Single && !validB3SingleShape(selected["b3"]) {
			traceValid = false
		}
	}
	if traceValid {
		// Extract on a fresh context so native B3 parser flags cannot escape.
		extracted := p.wire.Extract(context.Background(), selected)
		sc := trace.SpanContextFromContext(extracted).WithTraceState(trace.TraceState{})
		if sc.IsValid() {
			ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
		}
	}
	if bagValid && bagSeen && len(p.allowed) != 0 {
		if parsed, ok := p.parseBaggage(bagValue); ok {
			ctx = baggage.ContextWithBaggage(ctx, p.filterBaggage(parsed))
		}
	}
	return ctx
}

// Inject emits only permitted metadata within the policy bounds. HeaderCarrier
// and MapCarrier have stale recognized fields and aliases removed first. Other
// carriers must provide fresh propagation fields because TextMapCarrier has no
// delete operation.
func (p *Propagation) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	clearPropagationFields(carrier)
	sc := trace.SpanContextFromContext(ctx).WithTraceState(trace.TraceState{})
	sc = sc.WithTraceFlags(sc.TraceFlags() & trace.FlagsSampled)
	if sc.IsValid() {
		selected := propagation.MapCarrier{}
		p.wire.Inject(trace.ContextWithSpanContext(context.Background(), sc), selected)
		valid := true
		for _, value := range selected {
			if len(value) > p.maxTraceBytes {
				valid = false
				break
			}
		}
		if valid {
			for key, value := range selected {
				carrier.Set(key, value)
			}
		}
	}
	if bag := p.filterBaggage(baggage.FromContext(ctx)); bag.Len() > 0 {
		value := bag.String()
		if len(value) <= p.maxBaggageBytes && bag.Len() <= p.maxMembers {
			carrier.Set("baggage", value)
		}
	}
}

// Fields returns the fields this policy may emit. The returned slice is owned
// by the caller.
func (p *Propagation) Fields() []string {
	var fields []string
	switch p.format {
	case FormatW3C:
		fields = []string{"traceparent"}
	case FormatB3Single:
		fields = []string{"b3"}
	case FormatB3Multi:
		fields = []string{"x-b3-traceid", "x-b3-spanid", "x-b3-sampled"}
	}
	if len(p.allowed) > 0 {
		fields = append(fields, "baggage")
	}
	return fields
}

// FilterContext removes vendor state and unapproved baggage without changing
// trace or span identity or the local/remote parent distinction. Cancellation,
// deadlines, and ordinary context values are preserved.
func (p *Propagation) FilterContext(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx).WithTraceState(trace.TraceState{})
	ctx = baggage.ContextWithBaggage(ctx, p.filterBaggage(baggage.FromContext(ctx)))
	return trace.ContextWithSpanContext(ctx, sc)
}

func newDefaultPropagation() *Propagation {
	return &Propagation{
		format:          FormatW3C,
		wire:            propagation.TraceContext{},
		allowed:         make(map[string]map[string]struct{}),
		maxTraceBytes:   defaultMaxTraceHeaderBytes,
		maxBaggageBytes: defaultMaxBaggageHeaderBytes,
		maxMembers:      defaultMaxBaggageMembers,
	}
}

func (p *Propagation) accepts(key string) bool {
	switch p.format {
	case FormatW3C:
		return key == "traceparent"
	case FormatB3Single:
		return key == "b3"
	default:
		return strings.HasPrefix(key, "x-b3-")
	}
}

func (p *Propagation) parseBaggage(value string) (baggage.Baggage, bool) {
	members := strings.Split(value, ",")
	if len(members) > p.maxMembers {
		return baggage.Baggage{}, false
	}
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		key, _, ok := strings.Cut(member, "=")
		key = strings.TrimSpace(key)
		if _, exists := seen[key]; !ok || exists {
			return baggage.Baggage{}, false
		}
		seen[key] = struct{}{}
	}
	parsed, err := baggage.Parse(value)
	return parsed, err == nil
}

func (p *Propagation) filterBaggage(input baggage.Baggage) baggage.Baggage {
	members := make([]baggage.Member, 0, len(p.allowed))
	for key, values := range p.allowed {
		member := input.Member(key)
		if _, ok := values[member.Value()]; ok && len(member.Properties()) == 0 {
			members = append(members, member)
		}
	}
	result, err := baggage.New(members...)
	if err != nil {
		return baggage.Baggage{}
	}
	return result
}

func normalizedTraceparent(value string) (string, bool) {
	if len(value) != 55 || value[:3] != "00-" || value[35] != '-' || value[52] != '-' {
		return "", false
	}
	for i, c := range value {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	flags, _ := strconv.ParseUint(value[53:], 16, 8)
	if flags&1 == 0 {
		return value[:53] + "00", true
	}
	return value[:53] + "01", true
}

func validB3SingleShape(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) == 1 {
		return value == "0" || value == "1" || value == "d"
	}
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	if (len(parts[0]) != 16 && len(parts[0]) != 32) || len(parts[1]) != 16 {
		return false
	}
	if len(parts) >= 3 && parts[2] != "0" && parts[2] != "1" && parts[2] != "d" {
		return false
	}
	return len(parts) != 4 || len(parts[3]) == 16
}

func carrierValues(carrier propagation.TextMapCarrier, key string) []string {
	switch c := carrier.(type) {
	case propagation.HeaderCarrier:
		// http.Header.Values canonicalizes the key, which can hide raw aliases.
		return http.Header(c)[key]
	case propagation.MapCarrier:
		return []string{c[key]}
	case propagation.ValuesGetter:
		return c.Values(key)
	default:
		return []string{carrier.Get(key)}
	}
}

func isTraceHeader(key string) bool {
	switch key {
	case "traceparent", "b3", "x-b3-traceid", "x-b3-spanid", "x-b3-parentspanid", "x-b3-sampled", "x-b3-flags":
		return true
	default:
		return false
	}
}

func clearPropagationFields(carrier propagation.TextMapCarrier) {
	for _, key := range carrier.Keys() {
		lower := strings.ToLower(key)
		if !isTraceHeader(lower) && lower != "tracestate" && lower != "baggage" {
			continue
		}
		switch c := carrier.(type) {
		case propagation.HeaderCarrier:
			delete(c, key)
		case propagation.MapCarrier:
			delete(c, key)
		}
	}
}
