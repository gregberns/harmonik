package core

// Property tests for RedactByFieldName and RedactionRegistry using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. RedactByFieldName nil-safety: nil input yields nil output.
//
//  2. RedactByFieldName non-mutation: the original payload map is never mutated.
//
//  3. RedactByFieldName field-name redaction: any key matching the HC-031 regex
//     (secret|token|password|api[_-]?key|auth) yields RedactedSentinel in output.
//
//  4. RedactByFieldName pass-through: keys not matching the regex are copied
//     unchanged.
//
//  5. RedactionMiddleware nil-safety: nil payload yields nil output.
//
//  6. RedactionMiddleware non-mutation: the original payload map is never mutated.
//
//  7. RedactionMiddleware value-pattern redaction: any string value that matches a
//     registered pattern is replaced with RedactedSentinel; unmatched values pass
//     through.
//
// Spec refs: specs/handler-contract.md §4.7.HC-031, §4.7.HC-032.
// Bead ref: hk-djice.

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// sensitiveKeyPrefixes are key fragments that trigger HC-031 field-name redaction.
var sensitiveKeyPrefixes = []string{"secret", "token", "password", "api_key", "api-key", "apikey", "auth"}

// genSensitiveKey draws a key that must be redacted by HC-031.
func genSensitiveKey(rt *rapid.T, label string) string {
	rt.Helper()
	prefix := rapid.SampledFrom(sensitiveKeyPrefixes).Draw(rt, label+"_prefix")
	suffix := rapid.String().Draw(rt, label+"_suffix")
	return prefix + suffix
}

// genSafeKey draws a key that must NOT be redacted (no HC-031 prefix, case-insensitively).
func genSafeKey(rt *rapid.T, label string) string {
	rt.Helper()
	// Use rapid.StringMatching with a negative pattern: keys that contain none of
	// the sensitive prefixes (case-insensitive).  For simplicity generate from a
	// known-safe alphabet so the match is easy to verify without re-running the
	// regex inside the generator.
	return rapid.StringMatching(`^[bcdfghjklmnpqruvwxyz][bcdfghjklmnpqruvwxyz0-9]{0,15}$`).Draw(rt, label)
}

// genPayload draws a map[string]any with a mix of safe and sensitive keys and
// string values.
func genPayload(rt *rapid.T, label string) map[string]any {
	rt.Helper()
	size := rapid.IntRange(0, 8).Draw(rt, label+"_size")
	m := make(map[string]any, size)
	for i := 0; i < size; i++ {
		useSensitive := rapid.Bool().Draw(rt, label+"_sensitive")
		var k string
		if useSensitive {
			k = genSensitiveKey(rt, label+"_k")
		} else {
			k = genSafeKey(rt, label+"_k")
		}
		m[k] = rapid.String().Draw(rt, label+"_v")
	}
	return m
}

// copyPayload makes a shallow copy of a map[string]any for mutation detection.
func copyPayload(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// --- RedactByFieldName properties ---

func TestProp_RedactByFieldName_NilSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		got := RedactByFieldName(nil)
		if got != nil {
			rt.Errorf("RedactByFieldName(nil) = %v, want nil", got)
		}
	})
}

func TestProp_RedactByFieldName_NonMutation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		original := genPayload(rt, "p")
		snapshot := copyPayload(original)

		_ = RedactByFieldName(original)

		for k, wantV := range snapshot {
			if gotV, ok := original[k]; !ok {
				rt.Errorf("key %q was removed from original", k)
			} else if gotV != wantV {
				rt.Errorf("key %q mutated in original: was %v, now %v", k, wantV, gotV)
			}
		}
		if len(original) != len(snapshot) {
			rt.Errorf("original map length changed: was %d, now %d", len(snapshot), len(original))
		}
	})
}

func TestProp_RedactByFieldName_SensitiveKeysRedacted(t *testing.T) {
	hc031 := regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

	rapid.Check(t, func(rt *rapid.T) {
		payload := genPayload(rt, "p")
		out := RedactByFieldName(payload)

		for k := range payload {
			if hc031.MatchString(k) {
				if out[k] != RedactedSentinel {
					rt.Errorf("sensitive key %q not redacted: got %v", k, out[k])
				}
			}
		}
	})
}

func TestProp_RedactByFieldName_SafeKeysPassThrough(t *testing.T) {
	hc031 := regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

	rapid.Check(t, func(rt *rapid.T) {
		payload := genPayload(rt, "p")
		out := RedactByFieldName(payload)

		for k, v := range payload {
			if !hc031.MatchString(k) {
				if out[k] != v {
					rt.Errorf("safe key %q value changed: got %v, want %v", k, out[k], v)
				}
			}
		}
	})
}

// --- RedactionRegistry / RedactionMiddleware properties ---

func TestProp_RedactionMiddleware_NilSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := NewRedactionRegistry()
		got := r.RedactionMiddleware(nil)
		if got != nil {
			rt.Errorf("RedactionMiddleware(nil) = %v, want nil", got)
		}
	})
}

func TestProp_RedactionMiddleware_NonMutation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := NewRedactionRegistry()
		pattern := rapid.StringMatching(`^[a-z]{3,8}$`).Draw(rt, "pattern")
		re := regexp.MustCompile(regexp.QuoteMeta(pattern))
		r.RegisterPattern("test", []*regexp.Regexp{re})

		payload := genPayload(rt, "p")
		snapshot := copyPayload(payload)

		_ = r.RedactionMiddleware(payload)

		for k, wantV := range snapshot {
			if gotV, ok := payload[k]; !ok {
				rt.Errorf("key %q removed from original", k)
			} else if gotV != wantV {
				rt.Errorf("key %q mutated in original: was %v, now %v", k, wantV, gotV)
			}
		}
		if len(payload) != len(snapshot) {
			rt.Errorf("original map length changed: was %d, now %d", len(snapshot), len(payload))
		}
	})
}

func TestProp_RedactionMiddleware_RegisteredPatternRedactsValue(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := NewRedactionRegistry()

		// Pick a secret token string that we'll embed in some values.
		secret := rapid.StringMatching(`^[A-Z]{4,12}$`).Draw(rt, "secret")
		re := regexp.MustCompile(regexp.QuoteMeta(secret))
		r.RegisterPattern("subsys", []*regexp.Regexp{re})

		// Build a payload where at least one safe key has a value containing the
		// secret so the pattern fires on a value (not a field name).
		safeKey := genSafeKey(rt, "k")
		payload := map[string]any{
			safeKey: secret + "_suffix",
		}

		out := r.RedactionMiddleware(payload)

		if out[safeKey] != RedactedSentinel {
			rt.Errorf("value containing registered secret not redacted: got %v", out[safeKey])
		}
	})
}

func TestProp_RedactionMiddleware_UnmatchedValuePassesThrough(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := NewRedactionRegistry()

		// Register a pattern that will never match our generated safe values.
		re := regexp.MustCompile(`ZZZZZZZZZZZZZZZZZZZZ`) // effectively unmatchable
		r.RegisterPattern("subsys", []*regexp.Regexp{re})

		safeKey := genSafeKey(rt, "k")
		// Generate a value guaranteed not to contain the unmatchable literal.
		safeVal := rapid.StringMatching(`^[a-z0-9]{1,20}$`).Draw(rt, "v")
		payload := map[string]any{safeKey: safeVal}

		out := r.RedactionMiddleware(payload)

		if out[safeKey] != safeVal {
			rt.Errorf("unmatched value changed: got %v, want %v", out[safeKey], safeVal)
		}
	})
}

func TestProp_RedactionMiddleware_HC031FieldNamesAlwaysRedacted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		r := NewRedactionRegistry()
		// No value-level patterns registered; only HC-031 field-name rule applies.

		sensitiveKey := strings.ToLower(genSensitiveKey(rt, "k"))
		payload := map[string]any{
			sensitiveKey: rapid.String().Draw(rt, "v"),
		}

		out := r.RedactionMiddleware(payload)

		if out[sensitiveKey] != RedactedSentinel {
			rt.Errorf("HC-031 sensitive key %q not redacted: got %v", sensitiveKey, out[sensitiveKey])
		}
	})
}
