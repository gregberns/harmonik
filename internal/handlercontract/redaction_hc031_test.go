package handlercontract_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestRedactionHC031_SecretNamedFieldsAreRedacted verifies that every field
// name matching the HC-031 common-prefix regex is replaced with
// handlercontract.RedactedSentinel in the returned map.
//
// Input keys are drawn from redactionFixtureSecretNamedFieldNames (defined in
// redaction_hc028_test.go), which covers all five branches of the HC-031 regex.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_SecretNamedFieldsAreRedacted(t *testing.T) {
	t.Parallel()

	for _, fieldName := range redactionFixtureSecretNamedFieldNames {
		t.Run(fieldName, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{
				fieldName: "super-secret-value",
			}

			got := handlercontract.RedactByFieldName(payload)

			v, ok := got[fieldName]
			if !ok {
				t.Fatalf("RedactByFieldName: key %q missing from output map", fieldName)
			}
			if v != handlercontract.RedactedSentinel {
				t.Errorf(
					"RedactByFieldName(%q): got value %q, want %q (HC-031)",
					fieldName, v, handlercontract.RedactedSentinel,
				)
			}
		})
	}
}

// TestRedactionHC031_SafeFieldsArePreserved verifies that field names that do
// NOT match the HC-031 regex are copied unchanged into the output map.
//
// Field names are drawn from redactionFixtureSafePayload (defined in
// redaction_hc028_test.go): node_id, run_id, status, exit_code, agent_type.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_SafeFieldsArePreserved(t *testing.T) {
	t.Parallel()

	safePayload := map[string]any{
		"node_id":    "node-abc-123",
		"run_id":     "run-xyz-456",
		"status":     "SUCCESS",
		"exit_code":  0,
		"agent_type": "claude",
		"worker_id":  "w-001",
	}

	got := handlercontract.RedactByFieldName(safePayload)

	for k, want := range safePayload {
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			v, ok := got[k]
			if !ok {
				t.Fatalf("RedactByFieldName: safe key %q missing from output map", k)
			}
			if v != want {
				t.Errorf("RedactByFieldName(%q): got %v, want %v; safe fields MUST NOT be redacted (HC-031)", k, v, want)
			}
		})
	}
}

// TestRedactionHC031_MixedPayloadPartialRedaction verifies that a map
// containing both secret-named and safe-named fields is handled correctly:
// secret-named fields are replaced with RedactedSentinel and safe-named fields
// retain their original values.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_MixedPayloadPartialRedaction(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"node_id":  "node-abc-123",
		"token":    "tok-super-secret",
		"run_id":   "run-xyz-456",
		"password": "hunter2",
		"status":   "RUNNING",
	}

	got := handlercontract.RedactByFieldName(payload)

	for _, secretKey := range []string{"token", "password"} {
		v, ok := got[secretKey]
		if !ok {
			t.Errorf("RedactByFieldName: secret key %q missing from output", secretKey)
			continue
		}
		if v != handlercontract.RedactedSentinel {
			t.Errorf("RedactByFieldName(%q) = %v, want %q", secretKey, v, handlercontract.RedactedSentinel)
		}
	}

	for _, tc := range []struct {
		key  string
		want any
	}{
		{"node_id", "node-abc-123"},
		{"run_id", "run-xyz-456"},
		{"status", "RUNNING"},
	} {
		v, ok := got[tc.key]
		if !ok {
			t.Errorf("RedactByFieldName: safe key %q missing from output", tc.key)
			continue
		}
		if v != tc.want {
			t.Errorf("RedactByFieldName(%q) = %v, want %v", tc.key, v, tc.want)
		}
	}
}

// TestRedactionHC031_NilPayloadReturnsNil verifies that RedactByFieldName
// returns nil (not a panic) when given a nil map.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_NilPayloadReturnsNil(t *testing.T) {
	t.Parallel()
	got := handlercontract.RedactByFieldName(nil)
	if got != nil {
		t.Errorf("RedactByFieldName(nil) = %v, want nil", got)
	}
}

// TestRedactionHC031_EmptyPayloadReturnsEmpty verifies that RedactByFieldName
// returns a non-nil empty map when given an empty input map.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_EmptyPayloadReturnsEmpty(t *testing.T) {
	t.Parallel()
	got := handlercontract.RedactByFieldName(map[string]any{})
	if got == nil {
		t.Error("RedactByFieldName(empty) returned nil, want non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("RedactByFieldName(empty) returned map with %d entries, want 0", len(got))
	}
}

// TestRedactionHC031_ReturnIsNewMap verifies that RedactByFieldName does not
// mutate the input map. The caller retains the original payload unchanged.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_ReturnIsNewMap(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"secret": "original-value",
		"run_id": "run-xyz",
	}
	originalSecret := original["secret"]

	got := handlercontract.RedactByFieldName(original)

	if &got == &original {
		t.Error("RedactByFieldName returned the same map pointer; input MUST NOT be mutated")
	}

	if original["secret"] != originalSecret {
		t.Errorf("RedactByFieldName mutated input map: original[\"secret\"] = %v, want %v",
			original["secret"], originalSecret)
	}

	if got["secret"] != handlercontract.RedactedSentinel {
		t.Errorf("RedactByFieldName result[\"secret\"] = %v, want %q",
			got["secret"], handlercontract.RedactedSentinel)
	}
}

// TestRedactionHC031_SentinelMatchesFixture verifies that
// handlercontract.RedactedSentinel equals the fixture constant declared in
// redaction_hc028_test.go.
//
// Both constants must equal `"<redacted>"` per §4.7.HC-031. If they diverge,
// the fixture and the implementation are out of sync.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_SentinelMatchesFixture(t *testing.T) {
	t.Parallel()

	if handlercontract.RedactedSentinel != redactionFixtureRedactedSentinel {
		t.Errorf(
			"handlercontract.RedactedSentinel (%q) != redactionFixtureRedactedSentinel (%q); "+
				"implementation and fixture must agree on the literal per §4.7.HC-031",
			handlercontract.RedactedSentinel, redactionFixtureRedactedSentinel,
		)
	}
}

// TestRedactionHC031_CaseInsensitiveMatch verifies that upper-case and
// mixed-case variants of the HC-031 trigger words are redacted.
//
// The spec regex has the (?i) flag; a field named "SECRET", "Token", or
// "PASSWORD" must be treated identically to "secret", "token", "password".
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedactionHC031_CaseInsensitiveMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fieldName string
		desc      string
	}{
		{"SECRET", "all-caps secret"},
		{"Token", "title-case token"},
		{"PASSWORD", "all-caps password"},
		{"Api_Key", "mixed-case api_key"},
		{"AUTH", "all-caps auth"},
		{"API-KEY", "all-caps api-key hyphen form"},
		{"APIKEY", "all-caps apikey no-separator form"},
	}

	for _, tc := range cases {
		t.Run(tc.fieldName, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{tc.fieldName: "some-value"}
			got := handlercontract.RedactByFieldName(payload)

			v, ok := got[tc.fieldName]
			if !ok {
				t.Fatalf("RedactByFieldName: key %q missing from output", tc.fieldName)
			}
			if v != handlercontract.RedactedSentinel {
				t.Errorf(
					"RedactByFieldName(%q) [%s]: got %v, want %q; HC-031 regex is case-insensitive",
					tc.fieldName, tc.desc, v, handlercontract.RedactedSentinel,
				)
			}
		})
	}
}
