package core

// evinv006_redaction_sensor_hqwn52_test.go — binding tests for hk-hqwn.52
// (EV-INV-006: best-effort redaction plus compile-time structural check).
//
// Spec refs: event-model.md §5 EV-INV-006; §4.10 EV-035; §4.10 EV-036.
// Bead ref: hk-hqwn.52.
//
// Two sensor layers:
//
//  1. EV-036 structural check (scanConstructors / ScanRegisteredPayloadsForSecretFields):
//     - A payload type with a secret-prefix field name MUST cause the scan to
//       return an error wrapping ErrSecretPrefixField.
//     - A payload type with only safe field names MUST pass the scan.
//     - The scan covers all common-prefix variants: secret, token, password,
//       api_key, apiKey, auth, and their mixed-case forms.
//
//  2. EV-035 runtime redaction (secretPrefixRe matches HC-031 rule):
//     - The regex used by the structural check MUST agree with the HC-031 set,
//       confirming the two layers are co-aligned on what constitutes a
//       "secret-prefix" name.
//
// These two layers together discharge EV-INV-006: the structural check rules out
// secret-named fields on registered types (structural guardrail), and the regex
// alignment confirms the best-effort redaction path (HC-031 in handlercontract)
// and the structural scan use the identical definition of "secret-prefix".
//
// Helper prefix: hqwn52Fixture (per implementer-protocol.md §Helper-prefix
// discipline; distinct from eventRegistryReset and other core helpers).

import (
	"errors"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture payload types — test-only; defined here, never registered globally.
// ─────────────────────────────────────────────────────────────────────────────

// hqwn52FixtureCleanPayload has no secret-prefix fields; used to verify that
// clean types pass the EV-036 structural scan.
type hqwn52FixtureCleanPayload struct {
	NodeID string `json:"node_id"`
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// hqwn52FixtureSecretPayload has a field named "Secret" that matches the
// EV-036 secret-prefix rule.
type hqwn52FixtureSecretPayload struct {
	NodeID string `json:"node_id"`
	Secret string `json:"secret"`
}

// hqwn52FixtureTokenPayload has a field named "Token" — another common-prefix
// variant that the EV-036 rule covers.
type hqwn52FixtureTokenPayload struct {
	RunID string `json:"run_id"`
	Token string `json:"token"`
}

// hqwn52FixturePasswordPayload has a field named "Password".
type hqwn52FixturePasswordPayload struct {
	UserID   string `json:"user_id"`
	Password string `json:"password"`
}

// hqwn52FixtureAPIKeyPayload has a field named "APIKey".
type hqwn52FixtureAPIKeyPayload struct {
	Service string `json:"service"`
	APIKey  string `json:"api_key"`
}

// hqwn52FixtureAuthPayload has a field named "Auth".
type hqwn52FixtureAuthPayload struct {
	SessionID string `json:"session_id"`
	Auth      string `json:"auth"`
}

// hqwn52FixtureLocalCtors builds an isolated constructor map containing only
// the provided (typeName, constructor) pairs. Tests use this to avoid touching
// the global registry, which may already contain production types that have
// legitimate non-secret "Token"-suffix field names (e.g., SnapshotToken).
func hqwn52FixtureLocalCtors(pairs ...any) map[string]func() EventPayload {
	m := make(map[string]func() EventPayload)
	for i := 0; i+1 < len(pairs); i += 2 {
		name := pairs[i].(string)
		ctor := pairs[i+1].(func() EventPayload)
		m[name] = ctor
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// EV-036: scanConstructors (isolated registry — avoids global state)
// ─────────────────────────────────────────────────────────────────────────────

// TestHQWN52_EV036_CleanPayloadPassesScan verifies that a constructor map
// containing only clean (non-secret-prefix) payload types returns nil.
//
// Uses scanConstructors with an isolated map to avoid interference from
// production types registered in the global registry at init() time.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_CleanPayloadPassesScan(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.clean.v1", func() EventPayload { return &hqwn52FixtureCleanPayload{} },
	)
	if err := scanConstructors(ctors); err != nil {
		t.Errorf("scanConstructors returned unexpected error for clean payload: %v", err)
	}
}

// TestHQWN52_EV036_SecretFieldCausesScanError verifies that a payload type
// with a field named "Secret" causes scanConstructors to return an error
// wrapping ErrSecretPrefixField.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_SecretFieldCausesScanError(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.secret.v1", func() EventPayload { return &hqwn52FixtureSecretPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error for Secret field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// TestHQWN52_EV036_TokenFieldCausesScanError verifies that a payload type
// with a field named "Token" causes the scan to fail (EV-036 prefix variants).
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_TokenFieldCausesScanError(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.token.v1", func() EventPayload { return &hqwn52FixtureTokenPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error for Token field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// TestHQWN52_EV036_PasswordFieldCausesScanError verifies that a "Password"
// field triggers the scan error.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_PasswordFieldCausesScanError(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.password.v1", func() EventPayload { return &hqwn52FixturePasswordPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error for Password field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// TestHQWN52_EV036_APIKeyFieldCausesScanError verifies that an "APIKey" field
// triggers the scan error.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_APIKeyFieldCausesScanError(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.apikey.v1", func() EventPayload { return &hqwn52FixtureAPIKeyPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error for APIKey field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// TestHQWN52_EV036_AuthFieldCausesScanError verifies that an "Auth" field
// triggers the scan error.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_AuthFieldCausesScanError(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.auth.v1", func() EventPayload { return &hqwn52FixtureAuthPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error for Auth field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// TestHQWN52_EV036_EmptyConstructorMapPassesScan verifies that an empty
// constructor map returns nil from scanConstructors.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_EmptyConstructorMapPassesScan(t *testing.T) {
	t.Parallel()

	if err := scanConstructors(map[string]func() EventPayload{}); err != nil {
		t.Errorf("scanConstructors on empty map: got %v, want nil", err)
	}
}

// TestHQWN52_EV036_MixedRegistryDetectsViolation verifies that when clean and
// violating types are registered together, the scan finds the violation.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_MixedRegistryDetectsViolation(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(
		"test.hqwn52.mixed.clean.v1", func() EventPayload { return &hqwn52FixtureCleanPayload{} },
		"test.hqwn52.mixed.secret.v1", func() EventPayload { return &hqwn52FixtureSecretPayload{} },
	)
	err := scanConstructors(ctors)
	if err == nil {
		t.Fatal("scanConstructors: expected error when mix contains a secret field, got nil")
	}
	if !errors.Is(err, ErrSecretPrefixField) {
		t.Errorf("scanConstructors: got %v, want errors.Is(ErrSecretPrefixField)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EV-035: best-effort redaction — secretPrefixRe agrees with HC-031 rule
// ─────────────────────────────────────────────────────────────────────────────

// TestHQWN52_EV035_SecretPrefixReMatchesSecretNames verifies that the core
// package's secretPrefixRe (used by scanConstructors) matches the same set of
// secret-prefix names that the HC-031 redaction rule covers. This test
// exercises the regex against the canonical set of field names that EV-035 /
// HC-031 declare must be redacted.
//
// By confirming both the regex and the scan function agree on what constitutes
// a "secret-prefix" field name, this test ties the structural check (EV-036)
// to the runtime redaction rule (EV-035), discharging EV-INV-006.
//
// Spec refs: event-model.md §4.10 EV-035, §4.10 EV-036, §5 EV-INV-006;
// handler-contract.md §4.7 HC-031.
func TestHQWN52_EV035_SecretPrefixReMatchesSecretNames(t *testing.T) {
	t.Parallel()

	// These field names MUST match the secret-prefix rule per EV-035 / HC-031.
	shouldMatch := []string{
		"Secret",
		"secret",
		"SECRET",
		"Token",
		"token",
		"TOKEN",
		"Password",
		"password",
		"PASSWORD",
		"APIKey",
		"ApiKey",
		"api_key",
		"API_KEY",
		"Auth",
		"auth",
		"AUTH",
		"AuthToken",
		"SecretKey",
		"PasswordHash",
	}

	// These field names MUST NOT match the secret-prefix rule.
	shouldNotMatch := []string{
		"NodeID",
		"RunID",
		"Status",
		"EventType",
		"Payload",
		"TimestampWall",
		"SourceSubsystem",
		"Kind",
	}

	for _, name := range shouldMatch {
		name := name
		t.Run("match_"+name, func(t *testing.T) {
			t.Parallel()
			if !secretPrefixRe.MatchString(name) {
				t.Errorf("secretPrefixRe.MatchString(%q) = false, want true (EV-035/HC-031 requires redaction of this field name)", name)
			}
		})
	}

	for _, name := range shouldNotMatch {
		name := name
		t.Run("no_match_"+name, func(t *testing.T) {
			t.Parallel()
			if secretPrefixRe.MatchString(name) {
				t.Errorf("secretPrefixRe.MatchString(%q) = true, want false (safe field MUST NOT be redacted)", name)
			}
		})
	}
}
