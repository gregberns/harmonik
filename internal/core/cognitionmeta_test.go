package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// cognitionMetaFixture returns a fully-populated CognitionMeta with all fields
// set to valid non-zero values.
func cognitionMetaFixture(t *testing.T) CognitionMeta {
	t.Helper()
	usage := 512
	return CognitionMeta{
		DelegationPath: DelegationPath{
			Role:              "reviewer",
			ModelClass:        "reviewer-tier-1",
			InputSchemaRef:    "gate-input-v1",
			ResponseSchemaRef: "gate-response-v1",
			PromptTemplateRef: "gate-review-template-v1",
		},
		ModelResponseDigest: strings.Repeat("a", 64),
		TokenUsage:          &usage,
	}
}

// cognitionMetaFixtureNoTokenUsage returns a valid CognitionMeta with a nil
// TokenUsage (usage not reported by provider).
func cognitionMetaFixtureNoTokenUsage(t *testing.T) CognitionMeta {
	t.Helper()
	m := cognitionMetaFixture(t)
	m.TokenUsage = nil
	return m
}

// TestCognitionMeta_Valid_Full verifies that a fully-populated CognitionMeta
// passes Valid().
func TestCognitionMeta_Valid_Full(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	if !m.Valid() {
		t.Error("expected Valid() == true for fully-populated CognitionMeta, got false")
	}
}

// TestCognitionMeta_Valid_NilTokenUsage verifies that a CognitionMeta with nil
// TokenUsage passes Valid() (TokenUsage is optional).
func TestCognitionMeta_Valid_NilTokenUsage(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixtureNoTokenUsage(t)
	if !m.Valid() {
		t.Error("expected Valid() == true for CognitionMeta with nil TokenUsage, got false")
	}
}

// TestCognitionMeta_Valid_InvalidDelegationPath verifies that Valid() rejects a
// CognitionMeta whose DelegationPath is not well-formed.
func TestCognitionMeta_Valid_InvalidDelegationPath(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	m.DelegationPath.Role = ""
	if m.Valid() {
		t.Error("expected Valid() == false for CognitionMeta with invalid DelegationPath, got true")
	}
}

// TestCognitionMeta_Valid_EmptyModelResponseDigest verifies that Valid() rejects
// a CognitionMeta with an empty ModelResponseDigest.
func TestCognitionMeta_Valid_EmptyModelResponseDigest(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	m.ModelResponseDigest = ""
	if m.Valid() {
		t.Error("expected Valid() == false for empty ModelResponseDigest, got true")
	}
}

// TestCognitionMeta_Valid_NegativeTokenUsage verifies that Valid() rejects a
// CognitionMeta with a negative TokenUsage value.
func TestCognitionMeta_Valid_NegativeTokenUsage(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	neg := -1
	m.TokenUsage = &neg
	if m.Valid() {
		t.Error("expected Valid() == false for negative TokenUsage, got true")
	}
}

// TestCognitionMeta_Valid_ZeroTokenUsage verifies that a TokenUsage of zero is
// valid (zero tokens is a legitimate reported count).
func TestCognitionMeta_Valid_ZeroTokenUsage(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	zero := 0
	m.TokenUsage = &zero
	if !m.Valid() {
		t.Error("expected Valid() == true for zero TokenUsage, got false")
	}
}

// TestCognitionMeta_JSONRoundTrip verifies that a fully-populated CognitionMeta
// survives a JSON marshal/unmarshal round-trip with all fields intact.
func TestCognitionMeta_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := cognitionMetaFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got CognitionMeta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.DelegationPath != orig.DelegationPath {
		t.Errorf("DelegationPath: got %+v, want %+v", got.DelegationPath, orig.DelegationPath)
	}
	if got.ModelResponseDigest != orig.ModelResponseDigest {
		t.Errorf("ModelResponseDigest: got %q, want %q", got.ModelResponseDigest, orig.ModelResponseDigest)
	}
	if got.TokenUsage == nil || *got.TokenUsage != *orig.TokenUsage {
		t.Errorf("TokenUsage: got %v, want %v", got.TokenUsage, orig.TokenUsage)
	}
}

// TestCognitionMeta_JSONOmitsTokenUsageWhenNil verifies that when TokenUsage is
// nil the JSON output omits the token_usage key (omitempty).
func TestCognitionMeta_JSONOmitsTokenUsageWhenNil(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixtureNoTokenUsage(t)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := raw["token_usage"]; ok {
		t.Error("token_usage key present in JSON when TokenUsage is nil, want omitted")
	}
}

// TestCognitionMeta_JSONKeys verifies that the JSON field names match the
// snake_case wire shape declared in specs/control-points.md §6.1.6.
func TestCognitionMeta_JSONKeys(t *testing.T) {
	t.Parallel()

	m := cognitionMetaFixture(t)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"delegation_path", "model_response_digest", "token_usage"}
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}
