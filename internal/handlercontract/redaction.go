package handlercontract

import "regexp"

// redactionCommonPrefixRe is the compiled form of the HC-031 common-prefix
// regex. Any field whose NAME matches this regex MUST be replaced with
// RedactedSentinel before emission.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
// The regex is case-insensitive; the match is on field name, not value.
var redactionCommonPrefixRe = regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

// RedactedSentinel is the literal replacement string required by HC-031 and
// HC-032 for any redacted field value.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
const RedactedSentinel = "<redacted>"

// RedactByFieldName applies the HC-031 common-prefix redaction rule to payload.
//
// For every key in payload whose name matches the case-insensitive regex
// `(secret|token|password|api[_-]?key|auth)`, the associated value is replaced
// with RedactedSentinel. All other keys are copied unchanged.
//
// The match is on KEY NAME, not value. A payload that names a field "secret"
// is a producer-side bug; this function provides defence-in-depth per
// §4.7.HC-028.
//
// RedactByFieldName does NOT recurse into nested maps. Callers that need
// recursive redaction MUST apply RedactByFieldName at each nesting level. The
// redaction registry middleware (hk-8i31.37) is responsible for recursion.
//
// The returned map is a new map; payload is not mutated.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
//
// Tags: mechanism
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
