package daemon

// modelpreference.go — ModelPreference shape validation per HC-055a (hk-xo03m).
//
// Provides regex + enum guards for the model and effort fields that flow through
// claudeRunCtx into buildClaudeLaunchSpec.  Both guards are called before argv
// construction; invalid values produce a *ModelPreferenceError rather than being
// silently dropped.
//
// Judgment calls (hk-xo03m):
//   - modelRegex is a package-level compiled var (no per-call compile cost).
//   - validEffortLevels is a map[string]struct{} (O(1) lookup; enum is short).
//   - *ModelPreferenceError is the typed error; it names the failing field and value.
//
// Spec refs:
//   - specs/handler-contract.md §4.10 HC-055a — ModelPreference descriptor invariants.
//   - specs/execution-model.md §4.3 EM-012b — model/effort resolution chain.

import (
	"fmt"
	"regexp"
)

// modelRegex is the shape constraint for the model alias (HC-055a).
// Allows alphanumeric characters plus the punctuation required by common model
// identifiers (dots, underscores, colons, slashes, hyphens). Rejects shell
// metacharacters and whitespace.
var modelRegex = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

// modelMaxLen is the maximum permitted length for a model alias (HC-055a).
const modelMaxLen = 128

// validEffortLevels is the closed enum of permitted effort values (HC-055a).
// Empty string is handled by the caller (empty → no flag emitted, no validation).
var validEffortLevels = map[string]struct{}{
	"low":    {},
	"medium": {},
	"high":   {},
	"xhigh":  {},
	"max":    {},
}

// ModelPreferenceError is the typed error returned by validateModel and
// validateEffort when a ModelPreference field fails its shape or enum
// constraint (HC-055a).
type ModelPreferenceError struct {
	// Field is the name of the failing field: "model" or "effort".
	Field string
	// Value is the supplied value that failed validation.
	Value string
	// Reason is a short human-readable description of the constraint violated.
	Reason string
}

func (e *ModelPreferenceError) Error() string {
	return fmt.Sprintf("daemon: ModelPreference: field %q value %q is invalid: %s (HC-055a)", e.Field, e.Value, e.Reason)
}

// validateModel checks that model satisfies the HC-055a shape constraint:
//   - matches ^[A-Za-z0-9._:/-]+$
//   - length ≤ 128 chars
//
// Returns *ModelPreferenceError on violation; nil on success.
// Callers MUST NOT call validateModel with an empty string; the convention is
// to skip validation (and flag emission) when the field is empty.
func validateModel(model string) error {
	if len(model) > modelMaxLen {
		return &ModelPreferenceError{
			Field:  "model",
			Value:  model,
			Reason: fmt.Sprintf("exceeds maximum length %d", modelMaxLen),
		}
	}
	if !modelRegex.MatchString(model) {
		return &ModelPreferenceError{
			Field:  "model",
			Value:  model,
			Reason: fmt.Sprintf("does not match shape constraint %q", modelRegex.String()),
		}
	}
	return nil
}

// validateEffort checks that effort is a member of the closed enum
// {low, medium, high, xhigh, max} (HC-055a).
//
// Returns *ModelPreferenceError on violation; nil on success.
// Callers MUST NOT call validateEffort with an empty string; the convention is
// to skip validation (and flag emission) when the field is empty.
func validateEffort(effort string) error {
	if _, ok := validEffortLevels[effort]; !ok {
		return &ModelPreferenceError{
			Field:  "effort",
			Value:  effort,
			Reason: "must be one of {low, medium, high, xhigh, max}",
		}
	}
	return nil
}
