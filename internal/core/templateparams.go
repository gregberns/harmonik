package core

// templateparams.go — ingestion validation for launch template-params (WG-045).
//
// Launch template params arrive over the queue-submit RPC and are settable by any
// local agent (a crew/captain LLM running `harmonik queue submit`); they may also
// carry EXTERNAL data (e.g. a Sentry issue id threaded into a tool_command). They
// are therefore UNTRUSTED with respect to the shell sink. ValidateTemplateParams is
// the universal hygiene chokepoint shared by the queue-submit RPC boundary
// (internal/queue) and the workflow substitution path (internal/workflow); it
// rejects the highest-leverage injection primitives (NUL / newline / control chars)
// and bounds value/key size before a value can reach substitution.
//
// This is defense-in-depth; the load-bearing close is post-parse per-attribute
// substitution with shell-quoting of tool_command values (see internal/workflow).
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-045 — template-param trust boundary (untrusted).
//   - specs/queue-model.md §2.6 — Item.template_params ingestion obligation.
//
// Tags: mechanism, normative

import (
	"fmt"
	"regexp"
	"unicode"
)

// Template-param ingestion limits (WG-045 untrusted-param hardening). Named so the
// bound is auditable and reused identically at every chokepoint.
const (
	// MaxTemplateParamValueBytes caps a single template-param value. Generous for a
	// prompt-sized free-text value while bounding env/argv/memory bloat.
	MaxTemplateParamValueBytes = 8192

	// MaxTemplateParamKeyBytes caps a template-param key length.
	MaxTemplateParamKeyBytes = 128
)

// templateParamKeyRe is the required key grammar: an uppercase leading letter
// followed by uppercase letters, digits, or underscores. This matches the WG-045
// token grammar (minus the __ delimiters) and the POSIX environment-name shape.
var templateParamKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ErrInvalidTemplateParam is returned by ValidateTemplateParams when a key or value
// fails ingestion validation. It names the offending key so the submitter can fix it.
type ErrInvalidTemplateParam struct {
	// Key is the offending param key (may be empty if the key itself is empty).
	Key string
	// Reason is a human-readable description of the violation.
	Reason string
}

func (e *ErrInvalidTemplateParam) Error() string {
	return fmt.Sprintf("invalid template param %q: %s", e.Key, e.Reason)
}

// ValidateTemplateParams enforces ingestion hygiene on launch template params
// (WG-045). It is called at the queue-submit RPC boundary (fail-fast, before
// persist) and again at the workflow substitution chokepoint (backstop for the
// daemon-down local-persist path that bypasses the RPC).
//
// It REJECTS:
//   - a key that is empty, longer than MaxTemplateParamKeyBytes, or does not match
//     ^[A-Z][A-Z0-9_]*$ (hygiene; bounds blast radius even if a key were ever
//     re-spliced);
//   - a value containing a NUL, newline, carriage return, tab, or any other ASCII
//     or Unicode control character (these are never legitimate and are the
//     highest-leverage injection primitives — a newline is a shell command
//     separator);
//   - a value longer than MaxTemplateParamValueBytes.
//
// A nil or empty map is valid (returns nil). The first offending key is reported.
// NOTE: shell metacharacters in values are deliberately NOT rejected — neutralising
// them is the job of the post-parse shell-quoting close, and over-rejecting here
// would break legitimate free-text prompt/goal params.
func ValidateTemplateParams(params map[string]string) error {
	for k, v := range params {
		if k == "" || len(k) > MaxTemplateParamKeyBytes || !templateParamKeyRe.MatchString(k) {
			return &ErrInvalidTemplateParam{
				Key:    k,
				Reason: fmt.Sprintf("key must match ^[A-Z][A-Z0-9_]*$ and be 1..%d bytes", MaxTemplateParamKeyBytes),
			}
		}
		if len(v) > MaxTemplateParamValueBytes {
			return &ErrInvalidTemplateParam{
				Key:    k,
				Reason: fmt.Sprintf("value exceeds the %d-byte cap (got %d bytes)", MaxTemplateParamValueBytes, len(v)),
			}
		}
		for _, r := range v {
			if unicode.IsControl(r) {
				return &ErrInvalidTemplateParam{
					Key:    k,
					Reason: fmt.Sprintf("value contains a control character (U+%04X) which is not permitted", r),
				}
			}
		}
	}
	return nil
}
