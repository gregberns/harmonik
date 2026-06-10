package daemon

// codexbillingguard.go — positive codex billing guard (codex-harness C3/T11,
// hk-tu48u).
//
// BILLING LANDMINE (see project_flywheel_apikey_burn): `codex login` bills the
// ChatGPT *subscription* (wanted); a `--with-api-key` login or an inherited
// OPENAI_API_KEY / CODEX_API_KEY bills the API *credit pool* (NOT wanted).
//
// T10 (codexCredentialDenyKeys in codexlaunchspec.go) is the NEGATIVE guard: it
// strips the API-pool keys from the codex child env. This file is the POSITIVE
// guard, mirroring the fail-closed posture of the 2026-05-30 ANTHROPIC_API_KEY
// burn fixes:
//
//   1. materializeForcedLoginMethod — ensure forced_login_method = "chatgpt" in
//      $CODEX_HOME/config.toml before codex launches, so codex itself refuses to
//      fall back to API-key login.
//   2. assertChatGPTPlan — a FAIL-CLOSED pre-flight: refuse to launch codex unless
//      the ChatGPT plan can be positively confirmed.
//   3. emitCodexBillingGuard — emit a codex_billing_guard event at each step for
//      observability.
//
// The two filesystem helpers deliberately avoid a TOML dependency: only one
// top-level scalar key is read/written, so a line-oriented ensure/scan is
// sufficient and keeps the guard self-contained.
//
// Spec ref: C3-auth-billing-spec.md §Approach.
// Bead ref: hk-tu48u [C3/T11].

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

// codexRunIDIsNil reports whether runID is the zero (uuid.Nil) RunID. The codex
// launch-spec builder can run before a run_id is minted, in which case the
// billing-guard event is emitted run-unscoped via Emit rather than EmitWithRunID.
func codexRunIDIsNil(runID core.RunID) bool {
	return uuid.UUID(runID) == uuid.Nil
}

// forcedLoginMethodKey is the top-level config.toml key codex reads to pin the
// login method. Setting it to "chatgpt" makes codex refuse an API-key login.
const forcedLoginMethodKey = "forced_login_method"

// forcedLoginMethodValue is the only value the guard accepts: ChatGPT-subscription
// billing.
const forcedLoginMethodValue = "chatgpt"

// codexConfigFileName is the per-CODEX_HOME config file codex reads at startup.
const codexConfigFileName = "config.toml"

// codexAuthFileName is the per-CODEX_HOME auth file codex writes after login.
// A ChatGPT-plan login leaves the API-key field empty; an API-key login
// populates it. The pre-flight assert treats a populated API-key field as a
// fail-closed signal (API-pool billing).
const codexAuthFileName = "auth.json"

// materializeForcedLoginMethod ensures $CODEX_HOME/config.toml carries the
// top-level line `forced_login_method = "chatgpt"`.
//
// Behaviour:
//   - Creates codexHome (0700) and config.toml if absent.
//   - If config.toml already declares forced_login_method with the wanted value,
//     it is left untouched (idempotent).
//   - If it declares forced_login_method with a DIFFERENT value, that line is
//     rewritten to the wanted value (the guard owns this key).
//   - If it does not declare the key, the line is appended, preserving all other
//     content.
//
// Returns an error only on a filesystem fault (mkdir / read / write). A
// non-writable CODEX_HOME surfaces here as an error so the launch fails closed
// rather than launching codex against an unguarded config.
func materializeForcedLoginMethod(codexHome string) error {
	if codexHome == "" {
		return fmt.Errorf("materializeForcedLoginMethod: codexHome must be non-empty")
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return fmt.Errorf("materializeForcedLoginMethod: mkdir %q: %w", codexHome, err)
	}
	cfgPath := filepath.Join(codexHome, codexConfigFileName)

	existing, err := os.ReadFile(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("materializeForcedLoginMethod: read %q: %w", cfgPath, err)
	}

	wantLine := fmt.Sprintf("%s = %q", forcedLoginMethodKey, forcedLoginMethodValue)

	if os.IsNotExist(err) || len(existing) == 0 {
		if werr := os.WriteFile(cfgPath, []byte(wantLine+"\n"), 0o600); werr != nil {
			return fmt.Errorf("materializeForcedLoginMethod: write %q: %w", cfgPath, werr)
		}
		return nil
	}

	lines := strings.Split(string(existing), "\n")
	replaced := false
	for i, line := range lines {
		if topLevelKeyOf(line) == forcedLoginMethodKey {
			lines[i] = wantLine
			replaced = true
			break
		}
	}
	if !replaced {
		// Append; ensure exactly one trailing newline boundary before the key.
		out := strings.TrimRight(string(existing), "\n") + "\n" + wantLine + "\n"
		if werr := os.WriteFile(cfgPath, []byte(out), 0o600); werr != nil {
			return fmt.Errorf("materializeForcedLoginMethod: write %q: %w", cfgPath, werr)
		}
		return nil
	}
	if werr := os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")), 0o600); werr != nil {
		return fmt.Errorf("materializeForcedLoginMethod: write %q: %w", cfgPath, werr)
	}
	return nil
}

// topLevelKeyOf returns the bare top-level TOML key declared on a line, or "" if
// the line is a comment, a section header, blank, or otherwise not a `key = ...`
// assignment. Only top-level keys are recognised: a key under a [table] header is
// not a top-level forced_login_method and is ignored (the line scan does not
// track section state, so a same-named key inside a table would be a false match;
// codex declares forced_login_method at top level, so this is acceptable for the
// single key the guard owns).
func topLevelKeyOf(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return ""
	}
	eq := strings.IndexByte(trimmed, '=')
	if eq <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[:eq])
}

// configDeclaresChatGPTLogin reports whether $CODEX_HOME/config.toml declares
// forced_login_method = "chatgpt" (the value the guard materialized). It re-reads
// the file from disk rather than trusting the in-memory write, so a concurrent
// edit between materialize and assert is caught.
func configDeclaresChatGPTLogin(codexHome string) (bool, error) {
	cfgPath := filepath.Join(codexHome, codexConfigFileName)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false, fmt.Errorf("configDeclaresChatGPTLogin: read %q: %w", cfgPath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if topLevelKeyOf(line) != forcedLoginMethodKey {
			continue
		}
		eq := strings.IndexByte(line, '=')
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, "\"'")
		return val == forcedLoginMethodValue, nil
	}
	return false, nil
}

// codexAuthFile is the subset of $CODEX_HOME/auth.json the guard inspects. A
// ChatGPT-plan login leaves OPENAI_API_KEY empty/absent and writes an OAuth token
// set; an API-key login populates OPENAI_API_KEY. The guard only needs to detect
// a populated API key (the API-pool billing signal), so unknown fields are
// ignored.
type codexAuthFile struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
}

// authIndicatesAPIKeyLogin reports whether $CODEX_HOME/auth.json indicates an
// API-key login (a populated OPENAI_API_KEY field), which would bill the API
// credit pool. Returns (false, nil) when auth.json is absent (codex will perform
// the forced ChatGPT login on first use) or when the file parses but carries no
// API key. Returns an error on a read/parse fault so the assert can fail closed.
func authIndicatesAPIKeyLogin(codexHome string) (bool, error) {
	authPath := filepath.Join(codexHome, codexAuthFileName)
	data, err := os.ReadFile(authPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("authIndicatesAPIKeyLogin: read %q: %w", authPath, err)
	}
	var auth codexAuthFile
	if uerr := json.Unmarshal(data, &auth); uerr != nil {
		return false, fmt.Errorf("authIndicatesAPIKeyLogin: parse %q: %w", authPath, uerr)
	}
	return strings.TrimSpace(auth.OpenAIAPIKey) != "", nil
}

// assertChatGPTPlan is the FAIL-CLOSED pre-flight billing assert. It refuses to
// confirm the ChatGPT plan (returns a non-nil error) unless BOTH hold:
//
//   - config.toml declares forced_login_method = "chatgpt" (so codex cannot fall
//     back to an API-key login), AND
//   - auth.json does not carry a populated OPENAI_API_KEY (no existing API-pool
//     login).
//
// A nil return is the ONLY signal that codex may launch. Any error — a missing /
// wrong forced_login_method, a populated API key, or a filesystem/parse fault —
// MUST cause the caller to refuse the launch. This mirrors the API-key-burn
// fail-closed posture: when in doubt, do not bill.
func assertChatGPTPlan(codexHome string) error {
	if codexHome == "" {
		return fmt.Errorf("assertChatGPTPlan: codexHome must be non-empty")
	}

	ok, err := configDeclaresChatGPTLogin(codexHome)
	if err != nil {
		return fmt.Errorf("assertChatGPTPlan: cannot read config.toml: %w", err)
	}
	if !ok {
		return fmt.Errorf(
			"assertChatGPTPlan: %s/%s does not declare %s = %q; refusing to launch codex (fail closed)",
			codexHome, codexConfigFileName, forcedLoginMethodKey, forcedLoginMethodValue)
	}

	apiKeyLogin, err := authIndicatesAPIKeyLogin(codexHome)
	if err != nil {
		return fmt.Errorf("assertChatGPTPlan: cannot inspect auth.json: %w", err)
	}
	if apiKeyLogin {
		return fmt.Errorf(
			"assertChatGPTPlan: %s/%s carries a populated OPENAI_API_KEY (API-pool billing); refusing to launch codex (fail closed)",
			codexHome, codexAuthFileName)
	}

	return nil
}

// emitCodexBillingGuard emits a codex_billing_guard event (hk-tu48u) describing
// one observable step of the guard. Non-fatal: a nil emitter or a marshal error
// is silently discarded — the guard's enforcement is the materialize/assert
// return values, not this event. runID may be uuid.Nil (the spec builder can run
// before a run_id is minted); the payload's RunID is then left empty and the
// event is emitted run-unscoped.
func emitCodexBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, codexHome string,
	outcome core.CodexBillingGuardOutcome,
	reason string,
) {
	if bus == nil {
		return
	}
	pl := core.CodexBillingGuardPayload{
		BeadID:    beadID,
		CodexHome: codexHome,
		Outcome:   outcome,
		Reason:    reason,
	}
	if !codexRunIDIsNil(runID) {
		pl.RunID = runID.String()
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if codexRunIDIsNil(runID) {
		_ = bus.Emit(ctx, core.EventTypeCodexBillingGuard, b)
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeCodexBillingGuard, b)
}

// runCodexBillingGuard runs the full positive guard for one codex launch:
// materialize forced_login_method=chatgpt, emit "materialized", run the
// fail-closed pre-flight assert, and emit "allowed" or "denied" accordingly.
//
// Returns nil only when the launch may proceed. A non-nil error means the guard
// failed closed: the caller MUST NOT launch codex.
func runCodexBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, codexHome string,
) error {
	if err := materializeForcedLoginMethod(codexHome); err != nil {
		emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
			core.CodexBillingGuardDenied, "materialize failed: "+err.Error())
		return fmt.Errorf("codex billing guard: %w", err)
	}
	emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
		core.CodexBillingGuardMaterialized,
		fmt.Sprintf("%s = %q ensured in %s", forcedLoginMethodKey, forcedLoginMethodValue, codexConfigFileName))

	if err := assertChatGPTPlan(codexHome); err != nil {
		emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
			core.CodexBillingGuardDenied, err.Error())
		return fmt.Errorf("codex billing guard: %w", err)
	}
	emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
		core.CodexBillingGuardAllowed, "ChatGPT plan confirmed; codex launch permitted")
	return nil
}
