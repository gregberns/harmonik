package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// ErrTrustLockTimeout is returned when ensureWorktreeTrustAt cannot acquire the
// exclusive write lock within defaultTrustLockTimeout (hk-bfvby). It wraps
// handlercontract.ErrStructural so the daemon dispatch path classifies the
// launch failure as structural (reopen-the-bead) rather than hanging. The
// already-trusted fast path NEVER returns this error — it takes no write lock.
var ErrTrustLockTimeout = fmt.Errorf("workspace: EnsureWorktreeTrust: %w: write-lock acquire timed out (contended ~/.claude.json)", handlercontract.ErrStructural)

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
// On any error (lock, read, parse, marshal, write), EnsureWorktreeTrust returns
// a wrapped error. The caller MUST propagate this as a structural error and MUST
// NOT exec Claude — an un-trusted session would block rather than hang silently.
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
// ~/.claude.json override, allowing unit tests to redirect to a temp file.
//
// hk-bfvby: the already-trusted case takes NO write lock and performs NO
// read-modify-write. Only an actual mutation acquires the bounded LOCK_EX.
func ensureWorktreeTrustAt(worktreePath, cfgPath string) error {
	// Fast path: lock-free read-only probe. If worktreePath is already present
	// and trusted, return immediately — no LOCK_EX, no rewrite. This is the
	// overwhelmingly-common case (every repeat launch) and is the one that must
	// NOT contend on the write lock. A concurrent writer mid-rename only ever
	// makes this probe MISS (it then falls through to the locked write path and
	// re-checks under the lock), never produces a false trust.
	trusted, probeErr := alreadyTrustedAt(worktreePath, cfgPath)
	if probeErr != nil {
		return probeErr
	}
	if trusted {
		return nil
	}

	// Slow path: a mutation is needed. Acquire the bounded exclusive flock on the
	// sidecar lockfile (LOCK_EX|LOCK_NB + deadline) so a pathologically-contended
	// config cannot wedge the launch for minutes. The sidecar pattern (rather than
	// locking cfgPath directly) keeps the lock independent of the target file's
	// inode across atomic renames.
	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: open lockfile %s: %w", lockPath, err)
	}
	defer lockFd.Close() //nolint:errcheck // closing a lock fd; error is non-actionable and lock is advisory

	if err := acquireExclusiveBounded(int(lockFd.Fd()), defaultTrustLockTimeout); err != nil {
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

	// Re-check under the lock: a concurrent writer may have trusted this path
	// between our lock-free probe and acquiring the lock. If so, skip the rewrite.
	if t, ok := projectEntry["hasTrustDialogAccepted"].(bool); ok && t {
		return nil
	}

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

	return nil
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
