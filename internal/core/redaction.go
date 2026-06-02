package core

import "regexp"

// redactionCommonPrefixRe is the HC-031 common-prefix regex applied to payload
// field names before emission.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
var redactionCommonPrefixRe = regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

// RedactedSentinel is the replacement string for any redacted field value
// (HC-031 and HC-032).
//
// Spec: specs/handler-contract.md §4.7.HC-031.
const RedactedSentinel = "<redacted>"

// RedactByFieldName applies the HC-031 common-prefix redaction rule to payload.
//
// For every key whose name matches the case-insensitive regex
// `(secret|token|password|api[_-]?key|auth)`, the value is replaced with
// RedactedSentinel. All other keys are copied unchanged.
//
// Returns a new map; payload is not mutated. Returns nil for a nil payload.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
func RedactByFieldName(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if redactionCommonPrefixRe.MatchString(k) {
			out[k] = RedactedSentinel
		} else {
			out[k] = v
		}
	}
	return out
}
