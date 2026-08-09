package codex

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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

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

// codexConfigLockName is the advisory-lock sidecar the guard holds while it
// rewrites config.toml (hk-codex-billing-guard-race-v6dl5).
//
// $CODEX_HOME is GLOBAL: one directory shared by every codex process and every
// guard invocation on the host, as walguard_concurrency_test.go describes for
// the stale-WAL guard. At --max-concurrent N, N guards can reach
// materializeForcedLoginMethod at once, each doing a read-modify-write of this
// one file, and without a lock the last writer silently discards the others'
// edits.
//
// The name is dot-prefixed and harmonik-branded because the directory belongs to
// codex, not to us: an operator listing ~/.codex can tell at a glance which
// files are ours.
const codexConfigLockName = ".harmonik-config.toml.lock"

// codexConfigStagingName is the staging file replaceCodexConfig renames over
// config.toml. It shares the dot prefix so it is not mistaken for codex
// configuration.
//
// The name is FIXED rather than unique-per-call, and that is deliberate: the
// caller holds the config lock, so there is never a second writer to collide
// with, and a staging file stranded by a SIGKILL between write and rename is a
// single stale file that the next write truncates and reuses. A unique name per
// call would need a reaper to stop stale files accumulating in the operator's
// real ~/.codex.
const codexConfigStagingName = ".harmonik-config.toml.staging"

// codexConfigWriteMu serializes config.toml rewrites WITHIN this process, in
// front of the cross-process flock.
//
// Reason, learned in internal/workspace/claudetrust_wm040b.go (hk-z16): flock is
// unfair. With --max-concurrent 8, eight guards that all spin on LOCK_EX at once
// can let the cumulative hold time of seven serial holders exceed the eighth's
// acquire bound, so the eighth is starved and its launch is refused for a reason
// that has nothing to do with its work. Queueing in-process first means only one
// goroutine here ever reaches the flock, which leaves the flock arbitrating what
// it is actually for: other processes.
var codexConfigWriteMu sync.Mutex

// errCodexConfigLockTimeout is returned when the guard cannot acquire the config
// lock inside its bound. It wraps handlercontract.ErrStructural so the dispatch
// path classifies the refusal as structural — a contended host, retryable — and
// not as a verdict about the bead. Mirrors workspace.ErrTrustLockTimeout.
var errCodexConfigLockTimeout = fmt.Errorf(
	"codex billing guard: %w: config-lock acquire timed out (contended $CODEX_HOME)",
	handlercontract.ErrStructural)

// codexConfigLockTimeout bounds the wait for the config lock. The guard fails
// closed when it cannot take the lock, because it cannot then promise the login
// pin is in place, so this bound decides how long a launch WAITS before it is
// refused rather than whether it is refused. Mirrors the 10s bound in
// internal/schedule/store.go.
const codexConfigLockTimeout = 10 * time.Second

// codexConfigLockRetryInterval is the poll interval of the bounded
// LOCK_EX|LOCK_NB acquire. Mirrors internal/schedule/store.go.
const codexConfigLockRetryInterval = 25 * time.Millisecond

// acquireCodexConfigLock takes a bounded advisory exclusive flock on the
// $CODEX_HOME lock sidecar and returns the closure that releases it. codexHome
// must already exist.
//
// The bounded LOCK_EX|LOCK_NB retry is the idiom already used in
// internal/schedule/store.go and internal/workspace/claudetrust_wm040b.go: a
// stuck holder surfaces as a prompt error instead of an indefinite hang. flock
// is released by the kernel when the holding process dies, so a crashed guard
// cannot wedge the next launch.
func acquireCodexConfigLock(ctx context.Context, codexHome string, timeout time.Duration) (func(), error) {
	lockPath := filepath.Join(codexHome, codexConfigLockName)
	//nolint:gosec // G304: fixed lockfile name beneath the caller's CODEX_HOME root.
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", lockPath, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		flockErr := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			break
		}
		if !errors.Is(flockErr, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("flock %q: %w", lockPath, flockErr), fd.Close())
		}
		if time.Now().After(deadline) {
			return nil, errors.Join(
				fmt.Errorf("%w: %s after %s", errCodexConfigLockTimeout, lockPath, timeout),
				fd.Close())
		}
		// Wait on the retry interval and on cancellation together. A plain sleep
		// would make a cancelled run wait out the whole bound before it noticed.
		select {
		case <-ctx.Done():
			return nil, errors.Join(
				fmt.Errorf("flock %q: %w", lockPath, ctx.Err()),
				fd.Close())
		case <-time.After(codexConfigLockRetryInterval):
		}
	}
	return func() {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN) //nolint:errcheck // unlock error non-actionable; close also drops the advisory lock
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(ctx, "codex billing guard: close config lockfile",
				"path", lockPath, "error", closeErr.Error())
		}
	}, nil
}

// replaceCodexConfig puts data at cfgPath by writing a staging file in the same
// directory and renaming it over the target, so a concurrent reader sees either
// the whole old file or the whole new one and never a half-written one.
//
// os.WriteFile truncates the target before it writes. A reader inside that
// window reads an empty or partial config.toml, and for THIS file that window is
// a billing decision: a codex child that reads no forced_login_method falls back
// to API-pool billing, and a peer guard that reads none refuses a launch that
// was valid. An independent review measured 8047 of 12000 concurrent reads
// landing inside the window.
//
// The staging file is created in codexHome rather than the system temp dir
// because os.Rename is only atomic within one filesystem.
//
// The caller MUST hold the config lock: the staging path is a fixed name, so two
// unsynchronised writers would stage into the same file.
func replaceCodexConfig(codexHome, cfgPath string, data []byte) error {
	tmpPath := filepath.Join(codexHome, codexConfigStagingName)
	//nolint:gosec // G304: fixed staging filename beneath the caller's CODEX_HOME root.
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create staging file %q: %w", tmpPath, err)
	}
	discard := func(cause error, format string, args ...any) error {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup of a staging file we are already failing on
		return fmt.Errorf(format+": %w", append(args, cause)...)
	}
	if _, werr := tmp.Write(data); werr != nil {
		return discard(errors.Join(werr, tmp.Close()), "write staging file %q", tmpPath)
	}
	// fsync before the rename so a crash cannot leave the renamed file present
	// but empty, which would be the same unguarded-config hazard by another route.
	if serr := tmp.Sync(); serr != nil {
		return discard(errors.Join(serr, tmp.Close()), "fsync staging file %q", tmpPath)
	}
	if cerr := tmp.Close(); cerr != nil {
		return discard(cerr, "close staging file %q", tmpPath)
	}
	// O_CREATE only applies the 0600 mode when it CREATES the file, so a staging
	// file left by an earlier crash would carry whatever mode it already had, and
	// the rename would carry that mode onto config.toml. State the mode instead.
	if cerr := os.Chmod(tmpPath, 0o600); cerr != nil {
		return discard(cerr, "chmod staging file %q", tmpPath)
	}
	if rerr := os.Rename(tmpPath, cfgPath); rerr != nil {
		return discard(rerr, "rename %q over %q", tmpPath, cfgPath)
	}
	return nil
}

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
// An already-pinned config returns on a LOCK-FREE probe: no mutex, no flock, no
// rewrite. That is the case on every launch after the operator's first, so it is
// the case that must not queue behind anything. When a rewrite IS needed, the
// read and the write are one critical section under the $CODEX_HOME config lock
// and the write lands by rename. Together those give the three properties
// concurrent dispatch needs: the common case does not contend at all, no guard
// loses another guard's edit, and no reader ever sees a truncated config.
//
// The lock-free probe is safe because every write lands by rename, so a reader
// sees a whole file or a whole file. It is also ADVISORY: it reports what the
// config said at the instant it was read, and a non-cooperating writer can
// unpin the key immediately afterwards. That is why runCodexBillingGuard still
// re-reads in assertChatGPTPlan rather than trusting this result.
//
// Returns an error on a filesystem fault (mkdir / read / write) or on a lock the
// guard could not take inside codexConfigLockTimeout. A non-writable or
// contended CODEX_HOME surfaces here as an error so the launch fails closed
// rather than launching codex against an unguarded config.
func materializeForcedLoginMethod(ctx context.Context, codexHome string) error {
	return materializeForcedLoginMethodWithin(ctx, codexHome, codexConfigLockTimeout)
}

// materializeForcedLoginMethodWithin is materializeForcedLoginMethod with the
// lock bound as a parameter, so a test can assert the fail-closed contended path
// without waiting out the production timeout.
func materializeForcedLoginMethodWithin(ctx context.Context, codexHome string, lockTimeout time.Duration) error {
	if codexHome == "" {
		return fmt.Errorf("materializeForcedLoginMethod: codexHome must be non-empty")
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil { //dirmode:allow not a .harmonik state dir: $CODEX_HOME holds codex's own login credentials, 0o700 by design (never widen to core.HarmonikDirMode)
		return fmt.Errorf("materializeForcedLoginMethod: mkdir %q: %w", codexHome, err)
	}

	// Fast path: already pinned, nothing to write, so take no lock at all. A probe
	// ERROR is deliberately not returned here — an absent config.toml is the
	// ordinary first-launch case, and any other read fault re-surfaces below where
	// the locked path reads the same file.
	if pinned, probeErr := configDeclaresChatGPTLogin(codexHome); probeErr == nil && pinned {
		return nil
	}

	// A rewrite is needed. Queue in-process first so only one goroutine here
	// reaches the flock, then re-probe: a predecessor in this process may have
	// pinned the config while we waited.
	codexConfigWriteMu.Lock()
	defer codexConfigWriteMu.Unlock()
	if pinned, probeErr := configDeclaresChatGPTLogin(codexHome); probeErr == nil && pinned {
		return nil
	}

	release, err := acquireCodexConfigLock(ctx, codexHome, lockTimeout)
	if err != nil {
		return fmt.Errorf("materializeForcedLoginMethod: %w", err)
	}
	defer release()

	cfgPath := filepath.Join(codexHome, codexConfigFileName)

	//nolint:gosec // G304: cfgPath is a fixed config.toml filename beneath the caller's CODEX_HOME root.
	existing, err := os.ReadFile(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("materializeForcedLoginMethod: read %q: %w", cfgPath, err)
	}

	if werr := replaceCodexConfig(codexHome, cfgPath, pinnedConfigContent(existing)); werr != nil {
		return fmt.Errorf("materializeForcedLoginMethod: %w", werr)
	}
	return nil
}

// pinnedConfigContent returns what config.toml must contain so it declares the
// chatgpt login pin, given whatever it contains now. An empty or absent file
// becomes the single pinned line; a file that already declares the key has that
// one line rewritten and keeps everything else; a file that does not declare it
// gets the line appended after exactly one newline boundary.
//
// Pure: it does no I/O, so the three content rules are testable on their own and
// the caller stays a straight line of lock-then-read-then-write.
func pinnedConfigContent(existing []byte) []byte {
	wantLine := fmt.Sprintf("%s = %q", forcedLoginMethodKey, forcedLoginMethodValue)
	if len(existing) == 0 {
		return []byte(wantLine + "\n")
	}
	lines := strings.Split(string(existing), "\n")
	for i, line := range lines {
		if topLevelKeyOf(line) == forcedLoginMethodKey {
			lines[i] = wantLine
			return []byte(strings.Join(lines, "\n"))
		}
	}
	return []byte(strings.TrimRight(string(existing), "\n") + "\n" + wantLine + "\n")
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
	//nolint:gosec // G304: cfgPath is a fixed config.toml filename beneath the caller's CODEX_HOME root.
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
	//nolint:gosec // G304: authPath is a fixed auth.json filename beneath the caller's CODEX_HOME root.
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
		slog.WarnContext(ctx, "codex_billing_guard_event_marshal_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", err.Error())
		return
	}
	// A dropped emit loses the audit record for a fail-closed billing decision:
	// an operator asking "did the guard deny this launch?" would find nothing at
	// all. The emit stays best-effort — the guard's verdict is the caller's
	// return value, not this event — but the loss is no longer silent.
	var emitErr error
	if codexRunIDIsNil(runID) {
		emitErr = bus.Emit(ctx, core.EventTypeCodexBillingGuard, b)
	} else {
		emitErr = bus.EmitWithRunID(ctx, runID, core.EventTypeCodexBillingGuard, b)
	}
	if emitErr != nil {
		slog.WarnContext(ctx, "codex_billing_guard_event_emit_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", emitErr.Error())
	}
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
	if err := materializeForcedLoginMethod(ctx, codexHome); err != nil {
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
