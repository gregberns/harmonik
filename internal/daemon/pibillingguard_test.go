package daemon_test

// pibillingguard_test.go — unit tests for the Pi fail-closed billing guard
// (codename:pilot, PI-040/042/043, hk-l1bkp).
//
// Coverage:
//   - runPiBillingGuard fails closed when the api_key_env var is absent or empty
//     (PI-040).
//   - runPiBillingGuard allows when the key is present and no on-disk credential
//     exists (PI-040 + PI-042 happy path).
//   - runPiBillingGuard fails closed when the supplied piHome/auth.json carries a
//     populated api_key (PI-042 on-disk check — exercised via injected piHome).
//   - runPiBillingGuard fails closed when piHome/auth.json is malformed (PI-042
//     fail-closed posture).
//   - runPiBillingGuard with a nil emitter does not panic.
//   - runPiBillingGuard emits pi_billing_guard events with allowed/denied outcomes.
//   - buildPiLaunchSpec fails closed (end-to-end wiring) when the guard denies
//     (absent env var).
//   - buildPiLaunchSpec succeeds when the guard allows (key present, clean disk).
//   - Production wiring: PiHarness.LaunchSpec does NOT set skipBillingGuard.
//
// All filesystem state uses t.TempDir() as a fake Pi home; the real ~/.pi is
// never touched. runPiBillingGuard accepts piHome as a parameter (mirroring
// runCodexBillingGuard's codexHome) so all PI-042 paths are exercisable in tests.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// payload-capturing emitter for pi_billing_guard events
// ─────────────────────────────────────────────────────────────────────────────

// capturingPiBillingEmitter records the (eventType, decoded payload) of every
// emitted event so tests can assert the pi_billing_guard outcome sequence. Safe
// for concurrent use.
type capturingPiBillingEmitter struct {
	mu      sync.Mutex
	types   []core.EventType
	guards  []core.PiBillingGuardPayload
	rawErrs []error
}

func (e *capturingPiBillingEmitter) record(eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, eventType)
	if eventType == core.EventTypePiBillingGuard {
		var pl core.PiBillingGuardPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			e.rawErrs = append(e.rawErrs, err)
			return nil
		}
		e.guards = append(e.guards, pl)
	}
	return nil
}

func (e *capturingPiBillingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	return e.record(eventType, payload)
}

func (e *capturingPiBillingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return e.record(eventType, payload)
}

func (e *capturingPiBillingEmitter) guardOutcomes() []core.PiBillingGuardOutcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]core.PiBillingGuardOutcome, 0, len(e.guards))
	for _, g := range e.guards {
		out = append(out, g.Outcome)
	}
	return out
}

// mustWritePi writes content to path, creating parent directories as needed.
func mustWritePi(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mustWritePi: mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("mustWritePi: write %q: %v", path, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// piAuthIndicatesPersistentCredential (PI-042)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiAuthIndicatesPersistentCredential_AbsentFile verifies a missing auth.json
// returns (false, nil) — Pi has not persisted a credential.
func TestPiAuthIndicatesPersistentCredential_AbsentFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	got, err := daemon.ExportedPiAuthIndicatesPersistentCredential(home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for absent auth.json; got true")
	}
}

// TestPiAuthIndicatesPersistentCredential_EmptyAPIKey verifies auth.json with an
// empty api_key field returns (false, nil).
func TestPiAuthIndicatesPersistentCredential_EmptyAPIKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mustWritePi(t, filepath.Join(home, "auth.json"), `{"api_key":"","other":"val"}`)
	got, err := daemon.ExportedPiAuthIndicatesPersistentCredential(home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for empty api_key; got true")
	}
}

// TestPiAuthIndicatesPersistentCredential_PopulatedAPIKey verifies auth.json with
// a non-empty api_key returns (true, nil) — a persisted credential was detected.
func TestPiAuthIndicatesPersistentCredential_PopulatedAPIKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mustWritePi(t, filepath.Join(home, "auth.json"), `{"api_key":"sk-or-live-persisted-key"}`)
	got, err := daemon.ExportedPiAuthIndicatesPersistentCredential(home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for populated api_key; got false")
	}
}

// TestPiAuthIndicatesPersistentCredential_Malformed verifies a malformed auth.json
// returns an error (fail-closed posture: uncertain state → deny).
func TestPiAuthIndicatesPersistentCredential_Malformed(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mustWritePi(t, filepath.Join(home, "auth.json"), "{not-valid-json")
	_, err := daemon.ExportedPiAuthIndicatesPersistentCredential(home)
	if err == nil {
		t.Error("expected error for malformed auth.json; got nil")
	}
}

// TestPiAuthIndicatesPersistentCredential_EmptyHome verifies an empty piHome
// returns (false, nil) without error (no path to check).
func TestPiAuthIndicatesPersistentCredential_EmptyHome(t *testing.T) {
	t.Parallel()
	got, err := daemon.ExportedPiAuthIndicatesPersistentCredential("")
	if err != nil {
		t.Fatalf("unexpected error for empty piHome: %v", err)
	}
	if got {
		t.Error("expected false for empty piHome; got true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runPiBillingGuard — PI-042 on-disk deny path (injected piHome)
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPiBillingGuard_PI042_PersistentCredentialDenies verifies that when
// piHome/auth.json carries a populated api_key, runPiBillingGuard fails closed
// even when the PI-040 env-var check passes. This tests the PI-042 on-disk deny
// path via an injected piHome (not the real ~/.pi).
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_PI042_PersistentCredentialDenies(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_PI042_DENY_KEY"
	t.Setenv(envVarName, "sk-or-real-key") // PI-040 passes

	piHome := t.TempDir()
	mustWritePi(t, filepath.Join(piHome, "auth.json"), `{"api_key":"sk-or-persisted-key"}`)

	em := &capturingPiBillingEmitter{}
	err := daemon.ExportedRunPiBillingGuard(em, "hk-pi042-deny", "", envVarName, piHome)
	if err == nil {
		t.Fatal("expected PI-042 fail-closed error for populated on-disk api_key; got nil")
	}
	if !strings.Contains(err.Error(), "billing guard") {
		t.Errorf("error %q does not mention billing guard", err.Error())
	}

	outcomes := em.guardOutcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 pi_billing_guard event; got %d: %v", len(outcomes), outcomes)
	}
	if outcomes[0] != core.PiBillingGuardDenied {
		t.Errorf("outcome = %q; want %q", outcomes[0], core.PiBillingGuardDenied)
	}
}

// TestRunPiBillingGuard_PI042_MalformedAuthJsonDenies verifies that when
// piHome/auth.json is malformed (cannot parse), runPiBillingGuard fails closed
// on the read/parse error — consistent with the "when in doubt, do not launch"
// posture. PI-040 env-var check passes; PI-042 parse error triggers deny.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_PI042_MalformedAuthJsonDenies(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_PI042_MALFORMED_KEY"
	t.Setenv(envVarName, "sk-or-real-key") // PI-040 passes

	piHome := t.TempDir()
	mustWritePi(t, filepath.Join(piHome, "auth.json"), "{not-valid-json")

	em := &capturingPiBillingEmitter{}
	err := daemon.ExportedRunPiBillingGuard(em, "hk-pi042-malformed", "", envVarName, piHome)
	if err == nil {
		t.Fatal("expected PI-042 fail-closed error for malformed auth.json; got nil")
	}

	outcomes := em.guardOutcomes()
	if len(outcomes) != 1 || outcomes[0] != core.PiBillingGuardDenied {
		t.Errorf("outcomes = %v; want [denied]", outcomes)
	}
}

// TestRunPiBillingGuard_PI042_AbsentAuthJsonAllows verifies that when
// piHome/auth.json is absent (Pi has not persisted a credential), runPiBillingGuard
// allows the launch — PI-042 is a no-op when the file is absent.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_PI042_AbsentAuthJsonAllows(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_PI042_ABSENT_KEY"
	t.Setenv(envVarName, "sk-or-real-key") // PI-040 passes

	piHome := t.TempDir() // no auth.json written → absent

	em := &capturingPiBillingEmitter{}
	if err := daemon.ExportedRunPiBillingGuard(em, "hk-pi042-absent", "", envVarName, piHome); err != nil {
		t.Fatalf("expected allowed for absent auth.json; got: %v", err)
	}

	outcomes := em.guardOutcomes()
	if len(outcomes) != 1 || outcomes[0] != core.PiBillingGuardAllowed {
		t.Errorf("outcomes = %v; want [allowed]", outcomes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runPiBillingGuard — fail-closed table test (PI-040 + PI-042)
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPiBillingGuard_FailClosed is the core fail-closed table test. It sets
// up the operator environment and optional pi home state for each case and asserts
// whether the guard permits (nil) or refuses (non-nil) the launch.
//
// The PI-042 on-disk check is exercised via ExportedPiAuthIndicatesPersistentCredential
// directly; runPiBillingGuard tests here focus on the PI-040 env-var check and
// the end-to-end path through both assertions.
//
// Not parallel: uses t.Setenv (modifies process env, incompatible with t.Parallel).
func TestRunPiBillingGuard_FailClosed(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_BILLING_GUARD_KEY"

	cases := []struct {
		name     string
		envValue string // "" = unset/empty
		wantErr  bool
		wantMsg  string // substring expected in the error message
	}{
		{
			name:     "env var absent -> deny (PI-040 fail closed)",
			envValue: "",
			wantErr:  true,
			wantMsg:  "absent or empty",
		},
		{
			name:     "env var present and non-empty -> allow (PI-040 pass, PI-042 no file)",
			envValue: "sk-or-test-live-key",
			wantErr:  false,
		},
		{
			name:     "env var whitespace only -> deny (PI-040 trimmed)",
			envValue: "   ",
			wantErr:  true,
			wantMsg:  "absent or empty",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Not calling t.Parallel(): t.Setenv incompatible with t.Parallel.
			if tc.envValue != "" {
				t.Setenv(envVarName, tc.envValue)
			} else {
				// Explicitly unset so a stray env var in the test host does not leak.
				t.Setenv(envVarName, "")
			}

			em := &capturingPiBillingEmitter{}
			err := daemon.ExportedRunPiBillingGuard(em, "hk-guard-test", "", envVarName, "")

			if tc.wantErr && err == nil {
				t.Errorf("%s: expected fail-closed error; got nil", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("%s: expected nil (launch permitted); got: %v", tc.name, err)
			}
			if tc.wantErr && tc.wantMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("%s: error %q does not contain %q", tc.name, err.Error(), tc.wantMsg)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runPiBillingGuard — event emission
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPiBillingGuard_AllowedEmitsAllowedOutcome verifies the happy path:
// when the env var is present and auth.json is absent, the guard emits a single
// pi_billing_guard event with outcome=allowed.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_AllowedEmitsAllowedOutcome(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_BILLING_GUARD_ALLOW_KEY"
	t.Setenv(envVarName, "sk-or-real-key-value")

	em := &capturingPiBillingEmitter{}
	if err := daemon.ExportedRunPiBillingGuard(em, "hk-guard-allow", "", envVarName, ""); err != nil {
		t.Fatalf("guard returned error on a clean env+disk: %v", err)
	}

	outcomes := em.guardOutcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 pi_billing_guard event; got %d: %v", len(outcomes), outcomes)
	}
	if outcomes[0] != core.PiBillingGuardAllowed {
		t.Errorf("outcome = %q; want %q", outcomes[0], core.PiBillingGuardAllowed)
	}

	// Verify the event payload names the env-var NAME, not its value (PI-040).
	if len(em.guards) != 1 {
		t.Fatalf("expected 1 decoded guard payload; got %d", len(em.guards))
	}
	pl := em.guards[0]
	if pl.EnvVarName != envVarName {
		t.Errorf("EnvVarName = %q; want env var NAME %q (PI-040 leak prevention)", pl.EnvVarName, envVarName)
	}
	if strings.Contains(pl.Reason, "sk-or-real-key-value") {
		t.Errorf("event Reason must NOT contain the key VALUE; got: %q", pl.Reason)
	}
	if pl.BeadID != "hk-guard-allow" {
		t.Errorf("BeadID = %q; want %q", pl.BeadID, "hk-guard-allow")
	}
}

// TestRunPiBillingGuard_AbsentKeyEmitsDeniedOutcome verifies that an absent env
// var emits a single pi_billing_guard event with outcome=denied.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_AbsentKeyEmitsDeniedOutcome(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_BILLING_GUARD_DENY_KEY"
	t.Setenv(envVarName, "") // absent/empty

	em := &capturingPiBillingEmitter{}
	err := daemon.ExportedRunPiBillingGuard(em, "hk-guard-deny", "", envVarName, "")
	if err == nil {
		t.Fatal("expected fail-closed error for absent key; got nil")
	}

	outcomes := em.guardOutcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 pi_billing_guard event; got %d: %v", len(outcomes), outcomes)
	}
	if outcomes[0] != core.PiBillingGuardDenied {
		t.Errorf("outcome = %q; want %q", outcomes[0], core.PiBillingGuardDenied)
	}
	if em.guards[0].EnvVarName != envVarName {
		t.Errorf("EnvVarName = %q; want %q", em.guards[0].EnvVarName, envVarName)
	}
}

// TestRunPiBillingGuard_NilEmitterNoPanic verifies that a nil emitter does not
// panic — enforcement (error return) still works; only event emission is skipped.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_NilEmitterNoPanic(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_GUARD_NIL_EMITTER"
	t.Setenv(envVarName, "") // trigger the deny path

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("runPiBillingGuard with nil emitter panicked: %v", r)
		}
	}()
	err := daemon.ExportedRunPiBillingGuard(nil, "hk-nil-emitter", "", envVarName, "")
	if err == nil {
		t.Error("expected error for absent key even with nil emitter; got nil")
	}
}

// TestRunPiBillingGuard_NilEmitterAllowNoPanic verifies that a nil emitter on the
// allow path (key present) does not panic.
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_NilEmitterAllowNoPanic(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_GUARD_NIL_EMITTER_ALLOW"
	t.Setenv(envVarName, "sk-or-real-key")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("runPiBillingGuard with nil emitter (allow path) panicked: %v", r)
		}
	}()
	if err := daemon.ExportedRunPiBillingGuard(nil, "hk-nil-emitter-allow", "", envVarName, ""); err != nil {
		t.Errorf("expected nil for present key with nil emitter; got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildPiLaunchSpec end-to-end wiring (PI-040 + PI-043)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildPiLaunchSpec_GuardDenies_RefusesSpec verifies that buildPiLaunchSpec
// returns an error (no spec) when the billing guard fails closed (absent env var).
// This is the PI-043 structural wiring test: the guard failure surfaces as a
// buildPiLaunchSpec error, which the tier-1 dispatch path propagates to
// run_failed + bead reopen.
//
// Not parallel: uses t.Setenv.
func TestBuildPiLaunchSpec_GuardDenies_RefusesSpec(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_SPEC_WIRING_DENY_KEY"
	t.Setenv(envVarName, "") // absent/empty → guard denies

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath: "/tmp/wt-test-pi-guard-deny",
		BeadID:        "hk-guard-wiring-deny",
		Provider:      "openrouter",
		Model:         "openrouter/qwen/qwen3-coder",
		APIKeyEnv:     envVarName,
		BaseEnv:       []string{"PATH=/usr/bin"},
		// SkipBillingGuard is NOT set → false → guard runs.
	}

	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Fatal("expected fail-closed error from buildPiLaunchSpec when env var absent; got nil spec")
	}
	if !strings.Contains(err.Error(), "billing guard") {
		t.Errorf("error message %q does not mention billing guard", err.Error())
	}
}

// TestBuildPiLaunchSpec_GuardAllows_ReturnsSpec verifies that buildPiLaunchSpec
// returns a spec (no error) when the billing guard allows (key present, no
// persisted on-disk credential).
//
// Not parallel: uses t.Setenv.
func TestBuildPiLaunchSpec_GuardAllows_ReturnsSpec(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_SPEC_WIRING_ALLOW_KEY"
	t.Setenv(envVarName, "sk-or-real-key-wiring-allow")

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath: "/tmp/wt-test-pi-guard-allow",
		BeadID:        "hk-guard-wiring-allow",
		Provider:      "openrouter",
		Model:         "openrouter/qwen/qwen3-coder",
		APIKeyEnv:     envVarName,
		BaseEnv:       []string{"PATH=/usr/bin"},
		// SkipBillingGuard is NOT set → false → guard runs.
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("expected spec for present key; got error: %v", err)
	}
	if spec.Binary != "pi" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "pi")
	}
	if spec.WorkDir != rc.WorkspacePath {
		t.Errorf("WorkDir = %q; want %q", spec.WorkDir, rc.WorkspacePath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// skipBillingGuard production wiring
// ─────────────────────────────────────────────────────────────────────────────

// TestPiHarness_LaunchSpec_SkipBillingGuardIsFalseInProduction verifies that
// the production PiHarness.LaunchSpec call does NOT pass skipBillingGuard=true.
// This is a structural contract test: if skipBillingGuard were true in
// production, the fail-closed guard would silently no-op and never run.
//
// We verify this indirectly: call PiHarness.LaunchSpec without a real key and
// assert it returns an error (because the guard ran and denied the launch). If
// skipBillingGuard were true in production, the call would succeed.
//
// Not parallel: uses t.Setenv.
func TestPiHarness_LaunchSpec_SkipBillingGuardIsFalseInProduction(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_HARNESS_PROD_WIRING_KEY"
	t.Setenv(envVarName, "") // no key → guard denies if it runs

	harness := daemon.NewPiHarness(
		"pi",            // binary
		"openrouter",    // provider
		"openrouter/m1", // model
		envVarName,      // apiKeyEnv
		"",              // apiKeyFile: not set
		"",              // baseURL: not set
		"",              // api: not set
	)

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-test-pi-harness-prod",
		BeadID:        "hk-harness-prod-wiring",
		BaseEnv:       []string{"PATH=/usr/bin"},
	}

	_, err := harness.LaunchSpec(rc)
	if err == nil {
		t.Fatal("PiHarness.LaunchSpec returned nil error with absent key — this means skipBillingGuard is true in production (MUST be false)")
	}
	if !strings.Contains(err.Error(), "billing guard") {
		t.Logf("note: error message %q does not mention billing guard (but an error was returned, which is correct)", err.Error())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PiBillingGuardPayload validity
// ─────────────────────────────────────────────────────────────────────────────

// TestPiBillingGuardPayload_Valid verifies the Valid() method on the payload
// type and outcome type cover their declared constraints.
func TestPiBillingGuardPayload_Valid(t *testing.T) {
	t.Parallel()

	// Outcome Valid().
	for _, o := range []core.PiBillingGuardOutcome{
		core.PiBillingGuardAllowed,
		core.PiBillingGuardDenied,
	} {
		if !o.Valid() {
			t.Errorf("outcome %q should be Valid(); got false", o)
		}
	}
	if core.PiBillingGuardOutcome("unknown").Valid() {
		t.Error("outcome \"unknown\" should not be Valid(); got true")
	}

	// Payload Valid() — happy path.
	happy := core.PiBillingGuardPayload{
		BeadID:     "hk-test",
		EnvVarName: "OPENROUTER_API_KEY",
		Outcome:    core.PiBillingGuardAllowed,
		Reason:     "key present and no on-disk credential",
	}
	if !happy.Valid() {
		t.Error("happy payload should be Valid(); got false")
	}

	// RunID is optional — empty RunID is valid.
	happy.RunID = ""
	if !happy.Valid() {
		t.Error("payload with empty RunID should be Valid() (RunID is optional)")
	}

	// Missing required fields.
	cases := []struct {
		name   string
		mutate func(p *core.PiBillingGuardPayload)
	}{
		{"empty BeadID", func(p *core.PiBillingGuardPayload) { p.BeadID = "" }},
		{"empty EnvVarName", func(p *core.PiBillingGuardPayload) { p.EnvVarName = "" }},
		{"invalid Outcome", func(p *core.PiBillingGuardPayload) { p.Outcome = "bad" }},
		{"empty Reason", func(p *core.PiBillingGuardPayload) { p.Reason = "" }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := happy
			tc.mutate(&p)
			if p.Valid() {
				t.Errorf("%s: payload should not be Valid()", tc.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// event payload does not leak key value (PI-040 / ps-argv)
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPiBillingGuard_EventDoesNotLeakKeyValue verifies that the emitted
// pi_billing_guard event payload contains the env-var NAME but NOT its value
// (PI-040 / ps-argv leak prevention).
//
// Not parallel: uses t.Setenv.
func TestRunPiBillingGuard_EventDoesNotLeakKeyValue(t *testing.T) {
	// Not calling t.Parallel(): t.Setenv is incompatible with t.Parallel in Go 1.22+.

	const envVarName = "TEST_PI_BILLING_NOLEAK_KEY"
	const keyValue = "sk-or-secret-value-must-not-appear-in-event"
	t.Setenv(envVarName, keyValue)

	em := &capturingPiBillingEmitter{}
	if err := daemon.ExportedRunPiBillingGuard(em, "hk-noleak", "", envVarName, ""); err != nil {
		t.Fatalf("guard returned error on a clean env: %v", err)
	}

	if len(em.guards) == 0 {
		t.Fatal("expected at least one pi_billing_guard event; got none")
	}
	for _, pl := range em.guards {
		if strings.Contains(pl.Reason, keyValue) {
			t.Errorf("event Reason MUST NOT contain key value; got Reason=%q", pl.Reason)
		}
		if pl.EnvVarName != envVarName {
			t.Errorf("event EnvVarName = %q; want env-var NAME %q", pl.EnvVarName, envVarName)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers (ensure handlercontract import is satisfied)
// ─────────────────────────────────────────────────────────────────────────────

var _ handlercontract.EventEmitter = (*capturingPiBillingEmitter)(nil)
