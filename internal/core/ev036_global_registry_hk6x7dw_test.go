package core

import (
	"testing"
)

// ev036_global_registry_hk6x7dw_test.go — regression guard for hk-6x7dw.
//
// Before hk-6x7dw, StaleVerdictPayload had a field named SnapshotToken whose
// name matched the EV-036 secret-prefix regex (HC-031 matches "Token"). The
// field was renamed to Snapshot in commit 1ff0c86. This test binds the global
// registry against ScanRegisteredPayloadsForSecretFields to prevent the
// violation from being reintroduced.
//
// Spec ref: event-model.md §4.10 EV-036.
// Bead ref: hk-6x7dw.

// TestHK6X7DW_EV036_GlobalRegistryClean verifies that every payload type
// registered in the global event-type registry passes the EV-036 secret-prefix
// scan. Any exported struct field whose name matches the secret-prefix rule
// (secret, token, password, api_key, auth — case-insensitive) would cause the
// daemon to refuse to start; this test catches that class of regression at CI
// time.
//
// Spec ref: event-model.md §4.10 EV-036.
// Bead ref: hk-6x7dw (StaleVerdictPayload.SnapshotToken → Snapshot rename).
func TestHK6X7DW_EV036_GlobalRegistryClean(t *testing.T) {
	if err := ScanRegisteredPayloadsForSecretFields(); err != nil {
		t.Errorf("EV-036 violation in global event registry (hk-6x7dw regression): %v", err)
	}
}
