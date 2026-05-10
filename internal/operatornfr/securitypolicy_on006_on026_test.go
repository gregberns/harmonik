package operatornfr_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// TestON006_MVHBinarySigningPolicy_SigningNotRequired verifies that the MVH
// BinarySigningPolicy declares signing NOT required.
//
// Spec ref: operator-nfr.md §4.2 ON-006 — "Conforming MVH implementations
// MUST NOT be required to verify signatures beyond the commit-hash match."
func TestON006_MVHBinarySigningPolicy_SigningNotRequired(t *testing.T) {
	t.Parallel()

	p := operatornfr.MVHBinarySigningPolicy
	if p.SigningRequired {
		t.Error("ON-006: MVHBinarySigningPolicy.SigningRequired = true; binary signing MUST NOT be required for MVH conformance")
	}
}

// TestON006_MVHBinarySigningPolicy_MVHEraIsTrue verifies that the MVH policy
// is flagged as applying to the MVH era.
//
// Spec ref: operator-nfr.md §4.2 ON-006.
func TestON006_MVHBinarySigningPolicy_MVHEraIsTrue(t *testing.T) {
	t.Parallel()

	p := operatornfr.MVHBinarySigningPolicy
	if !p.MVHEra {
		t.Error("ON-006: MVHBinarySigningPolicy.MVHEra = false; the canonical policy MUST be tagged as MVH-era")
	}
}

// TestON006_SigningIsAdditive verifies the logical additive relationship: post-MVH
// signing (SigningRequired=true) does NOT affect the MVH-era policy.
//
// Spec ref: operator-nfr.md §4.2 ON-006 — "Post-MVH introduction of signing
// is additive and does NOT invalidate MVH conformance."
func TestON006_SigningIsAdditive(t *testing.T) {
	t.Parallel()

	// Post-MVH policy (hypothetical): signing may be required.
	postMVH := operatornfr.BinarySigningPolicy{
		SigningRequired: true,
		MVHEra:          false,
	}

	// MVH-era policy: signing is never required.
	mvh := operatornfr.MVHBinarySigningPolicy

	// Post-MVH requiring signing MUST NOT make MVH fail-in-conformance.
	// The MVH policy is unchanged by post-MVH additive requirements.
	if mvh.SigningRequired {
		t.Error("ON-006: MVH policy shows signing required; post-MVH additive change must not retroactively affect MVH conformance")
	}
	if !postMVH.SigningRequired {
		t.Error("ON-006: post-MVH policy must be constructible with signing required; additive means it CAN be added post-MVH")
	}
}

// TestON026_PromptInjectionOwnerHandler_Valid verifies that the handler
// owner constant is valid.
//
// Spec ref: operator-nfr.md §4.7 ON-026 — "Prompt-injection defense is
// handler-owned."
func TestON026_PromptInjectionOwnerHandler_Valid(t *testing.T) {
	t.Parallel()

	if !operatornfr.PromptInjectionOwnerHandler.Valid() {
		t.Error("ON-026: PromptInjectionOwnerHandler.Valid() = false; handler is the declared prompt-injection owner")
	}
}

// TestON026_PromptInjectionOwnerHandler_IsHandler verifies that the constant
// value is "handler".
//
// Spec ref: operator-nfr.md §4.7 ON-026.
func TestON026_PromptInjectionOwnerHandler_IsHandler(t *testing.T) {
	t.Parallel()

	if string(operatornfr.PromptInjectionOwnerHandler) != "handler" {
		t.Errorf("ON-026: PromptInjectionOwnerHandler = %q, want %q",
			operatornfr.PromptInjectionOwnerHandler, "handler")
	}
}

// TestON026_PromptInjectionOwner_InvalidRejected verifies that unknown owner
// strings are rejected by Valid().
//
// Spec ref: operator-nfr.md §4.7 ON-026.
func TestON026_PromptInjectionOwner_InvalidRejected(t *testing.T) {
	t.Parallel()

	invalid := []operatornfr.PromptInjectionOwner{"", "daemon", "orchestrator", "unknown"}
	for _, o := range invalid {
		o := o
		t.Run("invalid/"+string(o), func(t *testing.T) {
			t.Parallel()
			if o.Valid() {
				t.Errorf("ON-026: PromptInjectionOwner(%q).Valid() = true, want false; only 'handler' is the declared owner", o)
			}
		})
	}
}
