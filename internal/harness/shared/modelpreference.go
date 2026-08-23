package shared

import (
	"fmt"
	"regexp"
)

var modelRegex = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

const modelMaxLen = 128

var validEffortLevels = map[string]struct{}{
	"low":    {},
	"medium": {},
	"high":   {},
	"xhigh":  {},
	"max":    {},
}

// ModelPreferenceError is the typed error returned by ValidateModel and
// ValidateEffort when a ModelPreference field fails its shape or enum
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

// ValidateModel checks that model satisfies the HC-055a shape constraint:
//   - matches ^[A-Za-z0-9._:/-]+$
//   - length ≤ 128 chars
//
// Returns *ModelPreferenceError on violation; nil on success.
// Callers MUST NOT call ValidateModel with an empty string; the convention is
// to skip validation (and flag emission) when the field is empty.
func ValidateModel(model string) error {
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

// ValidateEffort checks that effort is a member of the closed enum
// {low, medium, high, xhigh, max} (HC-055a).
//
// Returns *ModelPreferenceError on violation; nil on success.
// Callers MUST NOT call ValidateEffort with an empty string; the convention is
// to skip validation (and flag emission) when the field is empty.
func ValidateEffort(effort string) error {
	if _, ok := validEffortLevels[effort]; !ok {
		return &ModelPreferenceError{
			Field:  "effort",
			Value:  effort,
			Reason: "must be one of {low, medium, high, xhigh, max}",
		}
	}
	return nil
}
