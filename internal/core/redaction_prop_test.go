package core

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var sensitiveKeyPrefixes = []string{"secret", "token", "password", "api_key", "api-key", "apikey", "auth"}

func genSensitiveKey(rt *rapid.T, label string) string {
	rt.Helper()
	prefix := rapid.SampledFrom(sensitiveKeyPrefixes).Draw(rt, label+"_prefix")
	suffix := rapid.String().Draw(rt, label+"_suffix")
	return prefix + suffix
}

func genSafeKey(rt *rapid.T, label string) string {
	rt.Helper()
	return rapid.StringMatching(`^[bcdfghjklmnpqruvwxyz][bcdfghjklmnpqruvwxyz0-9]{0,15}$`).Draw(rt, label)
}

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

		secret := rapid.StringMatching(`^[A-Z]{4,12}$`).Draw(rt, "secret")
		re := regexp.MustCompile(regexp.QuoteMeta(secret))
		r.RegisterPattern("subsys", []*regexp.Regexp{re})

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

		re := regexp.MustCompile(`ZZZZZZZZZZZZZZZZZZZZ`) // effectively unmatchable
		r.RegisterPattern("subsys", []*regexp.Regexp{re})

		safeKey := genSafeKey(rt, "k")
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
