package handlercontract_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// schemaChecker — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.40)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixture types
// ─────────────────────────────────────────────────────────────────────────────

// schemaCheckerFixtureSafePayload is a struct with no field names matching the
// HC-031 regex — CheckPayloadSchema MUST return nil for this type.
type schemaCheckerFixtureSafePayload struct {
	NodeID    string `json:"node_id"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	AgentType string `json:"agent_type"`
}

// schemaCheckerFixtureSecretFieldPayload has a field whose JSON name matches
// the HC-031 regex ("secret") — CheckPayloadSchema MUST return non-nil error.
type schemaCheckerFixtureSecretFieldPayload struct {
	NodeID string `json:"node_id"`
	Secret string `json:"secret"`
}

// schemaCheckerFixtureTokenFieldPayload has a field with JSON tag "token".
type schemaCheckerFixtureTokenFieldPayload struct {
	NodeID string `json:"node_id"`
	Token  string `json:"token"`
}

// schemaCheckerFixturePasswordFieldPayload has a field with JSON tag "password".
type schemaCheckerFixturePasswordFieldPayload struct {
	NodeID   string `json:"node_id"`
	Password string `json:"password"`
}

// schemaCheckerFixtureAPIKeyUnderscorePayload has field with JSON tag "api_key".
type schemaCheckerFixtureAPIKeyUnderscorePayload struct {
	NodeID string `json:"node_id"`
	APIKey string `json:"api_key"`
}

// schemaCheckerFixtureAPIKeyHyphenPayload has field with JSON tag "api-key".
type schemaCheckerFixtureAPIKeyHyphenPayload struct {
	NodeID string `json:"node_id"`
	APIKey string `json:"api-key"`
}

// schemaCheckerFixtureAuthFieldPayload has a field with JSON tag "auth".
type schemaCheckerFixtureAuthFieldPayload struct {
	NodeID string `json:"node_id"`
	Auth   string `json:"auth"`
}

// schemaCheckerFixtureOmittedSecretField has json:"-" on the secret field — the
// field is omitted from JSON output and MUST be skipped by the schema checker.
type schemaCheckerFixtureOmittedSecretField struct {
	NodeID string `json:"node_id"`
	Secret string `json:"-"`
}

// schemaCheckerFixtureNoJSONTag has a field named "Secret" with no json tag —
// the effective JSON name is the Go field name "Secret", which matches HC-031.
type schemaCheckerFixtureNoJSONTag struct {
	NodeID string `json:"node_id"`
	Secret string // no json tag → effective name = "Secret"
}

// schemaCheckerFixtureNestedViolation has a nested struct with a violating field.
type schemaCheckerFixtureNestedViolation struct {
	NodeID string                                 `json:"node_id"`
	Inner  schemaCheckerFixtureSecretFieldPayload `json:"inner"`
}

// schemaCheckerFixtureNestedSafe has a nested struct with no violations.
type schemaCheckerFixtureNestedSafe struct {
	NodeID string                          `json:"node_id"`
	Inner  schemaCheckerFixtureSafePayload `json:"inner"`
}

// schemaCheckerFixturePointerField has a pointer-to-struct field.
type schemaCheckerFixturePointerField struct {
	NodeID string                                  `json:"node_id"`
	Inner  *schemaCheckerFixtureSecretFieldPayload `json:"inner"`
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — CheckPayloadSchema: safe payloads return nil
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_SafePayload_ReturnsNil verifies that a payload struct with
// no field names matching the HC-031 regex causes CheckPayloadSchema to return
// nil.
func TestSchemaChecker_SafePayload_ReturnsNil(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureSafePayload{}); err != nil {
		t.Errorf("CheckPayloadSchema(safe): got error %v, want nil", err)
	}
}

// TestSchemaChecker_SafePayloadPtr_ReturnsNil verifies that passing a pointer
// to a safe struct also returns nil (pointer dereference works).
func TestSchemaChecker_SafePayloadPtr_ReturnsNil(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(&schemaCheckerFixtureSafePayload{}); err != nil {
		t.Errorf("CheckPayloadSchema(ptr-to-safe): got error %v, want nil", err)
	}
}

// TestSchemaChecker_NestedSafe_ReturnsNil verifies that a struct with a nested
// safe struct also passes the check.
func TestSchemaChecker_NestedSafe_ReturnsNil(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureNestedSafe{}); err != nil {
		t.Errorf("CheckPayloadSchema(nested-safe): got error %v, want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — CheckPayloadSchema: violating field names return error
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_SecretField_ReturnsError verifies that a field with JSON
// tag "secret" causes CheckPayloadSchema to return a non-nil error.
func TestSchemaChecker_SecretField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureSecretFieldPayload{}); err == nil {
		t.Error("CheckPayloadSchema(secret field): got nil, want non-nil error (HC-033)")
	}
}

// TestSchemaChecker_TokenField_ReturnsError verifies JSON tag "token".
func TestSchemaChecker_TokenField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureTokenFieldPayload{}); err == nil {
		t.Error("CheckPayloadSchema(token field): got nil, want non-nil error (HC-033)")
	}
}

// TestSchemaChecker_PasswordField_ReturnsError verifies JSON tag "password".
func TestSchemaChecker_PasswordField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixturePasswordFieldPayload{}); err == nil {
		t.Error("CheckPayloadSchema(password field): got nil, want non-nil error (HC-033)")
	}
}

// TestSchemaChecker_APIKeyUnderscoreField_ReturnsError verifies JSON tag "api_key".
func TestSchemaChecker_APIKeyUnderscoreField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureAPIKeyUnderscorePayload{}); err == nil {
		t.Error("CheckPayloadSchema(api_key field): got nil, want non-nil error (HC-033)")
	}
}

// TestSchemaChecker_APIKeyHyphenField_ReturnsError verifies JSON tag "api-key".
func TestSchemaChecker_APIKeyHyphenField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureAPIKeyHyphenPayload{}); err == nil {
		t.Error("CheckPayloadSchema(api-key field): got nil, want non-nil error (HC-033)")
	}
}

// TestSchemaChecker_AuthField_ReturnsError verifies JSON tag "auth".
func TestSchemaChecker_AuthField_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureAuthFieldPayload{}); err == nil {
		t.Error("CheckPayloadSchema(auth field): got nil, want non-nil error (HC-033)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — omitted fields (json:"-") are not flagged
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_JsonDashOmitsSensitiveField_ReturnsNil verifies that a
// field with json:"-" is excluded from the check even when the field name would
// otherwise violate HC-031.
//
// A field tagged json:"-" is omitted from JSON output and is therefore not
// observable in the event payload; excluding it from the schema check is correct.
func TestSchemaChecker_JsonDashOmitsSensitiveField_ReturnsNil(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureOmittedSecretField{}); err != nil {
		t.Errorf("CheckPayloadSchema(json:\"-\" field): got error %v, want nil (omitted field not observable)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — Go field names without json tags are checked
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_GoFieldNameNoTag_ViolationDetected verifies that a struct
// field with no json tag whose Go field name matches HC-031 is flagged.
//
// When there is no json tag, the effective JSON name is the Go field name.
// A field named "Secret" has effective JSON name "Secret", which matches
// the case-insensitive HC-031 regex.
func TestSchemaChecker_GoFieldNameNoTag_ViolationDetected(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureNoJSONTag{}); err == nil {
		t.Error("CheckPayloadSchema(Go field name 'Secret' no tag): got nil, want non-nil error (HC-033)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — recursion into nested structs
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_NestedViolation_ReturnsError verifies that a struct with a
// nested struct containing a violating field is flagged.
func TestSchemaChecker_NestedViolation_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixtureNestedViolation{}); err == nil {
		t.Error("CheckPayloadSchema(nested violation): got nil, want non-nil error (HC-033 recursion)")
	}
}

// TestSchemaChecker_PointerField_ViolationDetected verifies that recursion into
// pointer-to-struct fields works correctly.
func TestSchemaChecker_PointerField_ViolationDetected(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(schemaCheckerFixturePointerField{}); err == nil {
		t.Error("CheckPayloadSchema(pointer field with nested violation): got nil, want non-nil error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — input validation
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_NilPrototype_ReturnsError verifies that passing nil as the
// prototype returns an error (not a panic).
func TestSchemaChecker_NilPrototype_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema(nil); err == nil {
		t.Error("CheckPayloadSchema(nil): got nil, want non-nil error")
	}
}

// TestSchemaChecker_NonStruct_ReturnsError verifies that passing a non-struct
// value (e.g., a string) returns an error.
func TestSchemaChecker_NonStruct_ReturnsError(t *testing.T) {
	t.Parallel()

	if err := handlercontract.CheckPayloadSchema("not-a-struct"); err == nil {
		t.Error("CheckPayloadSchema(string): got nil, want non-nil error (must be struct)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-033 — sensor: existing fixture types from redaction_hc028_test.go
// ─────────────────────────────────────────────────────────────────────────────

// TestSchemaChecker_HC028FixtureSchemaViolation_ReturnsError verifies that
// redactionFixtureSchemaViolation (defined in redaction_hc028_test.go) is
// rejected by CheckPayloadSchema — this is the negative-case input called out
// in that fixture file's commentary.
//
// Spec ref: specs/handler-contract.md §4.7.HC-033.
func TestSchemaChecker_HC028FixtureSchemaViolation_ReturnsError(t *testing.T) {
	t.Parallel()

	// redactionFixtureSchemaViolation has a field with json tag "password"
	// which matches HC-031.  Defined in redaction_hc028_test.go.
	if err := handlercontract.CheckPayloadSchema(redactionFixtureSchemaViolation{}); err == nil {
		t.Error(
			"CheckPayloadSchema(redactionFixtureSchemaViolation): got nil, want non-nil error; " +
				"the fixture was designed as the negative-case input to HC-033 (§4.7.HC-033)",
		)
	}
}

// TestSchemaChecker_HC028FixtureSafePayload_ReturnsNil verifies that
// redactionFixtureSafePayload (defined in redaction_hc028_test.go) passes
// CheckPayloadSchema — it is the expected positive (safe) case.
//
// Spec ref: specs/handler-contract.md §4.7.HC-033.
func TestSchemaChecker_HC028FixtureSafePayload_ReturnsNil(t *testing.T) {
	t.Parallel()

	// redactionFixtureSafePayload has only safe fields (node_id, run_id, etc.).
	// Defined in redaction_hc028_test.go.
	if err := handlercontract.CheckPayloadSchema(redactionFixtureSafePayload{}); err != nil {
		t.Errorf("CheckPayloadSchema(redactionFixtureSafePayload): got %v, want nil", err)
	}
}
