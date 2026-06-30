package daemon

// pibillingguard.go — Pi fail-closed billing guard (codename:pilot,
// PI-040/042/043, hk-l1bkp).
//
// BILLING FAIL-CLOSED GUARD (inverted from codex):
//   Codex: positive guard — forces ChatGPT-subscription billing; refuses if an
//          API key IS present (don't bill the API pool).
//   Pi:    negative guard — requires an operator-provided API key; refuses if the
//          configured provider key IS ABSENT (fail closed without a key).
//
// Two steps per launch:
//  1. PI-040: The env var named by api_key_env must be present AND non-empty in
//     the operator environment (resolved via the shared resolvePiAPIKeyValue
//     helper so this guard and buildPiEnv agree on the exact key value Pi
//     receives). Absent/empty → typed error → launch refused BEFORE agent_ready.
//  2. PI-042 (UNCONFIRMED): On-disk credential check. Until Pi's no-persist
//     posture is confirmed (findings.md §4), assert that piDefaultHome()/auth.json
//     does NOT carry a persisted provider key. A populated key indicates a prior
//     Pi session wrote credentials to disk. FAIL CLOSED. If Pi does not persist
//     (file absent → no-op), this is a cheap extra safety check.
//
// PI-041 (allowlist strip of non-selected provider keys) is handled in
// buildPiEnv (pilaunchspec.go) before this guard is called.
//
// PI-043: any guard failure → buildPiLaunchSpec returns an error → the tier-1
// dispatch path propagates it to run_failed + bead reopen. The tier-4 claude
// fallback CANNOT fire because harness:pi resolves at tier-1, which fails loud
// on any LaunchSpec error.
//
// Events name the env-var NAME, never its value (PI-040 / ps-argv leak prevention).
//
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.6.
// Spec: specs/pi-harness.md §4 (PI-040/PI-042/PI-043).
// Bead: hk-l1bkp.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// piRunIDIsNil reports whether runID is the zero (uuid.Nil) RunID. The Pi
// launch-spec builder can run before a run_id is minted, in which case the
// billing-guard event is emitted run-unscoped via Emit rather than EmitWithRunID.
func piRunIDIsNil(runID core.RunID) bool {
	return uuid.UUID(runID) == uuid.Nil
}

// piAuthFileName is the on-disk file the PI-042 speculative credential check
// inspects for a persisted Pi provider key.
//
// UNCONFIRMED (PI-042): Pi may not persist credentials at all (findings.md §4).
// If this file is absent the check is a no-op.
const piAuthFileName = "auth.json"

// piAuthFile is the subset of piDefaultHome()/auth.json that the PI-042
// on-disk credential check inspects.
//
// UNCONFIRMED (PI-042): Pi's auth.json schema is not documented. This struct
// captures the field that would indicate a persisted key in a typical LLM CLI
// auth file. If Pi does not write this file the guard returns (false, nil).
type piAuthFile struct {
	APIKey string `json:"api_key"`
}

// piDefaultHome returns the default Pi home directory (~/.pi).
//
// UNCONFIRMED (PI-042): Pi may store auth/config under a different path. This is
// a speculative default mirroring codex's "$HOME/.codex". The PI-042 check is
// a no-op when the directory or file is absent, so an incorrect path is safe.
func piDefaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi")
}

// piAuthIndicatesPersistentCredential reports whether piHome/auth.json carries a
// persisted provider API key.
//
// UNCONFIRMED (PI-042): Pi may not persist credentials (findings.md §4). Returns
// (false, nil) when auth.json is absent (Pi has not persisted anything) or when
// the file parses but carries an empty api_key field. Returns an error on a
// read/parse fault so the caller can fail closed.
//
// Exported via export_test.go for tests (ExportedPiAuthIndicatesPersistentCredential).
func piAuthIndicatesPersistentCredential(piHome string) (bool, error) {
	if piHome == "" {
		return false, nil
	}
	authPath := filepath.Join(piHome, piAuthFileName)
	data, err := os.ReadFile(authPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("piAuthIndicatesPersistentCredential: read %q: %w", authPath, err)
	}
	var auth piAuthFile
	if uerr := json.Unmarshal(data, &auth); uerr != nil {
		return false, fmt.Errorf("piAuthIndicatesPersistentCredential: parse %q: %w", authPath, uerr)
	}
	return strings.TrimSpace(auth.APIKey) != "", nil
}

// emitPiBillingGuard emits a pi_billing_guard event describing one observable
// step of the guard (PI-040/PI-042/PI-043, hk-l1bkp).
//
// Non-fatal: a nil emitter or a marshal error is silently discarded — the
// guard's enforcement is the return value of runPiBillingGuard, not this event.
// runID may be uuid.Nil (spec builder can run before a run_id is minted); the
// event is then emitted run-unscoped via Emit rather than EmitWithRunID.
//
// Events name apiKeyEnv (the env-var NAME), never its value (PI-040 / ps/argv
// leak prevention).
func emitPiBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, apiKeyEnv string,
	outcome core.PiBillingGuardOutcome,
	reason string,
) {
	if bus == nil {
		return
	}
	pl := core.PiBillingGuardPayload{
		BeadID:     beadID,
		EnvVarName: apiKeyEnv,
		Outcome:    outcome,
		Reason:     reason,
	}
	if !piRunIDIsNil(runID) {
		pl.RunID = runID.String()
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if piRunIDIsNil(runID) {
		_ = bus.Emit(ctx, core.EventTypePiBillingGuard, b)
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypePiBillingGuard, b)
}

// runPiBillingGuard runs the full fail-closed guard for one Pi launch.
//
// PI-040: The env var named by apiKeyEnv must be present and non-empty in the
// operator environment (read via resolvePiAPIKeyValue — the shared helper that
// buildPiEnv also calls, so both agree on the key value Pi receives).
// Absent/empty → error → caller MUST NOT launch Pi.
//
// PI-042 (UNCONFIRMED): piDefaultHome()/auth.json must not carry a persisted
// provider API key. A populated key signals that a prior Pi session wrote
// credentials to disk; we refuse to launch over a persisted credential.
// If Pi does not persist (file absent → no-op), this check is free.
//
// Returns nil only when the launch may proceed. A non-nil error means the guard
// failed closed (PI-043): the caller MUST NOT launch Pi. PI-043 is enforced
// structurally: this error propagates from buildPiLaunchSpec → tier-1 dispatch
// path → run_failed + bead reopen; the tier-4 claude fallback cannot fire.
func runPiBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, apiKeyEnv string,
) error {
	// PI-040: assert env var named by apiKeyEnv is present and non-empty.
	keyValue := resolvePiAPIKeyValue(apiKeyEnv)
	if strings.TrimSpace(keyValue) == "" {
		reason := fmt.Sprintf(
			"env var %s is absent or empty; Pi cannot launch without a provider API key (PI-040 fail closed)",
			apiKeyEnv)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}

	// PI-042 (UNCONFIRMED on-disk check): assert piDefaultHome()/auth.json does
	// not carry a persisted provider key. Fail closed on any read/parse error
	// (consistent with the "when in doubt, do not launch" posture).
	piHome := piDefaultHome()
	persisted, err := piAuthIndicatesPersistentCredential(piHome)
	if err != nil {
		reason := fmt.Sprintf(
			"PI-042 on-disk check failed reading %q: %v (fail closed)",
			piHome, err)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}
	if persisted {
		reason := fmt.Sprintf(
			"PI-042 on-disk check: %q/auth.json carries a persisted api_key; refusing to launch Pi over a persisted credential (fail closed)",
			piHome)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}

	// All checks passed: emit allowed and permit the launch.
	emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardAllowed,
		fmt.Sprintf(
			"env var %s is present and non-empty; no persisted Pi credential detected; Pi launch permitted (PI-040/PI-042)",
			apiKeyEnv))
	return nil
}
