package core

// codexbillingguard_hktu48u_test.go — unit tests for CodexBillingGuardPayload
// and CodexBillingGuardOutcome (codex-harness C3/T11, hk-tu48u).

import "testing"

func TestCodexBillingGuardOutcome_Valid(t *testing.T) {
	t.Parallel()
	valid := []CodexBillingGuardOutcome{
		CodexBillingGuardMaterialized,
		CodexBillingGuardAllowed,
		CodexBillingGuardDenied,
	}
	for _, o := range valid {
		if !o.Valid() {
			t.Errorf("outcome %q: Valid() = false; want true", o)
		}
	}
	for _, o := range []CodexBillingGuardOutcome{"", "approve", "blocked", "chatgpt"} {
		if o.Valid() {
			t.Errorf("outcome %q: Valid() = true; want false", o)
		}
	}
}

func TestCodexBillingGuardPayload_Valid(t *testing.T) {
	t.Parallel()

	base := CodexBillingGuardPayload{
		BeadID:    "hk-x",
		CodexHome: "/tmp/codex-home",
		Outcome:   CodexBillingGuardAllowed,
		Reason:    "ok",
	}
	if !base.Valid() {
		t.Fatalf("base payload should be valid: %+v", base)
	}

	// RunID is optional: an empty RunID must still be valid.
	noRun := base
	noRun.RunID = ""
	if !noRun.Valid() {
		t.Errorf("payload with empty RunID should be valid (run-unscoped)")
	}

	cases := []struct {
		name  string
		mut   func(p *CodexBillingGuardPayload)
		valid bool
	}{
		{"missing bead", func(p *CodexBillingGuardPayload) { p.BeadID = "" }, false},
		{"missing codex_home", func(p *CodexBillingGuardPayload) { p.CodexHome = "" }, false},
		{"invalid outcome", func(p *CodexBillingGuardPayload) { p.Outcome = "nope" }, false},
		{"missing reason", func(p *CodexBillingGuardPayload) { p.Reason = "" }, false},
		{"denied is valid", func(p *CodexBillingGuardPayload) { p.Outcome = CodexBillingGuardDenied }, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mut(&p)
			if got := p.Valid(); got != tc.valid {
				t.Errorf("%s: Valid() = %v; want %v", tc.name, got, tc.valid)
			}
		})
	}
}

// TestCodexBillingGuard_Registered verifies the event type is registered in the
// global registry so emitted events decode to a CodexBillingGuardPayload.
func TestCodexBillingGuard_Registered(t *testing.T) {
	t.Parallel()
	if _, ok := LookupTypeSchemaVersion(string(EventTypeCodexBillingGuard)); !ok {
		t.Fatalf("event type %q is not registered", EventTypeCodexBillingGuard)
	}
}
