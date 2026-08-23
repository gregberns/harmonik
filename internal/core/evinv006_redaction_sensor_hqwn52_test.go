package core

import (
	"errors"
	"testing"
)

type hqwn52FixtureCleanPayload struct {
	NodeID string `json:"node_id"`
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type hqwn52FixtureSecretPayload struct {
	NodeID string `json:"node_id"`
	Secret string `json:"secret"`
}

type hqwn52FixtureTokenPayload struct {
	RunID string `json:"run_id"`
	Token string `json:"token"`
}

type hqwn52FixturePasswordPayload struct {
	UserID   string `json:"user_id"`
	Password string `json:"password"`
}

type hqwn52FixtureAPIKeyPayload struct {
	Service string `json:"service"`
	APIKey  string `json:"api_key"`
}

type hqwn52FixtureAuthPayload struct {
	SessionID string `json:"session_id"`
	Auth      string `json:"auth"`
}

func hqwn52FixtureLocalCtors(t *testing.T, pairs ...any) map[string]func() EventPayload {
	m := make(map[string]func() EventPayload)
	for i := 0; i+1 < len(pairs); i += 2 {
		name, nameOK := pairs[i].(string)
		if !nameOK {
			t.Fatal("hqwn52FixtureLocalCtors: fixture name must be a string")
		}
		ctor, ctorOK := pairs[i+1].(func() EventPayload)
		if !ctorOK {
			t.Fatal("hqwn52FixtureLocalCtors: fixture constructor must return EventPayload")
		}
		m[name] = ctor
	}
	return m
}

// TestHQWN52_EV036_CleanPayloadPassesScan verifies that a constructor map
// containing only clean (non-secret-prefix) payload types returns nil.
//
// Uses scanConstructors with an isolated map to avoid interference from
// production types registered in the global registry at init() time.
//
// Spec ref: event-model.md §4.10 EV-036.
func TestHQWN52_EV036_CleanPayloadPassesScan(t *testing.T) {
	t.Parallel()

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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

	ctors := hqwn52FixtureLocalCtors(t,
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
		t.Run("match_"+name, func(t *testing.T) {
			t.Parallel()
			if !secretPrefixRe.MatchString(name) {
				t.Errorf("secretPrefixRe.MatchString(%q) = false, want true (EV-035/HC-031 requires redaction of this field name)", name)
			}
		})
	}

	for _, name := range shouldNotMatch {
		t.Run("no_match_"+name, func(t *testing.T) {
			t.Parallel()
			if secretPrefixRe.MatchString(name) {
				t.Errorf("secretPrefixRe.MatchString(%q) = true, want false (safe field MUST NOT be redacted)", name)
			}
		})
	}
}
