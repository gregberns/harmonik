package handlercontract

import (
	"regexp"
	"sync"
)

// RedactionRegistry holds the per-handler value-pattern redaction patterns
// contributed by each subsystem at daemon startup (HC-032).
//
// Each subsystem calls RegisterPattern once during the PL-005 step 0 wiring
// phase to declare the value-regex shapes that identify its provider secrets.
// The registry is then passed to the EventBus constructor so that
// RedactionMiddleware can be called on every payload in Emit.
//
// Spec refs: specs/handler-contract.md §4.7.HC-030, §4.7.HC-032.
// Bead ref: hk-8i31.83.
type RedactionRegistry struct {
	mu       sync.RWMutex
	patterns map[string][]*regexp.Regexp // keyed by subsystem
}

// NewRedactionRegistry constructs an empty RedactionRegistry.
//
// The returned registry has zero registered patterns; subsystems call
// RegisterPattern to populate it before bus construction. The registry
// is safe for concurrent use once all RegisterPattern calls complete and
// before the bus enters live-delivery mode.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func NewRedactionRegistry() *RedactionRegistry {
	return &RedactionRegistry{
		patterns: make(map[string][]*regexp.Regexp),
	}
}

// RegisterPattern registers a set of value-regex patterns for a named
// subsystem (HC-032).
//
// subsystem is the Go-package-identifier string of the registering handler
// (e.g., "claude_handler"). patterns is the slice of compiled regexes; each
// regex is matched against every string-typed payload value in
// RedactionMiddleware. A value that matches any registered pattern is replaced
// with RedactedSentinel.
//
// RegisterPattern MUST be called before the EventBus is sealed per PL-005
// step 0. Calling it after seal produces no error but the patterns will have
// no effect on already-dispatched events.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func (r *RedactionRegistry) RegisterPattern(subsystem string, patterns []*regexp.Regexp) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.patterns[subsystem]
	r.patterns[subsystem] = append(existing, patterns...)
}

// allPatterns returns a flat slice of all registered patterns across all
// subsystems. The slice is a snapshot; the registry mutex is held for reading
// only during the copy.
func (r *RedactionRegistry) allPatterns() []*regexp.Regexp {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []*regexp.Regexp
	for _, pats := range r.patterns {
		all = append(all, pats...)
	}
	return all
}

// RedactionMiddleware applies the full redaction pipeline to payload:
//
//  1. HC-031: replace every field whose NAME matches the common-prefix regex
//     (via [RedactByFieldName]).
//  2. HC-032: replace every string-typed VALUE that matches any pattern
//     registered with the receiver.
//
// Both rules apply the same [RedactedSentinel] replacement. The result is a
// new map; payload is not mutated.
//
// When payload is nil, RedactionMiddleware returns nil.
//
// Spec refs: specs/handler-contract.md §4.7.HC-031, §4.7.HC-032.
func (r *RedactionRegistry) RedactionMiddleware(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}

	// Step 1: HC-031 field-name redaction.
	out := RedactByFieldName(payload)

	// Step 2: HC-032 per-handler value-pattern redaction.
	patterns := r.allPatterns()
	if len(patterns) == 0 {
		return out
	}

	for k, v := range out {
		if s, ok := v.(string); ok {
			for _, re := range patterns {
				if re.MatchString(s) {
					out[k] = RedactedSentinel
					break
				}
			}
		}
	}
	return out
}
