package shared

// modelpreference.go — the HC-055a shape/enum guards for the ModelPreference
// model and effort fields.
//
// These validators are called from a harness launch-spec builder (the claude
// builder runs them before it appends --model / --effort to argv), so they
// cannot stay daemon-private once a harness leaves the daemon. The 4-tier
// EM-012b RESOLUTION walk stays in internal/daemon/modelpreference.go — it
// reads project config and emits bus events, which is daemon wiring, not
// harness logic.
//
// The "daemon:" prefix on ModelPreferenceError.Error() is preserved
// byte-verbatim from the daemon original: this is a pure move, and rewriting
// the prefix would be a behavior delta smuggled into an extraction. A
// prefix-hygiene sweep across the extracted packages is follow-up work.
//
// Spec refs:
//   - specs/handler-contract.md §4.10 HC-055a — ModelPreference descriptor invariants.
//
// Origin: internal/daemon/modelpreference.go lines 43–115, moved by
// plans/2026-07-21-p2-extraction/E1b-claude.md unit E1b-prep.

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
