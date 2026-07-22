package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// defaultTrustLockTimeout bounds how long ensureWorktreeTrustAt waits to acquire
// the exclusive write lock on the ~/.claude.json sidecar before failing the
// launch with a structural error (hk-bfvby). Under pathological contention
// (~23 live claude processes all rewriting an 8MB config, flock being unfair),
// the daemon's LOCK_EX waiter could starve for minutes, wedging the spawn path
// upstream of the spawn semaphore. Bounding the wait converts that indefinite
// hang into a prompt, observable launch failure that reopens the bead. Far below
// the 30-min implementer commit budget so the wedge surfaces promptly, but
// generous enough to absorb a normal in-flight RMW cycle (~180ms) under load.
const defaultTrustLockTimeout = 15 * time.Second

// trustLockRetryInterval is the poll interval for the bounded LOCK_EX|LOCK_NB
// acquire loop. Short enough that a freed lock is grabbed promptly, long enough
// not to spin-burn a core while waiting.
const trustLockRetryInterval = 50 * time.Millisecond

// trustWriteMaxAttempts bounds how many read-modify-write cycles the trust
// writer will run before giving up (hk-qx065). Each attempt re-reads the config
// from disk, re-applies the trust key on top of whatever is there now, writes,
// and then VERIFIES the key survived. The retry exists because the clobberer is
// NOT another harmonik goroutine or process — it is Claude Code itself, which
// rewrites ~/.claude.json wholesale from its own in-memory snapshot and does not
// honor our advisory flock (observed in a964cbcb: ~15 concurrent live claude
// processes rewriting the shared config). No amount of locking on our side can
// exclude a writer that never asks for the lock, so we repair instead.
//
// # What the evidence actually was, and what is inference
//
// OBSERVED, at max-concurrent 3: two of three workers parked on the folder-trust
// modal, and the failed worktree's projects[<realpath>].hasTrustDialogAccepted
// was ABSENT from ~/.claude.json even though EnsureWorktreeTrust had run and
// returned success. INFERRED from that, plus a964cbcb's independent note that
// live claude processes rewrite the shared config without honoring our lock: the
// key was written and then erased by such a rewrite. Nobody counted clobbers —
// there is no measurement of how many rewrites hit a given launch, and this
// constant is NOT calibrated against one.
//
// 4 is therefore a judgement call, not a fitted value: enough attempts that a
// short burst of foreign rewrites is ridden out, few enough that the cost is
// negligible when nothing is wrong (the happy path performs exactly one cycle).
// If four consecutive attempts are all erased, the config is being rewritten
// continuously and a fifth is unlikely to differ — failing loudly beats retrying
// while the provisioning window stretches.
const trustWriteMaxAttempts = 4

// trustWriteRetryBackoff is the pause between a failed verification and the next
// read-modify-write attempt (hk-qx065). Sized just above the ~180ms in-flight
// RMW cycle noted on defaultTrustLockTimeout: long enough that a competing
// writer's rename has landed before we re-read (so we re-apply on top of its
// result rather than colliding with it mid-cycle), short enough that the whole
// retry budget stays negligible on the launch path.
//
// # Latency budget (worst case, and why it is safe)
//
// defaultTrustLockTimeout is a budget for the WHOLE verify-and-repair loop, not
// per attempt: ensureWorktreeTrustAt computes one deadline up front and passes
// each attempt only the time remaining. That matters because a lock wait which
// SUCCEEDS at 14.9s neither errors nor ends the loop — a fresh 15s per attempt
// would make the worst case that still returns nil ~4x15s + 3x200ms ≈ 60.6s.
// With a single shared deadline the worst case is 15s of lock waiting in total
// plus 3 x 200ms of backoff ≈ 15.6s, restoring the bound hk-bfvby established.
//
// That bound is what makes it safe, NOT the 150s agent_ready budget: this code
// runs during PROVISIONING (workloop's BuildSpec), and the agent_ready timer is
// only armed later, at the Idle->Launching transition in internal/runexec
// (stepDispatchIdle's ActArmTimer), reached after provisioning completes. There
// is no agent_ready deadline ticking while we retry. The real constraint is that
// trustWriteMu is held for the whole loop, so every other in-process launch that
// needs a trust write queues behind us — bounding the loop at ~15.6s keeps that
// queueing in the same range hk-bfvby already accepted.
const trustWriteRetryBackoff = 200 * time.Millisecond

// trustPostWriteHook is a TEST-ONLY seam (hk-qx065). When non-nil it is invoked
// immediately after a trust-upsert attempt's atomic write succeeds and BEFORE the
// post-write verification read, letting a test stand in for the non-cooperating
// external writer that erases our key. Production never sets it.
//
// It is read only while trustWriteMu is held (the write path holds that mutex for
// its whole duration), so tests MUST take trustWriteMu to set and restore it —
// that is what makes the access properly synchronized under -race.
var trustPostWriteHook func(cfgPath string)

// ErrTrustLockTimeout is returned when ensureWorktreeTrustAt cannot acquire the
// exclusive write lock within defaultTrustLockTimeout (hk-bfvby). It wraps
// handlercontract.ErrStructural so the daemon dispatch path classifies the
// launch failure as structural (reopen-the-bead) rather than hanging. The
// already-trusted fast path NEVER returns this error — it takes no write lock.
var ErrTrustLockTimeout = fmt.Errorf("workspace: EnsureWorktreeTrust: %w: write-lock acquire timed out (contended ~/.claude.json)", handlercontract.ErrStructural)

// trustWriteMu serializes in-process write operations on the global trust config
// (hk-z16). At -c8 all 8 implementers start simultaneously with NEW worktree
// paths; without this mutex all 8 spin on the LOCK_EX flock concurrently. Under
// a bloated ~/.claude.json (~8MB, slow per-call JSON marshal+atomic-write), the
// cumulative hold time of 7 serial flock holders can exceed
// defaultTrustLockTimeout, starving the 8th and causing ErrTrustLockTimeout.
// By serializing in-process first, only one goroutine ever waits on the flock;
// the flock is held for just one write cycle, so the external-process boundary
// is still protected while in-process starvation is impossible.
var trustWriteMu sync.Mutex

// defaultClaudeGlobalConfigPath returns the path to Claude Code's user-level
// JSON config file. Precedence (first match wins):
//
//  1. HARMONIK_CLAUDE_CONFIG_PATH — treated as a full file path. Intended for
//     test isolation: set to t.TempDir()+"/.claude.json" so unit and
//     integration tests never touch the real user config.
//  2. CLAUDE_CONFIG_HOME — treated as a directory; the config file is
//     filepath.Join(CLAUDE_CONFIG_HOME, ".claude.json"). Matches Claude Code's
//     own env-var convention.
//  3. ~/.claude.json — the production default.
//
// Exposed as a var so callers that cannot set env vars may redirect via direct
// assignment (integration-test helpers only; prefer the env var).
var claudeGlobalConfigPath = defaultClaudeGlobalConfigPath

func defaultClaudeGlobalConfigPath() string {
	// 1. Full-path override for test isolation.
	if p := os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH"); p != "" {
		return p
	}
	// 2. Directory override (Claude Code's own convention).
	if dir := os.Getenv("CLAUDE_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	// 3. Production default.
	home, err := os.UserHomeDir()
	if err != nil {
		// Unreachable on all supported platforms under normal operation.
		panic(fmt.Sprintf("workspace: claudeGlobalConfigPath: UserHomeDir: %v", err))
	}
	return filepath.Join(home, ".claude.json")
}

// EnsureWorktreeTrust pre-seeds Claude Code's user-level config (~/.claude.json)
// with a trust entry for worktreePath so that no interactive "Trust this
// directory?" prompt appears when Claude Code starts inside a daemon-spawned
// tmux pane (per workspace-model.md §4.7b WM-040b and claude-hook-bridge.md
// §4.12 CHB-029).
//
// # Mechanism
//
// Claude Code stores per-project trust state in ~/.claude.json under a
// top-level "projects" map keyed by absolute directory path. When the key is
// absent, or present but hasTrustDialogAccepted is false/missing, Claude Code
// shows an interactive trust prompt on startup. With no human at the terminal
// (daemon-spawned pane), that prompt blocks indefinitely and HC-056 fires.
//
// This function upserts the entry:
//
//	~/.claude.json["projects"][worktreePath]["hasTrustDialogAccepted"] = true
//
// It is idempotent: a second call for the same worktreePath is a no-op.
//
// # Concurrency (hk-bfvby)
//
// The overwhelmingly-common already-trusted case takes NO lock at all: a
// lock-free read-only probe (alreadyTrustedAt) reads the config without any
// flock and checks whether worktreePath is already present+trusted, returning
// immediately without the read-modify-write cycle. This removes the daemon from
// the write-contention path entirely for repeat launches — the original cause
// of the ~16-min spawn stall, where every call took LOCK_EX and starved behind
// ~23 live claude processes rewriting an 8MB config (flock is unfair). The probe
// is safe lock-free because the write path commits via atomic rename, so a
// reader sees a whole old or whole new file, never a torn one.
//
// Only when an actual mutation is needed (a new/untrusted path) is the advisory
// exclusive flock taken on a sidecar lockfile (<cfgPath>.lock) across the
// read-modify-write cycle. The acquire is BOUNDED (LOCK_EX|LOCK_NB with a
// deadline of defaultTrustLockTimeout); under pathological contention it returns
// ErrTrustLockTimeout (wrapping handlercontract.ErrStructural) so the launch
// fails fast and the bead reopens, rather than hanging for minutes. The sidecar
// approach keeps the target file's rename-atomic identity stable and the lock
// independent of the file's inode.
//
// # Verify-and-repair against a non-cooperating writer (hk-qx065)
//
// The flock only excludes writers that TAKE it. Claude Code does not: each live
// claude process rewrites ~/.claude.json wholesale from its own in-memory
// snapshot, so an entry we just wrote can be erased moments later. That was
// observed directly at max-concurrent 3 — two of three workers parked on the
// folder-trust modal, and the failed worktree's hasTrustDialogAccepted key was
// simply ABSENT from the config even though this function had returned success.
// A successful write syscall is therefore not proof the key is on disk.
//
// So every write attempt is followed by a RE-READ that confirms the key actually
// persisted, and a failed verification triggers a bounded retry of the whole
// read-modify-write (fresh read each time, so we re-apply on top of whatever the
// other writer left). If the key still has not stuck after trustWriteMaxAttempts,
// the function returns a structural error rather than reporting a success the
// disk does not support.
//
// This NARROWS the race; it does not eliminate it. A clobber that lands after our
// final verification but before Claude Code reads the config at startup will still
// produce the trust modal, and nothing on our side of the process boundary can
// prevent that. What the verification buys is that the common case — a clobber
// during our own write window — is now detected and repaired instead of being
// silently reported as success.
//
// # Ordering obligation (CHB-029 / WM-040b)
//
// MUST be called AFTER WM-003 (worktree creation) and WM-040a
// (settings.json materialization) and BEFORE exec'ing Claude via the tmux
// substrate (SubstrateSpawn). The ~/.claude.json write is NOT an atomic WM-026
// rename because the file must be stable across concurrent daemon activity; the
// function uses a PID-keyed temp file + rename for atomicity.
//
// # Failure semantics
//
// On any error (lock, read, parse, marshal, write, or a write that would not
// stick), EnsureWorktreeTrust returns a wrapped error. The caller MUST propagate
// this as a structural error and MUST NOT exec Claude — an un-trusted session
// would block rather than hang silently.
//
// # Parameters
//
//   - worktreePath: absolute path to the workspace root (worktree directory).
//     MUST be the same path Claude Code will be launched with as its working
//     directory (cmd.Dir / tmux start-directory).
func EnsureWorktreeTrust(worktreePath string) error {
	// Resolve symlinks so the key matches what Claude Code stores after its own
	// realpath() normalization (e.g. macOS /var/folders → /private/var/folders).
	// Without this, the trust entry is written under one path and looked up
	// under another, the interactive trust dialog fires, SessionStart never
	// arrives, and HC-056 times out (smoke v8 RED, hk-o5eww).
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		worktreePath = resolved
	}
	cfgPath := claudeGlobalConfigPath()
	return ensureWorktreeTrustAt(worktreePath, cfgPath)
}

// ensureWorktreeTrustAt is the testable inner implementation; cfgPath is the
// ~/.claude.json override, allowing unit tests to redirect to a temp file. It is
// also the path used against an EXPLICIT config file by
// PrepareIsolatedClaudeConfigDir (hk-8juwz), so it must stay correct for a
// per-launch private config, not just the shared global one.
//
// hk-bfvby: the already-trusted case takes NO write lock and performs NO
// read-modify-write. Only an actual mutation acquires the bounded LOCK_EX.
//
// hk-qx065: every write attempt is verified by a re-read, and a lost write is
// repaired by retrying the whole read-modify-write. See EnsureWorktreeTrust's
// "Verify-and-repair" section for why locking alone cannot be the answer.
func ensureWorktreeTrustAt(worktreePath, cfgPath string) error {
	// Fast path: lock-free read-only probe. If worktreePath is already present
	// and trusted, return immediately — no LOCK_EX, no rewrite. This is the
	// overwhelmingly-common case (every repeat launch) and is the one that must
	// NOT contend on the write lock. A concurrent writer mid-rename only ever
	// makes this probe MISS (it then falls through to the locked write path and
	// re-checks under the lock), never produces a false trust.
	//
	// The fast path is inherently ADVISORY (hk-qx065): it reports what the config
	// said at the instant it was read. A non-cooperating writer can erase the entry
	// immediately afterwards, and this probe has no way to notice. That is exactly
	// why the write path below verifies rather than trusting its own write — but it
	// also means a "true" here is a snapshot, not a guarantee that claude will find
	// the key when it starts.
	trusted, probeErr := alreadyTrustedAt(worktreePath, cfgPath)
	if probeErr != nil && !trustConfigDecodeErr(probeErr) {
		return probeErr
	}
	if trusted {
		return nil
	}
	// A DECODE error here does not return: it means the snapshot we read was not
	// valid JSON, which a torn read of a foreign non-atomic write looks exactly
	// like. Fall through to the repair loop, which re-reads under the retry budget
	// and surfaces the decode error only if it is still there on the last attempt.

	// Slow path: a mutation is needed. Serialize in-process first (hk-z16): hold
	// trustWriteMu so only one goroutine within this daemon process attempts the
	// flock at a time. Without this, -c8 concurrent new-worktree launches all spin
	// on the flock simultaneously, and under a bloated ~/.claude.json the
	// cumulative flock hold time can exceed defaultTrustLockTimeout. Re-check the
	// trusted state after acquiring the mutex: a predecessor in this process may
	// have just written the entry.
	trustWriteMu.Lock()
	defer trustWriteMu.Unlock()

	if trusted2, err2 := alreadyTrustedAt(worktreePath, cfgPath); err2 != nil && !trustConfigDecodeErr(err2) {
		return err2
	} else if trusted2 {
		return nil
	}

	// Write, verify, repair (hk-qx065).
	//
	// LOCK DISCIPLINE, precisely: each attempt re-acquires the sidecar FLOCK and
	// releases it before we sleep, so other PROCESSES are never blocked by our
	// backoff. trustWriteMu is a different story — it IS held across the whole
	// loop including the sleeps, so other in-process launches needing a trust
	// write DO queue behind us for the loop's duration. That is why the loop is
	// bounded (see trustWriteRetryBackoff's latency note): the total is ~15.6s
	// worst case, the same order hk-bfvby settled on, rather than the ~60.6s a
	// per-attempt lock deadline would allow.
	//
	// The lock budget is shared across attempts: one deadline computed here, each
	// attempt gets only what is left.
	lockDeadline := time.Now().Add(defaultTrustLockTimeout)

	// lastErr carries a DECODE error forward between attempts. Nil means the
	// cycle completed but the key simply was not there afterwards.
	var lastErr error

	for attempt := 1; attempt <= trustWriteMaxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(trustWriteRetryBackoff)
		}

		remaining := time.Until(lockDeadline)
		if remaining <= 0 {
			return ErrTrustLockTimeout
		}

		err := trustUpsertOnce(worktreePath, cfgPath, remaining)
		if err == nil {
			// Verification: re-read from disk. The write syscall succeeding proves
			// nothing about what is in the file now — an external writer may have
			// replaced the whole config in between (the observed hk-qx065 failure).
			var persisted bool
			persisted, err = alreadyTrustedAt(worktreePath, cfgPath)
			if err == nil && persisted {
				return nil
			}
		}

		// A lock timeout, an IO failure, or a marshal failure is not something a
		// retry can improve, and each costs real launch time — surface it now. A
		// DECODE failure is different: we cannot distinguish a genuinely corrupt
		// config from a torn read of a foreign writer's non-atomic rewrite by
		// looking at one snapshot, and this whole fix exists because such a writer
		// is active. So decode failures re-read within the same bounded budget; a
		// truly corrupt file costs ~600ms and then reports itself.
		if err != nil && !trustConfigDecodeErr(err) {
			return err
		}
		lastErr = err
	}

	// Out of attempts. A decode failure that survived every re-read is reported as
	// itself — it is more specific than "did not persist", and it preserves the
	// fail-rather-than-corrupt contract (we never overwrite an unparseable config
	// with a fresh one).
	if lastErr != nil {
		return lastErr
	}

	// The key would not stick. Fail structurally: the callers treat a trust-seed
	// failure as fatal and MUST NOT exec claude, because a launch into an untrusted
	// folder parks on the trust modal before SessionStart and no agent_ready is
	// ever synthesized — the run then dies at its ready deadline with nothing
	// explaining why. This error is the explanation.
	return fmt.Errorf("workspace: EnsureWorktreeTrust: %w: projects[%q].hasTrustDialogAccepted did not persist in %s after %d write attempts; "+
		"the most likely cause is a concurrent writer rewriting the shared config wholesale (live claude processes do this and do not honor our advisory lock)",
		handlercontract.ErrStructural, worktreePath, cfgPath, trustWriteMaxAttempts)
}

// trustConfigDecodeErr reports whether err is a JSON-decode failure of the config
// file, as opposed to a lock, IO, or marshal failure. Decode failures are the
// only class this file retries: a foreign writer that does not commit via rename
// can be observed mid-write, and that torn snapshot is indistinguishable from a
// corrupt file in a single read. Everything else either cannot improve with time
// (marshal, unexpected JSON shape) or indicates the file is unreachable (IO).
func trustConfigDecodeErr(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}

// trustUpsertOnce performs ONE read-modify-write cycle: acquire the bounded
// sidecar flock, read the config fresh from disk, upsert
// projects[worktreePath].hasTrustDialogAccepted = true, and atomically write it
// back. It returns nil both when it wrote and when it found the entry already
// trusted under the lock; the CALLER verifies the result by re-reading, so a nil
// here means "the cycle completed", not "the key is on disk".
//
// The flock is released when this function returns (deferred close), so the
// caller's backoff sleep never holds it. Reading the config fresh on every call
// is load-bearing: a retry must re-apply the key on top of whatever the clobbering
// writer left behind, never on a stale in-memory snapshot. Re-applying a stale
// snapshot would turn harmonik itself into the lost-update clobberer, erasing
// every key another writer committed in between — including a sibling worker's
// trust entry. That is the exact bug class this file exists to prevent.
//
// lockTimeout is the REMAINING share of the caller's single lock budget, not a
// fresh per-attempt deadline; see trustWriteRetryBackoff's latency note.
//
// MUST be called with trustWriteMu held — it reads trustPostWriteHook.
func trustUpsertOnce(worktreePath, cfgPath string, lockTimeout time.Duration) error {
	// Acquire the bounded exclusive flock on the sidecar lockfile (LOCK_EX|LOCK_NB
	// + deadline) to guard against OTHER PROCESSES rewriting ~/.claude.json
	// concurrently. The sidecar pattern keeps the lock independent of the target
	// file's inode across atomic renames. Note this only excludes writers that
	// cooperate — see EnsureWorktreeTrust's "Verify-and-repair" section.
	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: open lockfile %s: %w", lockPath, err)
	}
	defer lockFd.Close() //nolint:errcheck // closing a lock fd; error is non-actionable and lock is advisory

	if err := acquireExclusiveBounded(int(lockFd.Fd()), lockTimeout); err != nil {
		return err
	}
	// Lock is released automatically when lockFd is closed by the deferred call.

	// Read existing config, or start from an empty map.
	var cfg map[string]interface{}
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
			// Malformed ~/.claude.json: fail rather than silently corrupt.
			return fmt.Errorf("workspace: EnsureWorktreeTrust: parse %s: %w", cfgPath, jsonErr)
		}
	case os.IsNotExist(err):
		cfg = make(map[string]interface{})
	default:
		return fmt.Errorf("workspace: EnsureWorktreeTrust: read %s: %w", cfgPath, err)
	}

	// Navigate to cfg["projects"] map.
	var projects map[string]interface{}
	if raw, ok := cfg["projects"]; ok && raw != nil {
		projects, ok = raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("workspace: EnsureWorktreeTrust: ~/.claude.json projects field has unexpected type %T", raw)
		}
	} else {
		projects = make(map[string]interface{})
		cfg["projects"] = projects
	}

	// Upsert the per-project entry for worktreePath.
	var projectEntry map[string]interface{}
	if raw, ok := projects[worktreePath]; ok && raw != nil {
		projectEntry, ok = raw.(map[string]interface{})
		if !ok {
			// Unexpected shape — replace with a minimal entry.
			projectEntry = make(map[string]interface{})
		}
	} else {
		projectEntry = make(map[string]interface{})
	}

	// Re-check under the lock: a cooperating writer may have trusted this path
	// between our lock-free probe and acquiring the lock. If so, skip the rewrite —
	// the caller's verification read will confirm it independently.
	if t, ok := projectEntry["hasTrustDialogAccepted"].(bool); ok && t {
		return nil
	}

	// hasTrustDialogAccepted is the operative key for Claude Code's folder-trust
	// gate — confirmed empirically on 2.1.201: seeding this key alone boots a
	// daemon-spawned pane clean with no "Is this a project you trust?" modal
	// (2026-07-06 trust-modal investigation). The separate "pre-approves N tool
	// permissions" consent modal is NOT a trust-key concern — it is driven by the
	// permissions.allow block in the worktree settings.json, which harmonik no
	// longer writes (see claudesettings_wm040a.go). hasCompletedProjectOnboarding
	// is NOT written here: it is not the operative key, and requiring it in the
	// already-trusted probe would defeat the hk-bfvby lock-free fast path (every
	// legacy single-key entry would take the LOCK_EX write path).
	projectEntry["hasTrustDialogAccepted"] = true
	projects[worktreePath] = projectEntry
	cfg["projects"] = projects

	// Marshal and atomically write back.
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: marshal: %w", err)
	}
	out = append(out, '\n')

	if err := atomicWriteWithParentFsync(cfgPath, out); err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: write %s: %w", cfgPath, err)
	}

	// Test seam only (hk-qx065): stands in for the external process that rewrites
	// the config out from under us. Fires here — after our rename, before the
	// caller's verification read — because that is precisely the window the real
	// clobber lands in. nil in production.
	if trustPostWriteHook != nil {
		trustPostWriteHook(cfgPath)
	}

	return nil
}

// claudeDefaultTheme is the value seeded into ~/.claude.json["theme"] when it is
// unset, to suppress Claude Code's first-run THEME-SELECTION modal (hk-oga33).
// Claude Code >= 2.1.214 renders an interactive "Choose the text style … / Dark
// mode" onboarding modal at Stage 1 (before SessionStart) whenever the top-level
// "theme" key is null/absent, and --dangerously-skip-permissions does NOT suppress
// it (that covers only the trust modal, HC-055b). A daemon-spawned pane parks on
// it forever → SessionStart never fires → agent_ready times out at 150s. "dark"
// mirrors the modal's own default-highlighted option. Kept in sync with the remote
// worker path (workerThemeUpsertProgram in remotematerialize.go).
const claudeDefaultTheme = "dark"

// EnsureClaudeTheme pre-seeds ~/.claude.json's top-level "theme" key (when unset)
// so a daemon-spawned Claude Code pane does not wedge on the first-run theme modal
// (hk-oga33). It is the theme-modal analogue of EnsureWorktreeTrust's trust-modal
// mitigation, and mirrors its concurrency model: a lock-free probe fast path (theme
// is a GLOBAL key, so once any launch seeds it, every later launch short-circuits
// with no lock), and a bounded sidecar-flock read-modify-write only when a mutation
// is needed. It shares trustWriteMu and the ~/.claude.json.lock sidecar with the
// trust writer so the two never race the same config within the process.
//
// Only seeds when "theme" is absent, null, or an empty string — an operator's
// explicit theme choice is always preserved. MUST be called BEFORE exec'ing Claude
// (same ordering obligation as EnsureWorktreeTrust). Non-fatal by contract at the
// call site: a theme-seed failure should not harder-fail a launch than the trust
// seed does, but the function returns the error so the caller can decide.
func EnsureClaudeTheme() error {
	return ensureClaudeThemeAt(claudeGlobalConfigPath())
}

// ensureClaudeThemeAt is the testable inner implementation; cfgPath is the
// ~/.claude.json override, allowing unit tests to redirect to a temp file.
func ensureClaudeThemeAt(cfgPath string) error {
	// Fast path: lock-free probe. Theme is global, so once set this exits with no
	// lock for every subsequent launch (mirrors alreadyTrustedAt).
	set, probeErr := themeSetAt(cfgPath)
	if probeErr != nil {
		return probeErr
	}
	if set {
		return nil
	}

	// Slow path: serialize in-process (hk-z16) then take the bounded sidecar flock,
	// re-checking after each barrier — a predecessor may have just seeded the theme.
	trustWriteMu.Lock()
	defer trustWriteMu.Unlock()
	if set2, err2 := themeSetAt(cfgPath); err2 != nil {
		return err2
	} else if set2 {
		return nil
	}

	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: EnsureClaudeTheme: open lockfile %s: %w", lockPath, err)
	}
	defer lockFd.Close() //nolint:errcheck // closing a lock fd; error is non-actionable and lock is advisory

	if err := acquireExclusiveBounded(int(lockFd.Fd()), defaultTrustLockTimeout); err != nil {
		return err
	}

	var cfg map[string]interface{}
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
			return fmt.Errorf("workspace: EnsureClaudeTheme: parse %s: %w", cfgPath, jsonErr)
		}
	case os.IsNotExist(err):
		cfg = make(map[string]interface{})
	default:
		return fmt.Errorf("workspace: EnsureClaudeTheme: read %s: %w", cfgPath, err)
	}

	// Re-check under the lock, then seed only when absent/null/empty (never clobber
	// an operator's explicit choice).
	if s, ok := cfg["theme"].(string); ok && s != "" {
		return nil
	}
	cfg["theme"] = claudeDefaultTheme

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: EnsureClaudeTheme: marshal: %w", err)
	}
	out = append(out, '\n')
	if err := atomicWriteWithParentFsync(cfgPath, out); err != nil {
		return fmt.Errorf("workspace: EnsureClaudeTheme: write %s: %w", cfgPath, err)
	}
	return nil
}

// themeSetAt reports whether ~/.claude.json already has a non-empty top-level
// "theme" string, taking NO flock (mirrors alreadyTrustedAt's lock-free probe).
// A missing/unparseable file or absent/null/empty theme reports (false, nil):
// proceed to seed. Only genuinely reports an error the write path would also hit.
func themeSetAt(cfgPath string) (bool, error) {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("workspace: EnsureClaudeTheme: read %s: %w", cfgPath, err)
	}
	var cfg map[string]interface{}
	if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
		// A malformed config is not the theme-probe's concern to fix; report
		// not-set so the (locked) trust/theme write path is the single place that
		// fail-rather-than-corrupts on a truly malformed file.
		//nolint:nilerr // intentional: a malformed config reports not-set (proceed to seed); the locked write path is the single fail-rather-than-corrupt place
		return false, nil
	}
	s, ok := cfg["theme"].(string)
	return ok && s != "", nil
}

// alreadyTrustedAt reports whether ~/.claude.json already records worktreePath
// as trusted (hasTrustDialogAccepted == true), taking NO flock at all (hk-bfvby).
//
// This is deliberately lock-free. The write path commits via an atomic
// temp-file + rename (atomicWriteWithParentFsync), so any concurrent reader
// observes either the complete old file or the complete new file — never a torn
// one. A SHARED lock would buy nothing for a rename-atomic file yet would
// reintroduce exactly the contention this fix removes: LOCK_SH blocks on an
// active LOCK_EX writer, so an already-trusted daemon launch would again starve
// behind the ~23 writers rewriting the bloated config. By reading without a
// lock, the overwhelmingly-common already-trusted launch never waits on the
// write path at all.
//
// Correctness under races: the probe only ever produces a MISS (returns false)
// if it happens to read a snapshot in which the entry is absent/untrusted; the
// caller then falls through to the bounded write path and RE-CHECKS under
// LOCK_EX, so a concurrent writer can never cause a missed-or-false trust write.
// A snapshot that already shows the entry trusted is authoritative.
//
// A missing config file, a missing entry, or any shape mismatch reports
// (false, nil): not-yet-trusted, proceed to the write path. Only a malformed
// (unparseable) config returns a non-nil error, matching the write path's
// fail-rather-than-corrupt contract.
func alreadyTrustedAt(worktreePath, cfgPath string) (bool, error) {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("workspace: EnsureWorktreeTrust: read %s: %w", cfgPath, err)
	}

	var cfg map[string]interface{}
	if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
		return false, fmt.Errorf("workspace: EnsureWorktreeTrust: parse %s: %w", cfgPath, jsonErr)
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		return false, nil
	}
	entry, ok := projects[worktreePath].(map[string]interface{})
	if !ok {
		return false, nil
	}
	trusted, _ := entry["hasTrustDialogAccepted"].(bool)
	return trusted, nil
}

// acquireExclusiveBounded acquires an advisory exclusive flock on fd, retrying
// the non-blocking LOCK_EX|LOCK_NB attempt every trustLockRetryInterval until it
// succeeds or timeout elapses (hk-bfvby). On timeout it returns
// ErrTrustLockTimeout (wrapping handlercontract.ErrStructural) so the dispatch
// path fails the launch fast and reopens the bead, rather than blocking
// indefinitely behind unfair flock waiters.
func acquireExclusiveBounded(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			// A non-contention error (EBADF, EINTR-after-retries, etc.): surface it.
			return fmt.Errorf("workspace: EnsureWorktreeTrust: flock LOCK_EX: %w", err)
		}
		if time.Now().After(deadline) {
			return ErrTrustLockTimeout
		}
		time.Sleep(trustLockRetryInterval)
	}
}

// PruneWorktreeTrust removes the per-worktree trust entry for worktreePath from
// ~/.claude.json (hk-bfvby). Harmonik creates one ephemeral worktree per bead
// run and never reuses the path; without cleanup the "projects" map grows
// unbounded (the observed 36.6k leaked keys / 8.6MB bloat that, combined with
// the per-call rewrite, produced the spawn stall). The daemon calls this when it
// removes a worktree so the trust map tracks the live-worktree set instead of
// accumulating forever.
//
// Best-effort: a missing config, missing entry, or write failure is non-fatal
// (returns nil for the absent cases; an error only when the existing config is
// malformed or the bounded lock cannot be acquired). It takes the same bounded
// exclusive lock as the write path so it never wedges the daemon. When the entry
// is absent it does NOT rewrite the file.
func PruneWorktreeTrust(worktreePath string) error {
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		worktreePath = resolved
	}
	return pruneWorktreeTrustAt(worktreePath, claudeGlobalConfigPath())
}

// pruneWorktreeTrustAt is the testable inner implementation of PruneWorktreeTrust.
func pruneWorktreeTrustAt(worktreePath, cfgPath string) error {
	// Serialize in-process writes (hk-z16): hold trustWriteMu alongside the flock
	// so prune and ensure never contend within the daemon process.
	trustWriteMu.Lock()
	defer trustWriteMu.Unlock()

	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: PruneWorktreeTrust: open lockfile %s: %w", lockPath, err)
	}
	defer lockFd.Close() //nolint:errcheck // closing a lock fd; error is non-actionable and lock is advisory

	if err := acquireExclusiveBounded(int(lockFd.Fd()), defaultTrustLockTimeout); err != nil {
		return err
	}

	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to prune
		}
		return fmt.Errorf("workspace: PruneWorktreeTrust: read %s: %w", cfgPath, err)
	}

	var cfg map[string]interface{}
	if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
		return fmt.Errorf("workspace: PruneWorktreeTrust: parse %s: %w", cfgPath, jsonErr)
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		return nil
	}
	if _, present := projects[worktreePath]; !present {
		return nil // entry absent; do NOT rewrite
	}
	delete(projects, worktreePath)
	cfg["projects"] = projects

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: PruneWorktreeTrust: marshal: %w", err)
	}
	out = append(out, '\n')

	if err := atomicWriteWithParentFsync(cfgPath, out); err != nil {
		return fmt.Errorf("workspace: PruneWorktreeTrust: write %s: %w", cfgPath, err)
	}
	return nil
}
