package core

import (
	"regexp"
	"sync"
)

// RedactionRegistry holds per-handler value-pattern redaction patterns
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
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func NewRedactionRegistry() *RedactionRegistry {
	return &RedactionRegistry{
		patterns: make(map[string][]*regexp.Regexp),
	}
}

// RegisterPattern registers a set of value-regex patterns for a named subsystem
// (HC-032).
//
// RegisterPattern MUST be called before the EventBus is sealed per PL-005
// step 0.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func (r *RedactionRegistry) RegisterPattern(subsystem string, patterns []*regexp.Regexp) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.patterns[subsystem]
	r.patterns[subsystem] = append(existing, patterns...)
}

// allPatterns returns a flat snapshot of all registered patterns across all
// subsystems.
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
//     (via RedactByFieldName).
//  2. HC-032: replace every string-typed VALUE that matches any pattern
//     registered with the receiver.
//
// Returns a new map; payload is not mutated. Returns nil for a nil payload.
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
