package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

const defaultTrustLockTimeout = 15 * time.Second

const trustLockRetryInterval = 50 * time.Millisecond

const trustWriteMaxAttempts = 4

const trustWriteRetryBackoff = 200 * time.Millisecond

var trustPostWriteHook func(cfgPath string)

// ErrTrustLockTimeout is returned when a writer of the shared Claude config
// cannot acquire the exclusive write lock within its budget (hk-bfvby). It wraps
// handlercontract.ErrStructural so the daemon dispatch path classifies the
// launch failure as structural (reopen-the-bead) rather than hanging. The
// already-trusted / already-set fast paths NEVER return it — they take no write
// lock.
//
// Every writer of that file returns it, not one: EnsureWorktreeTrust and
// PruneWorktreeTrust in process, and EnsureWorktreeTrustVia when the worker
// program exits with workerConfigLockTimeoutExit. So the text names no function.
//
// It names no file either. The config path is what the environment configures it
// to be (claudeGlobalConfigPath), so a fixed "~/.claude.json" in the text is
// wrong whenever an override is in force. The remote callers append the worker's
// own message, which names the exact config file and lock file it waited on.
var ErrTrustLockTimeout = fmt.Errorf("workspace: %w: write-lock acquire timed out on the Claude config", handlercontract.ErrStructural)

// ErrTrustConfigUnparseable is returned when the Claude config on disk cannot be
// parsed as a JSON object and so cannot be safely updated in place. Structural,
// for the same reason ErrTrustLockTimeout is: the launch must stop rather than
// exec claude into a folder whose trust entry was never written.
//
// The alternative is worse than a failed launch. Treating an unreadable config
// as an absent one means writing a fresh file over it, which discards every
// other project entry and every other top-level key. Failing here keeps the
// file intact for whoever looks at it next.
//
// Only the remote path returns this today. The in-process path can retry a
// decode failure within its write budget — a torn read of a foreign writer's
// non-atomic rewrite usually resolves on the next read — and returns the decode
// error itself when it does not. The worker program has no such loop, so it
// reports the condition instead of guessing at it.
var ErrTrustConfigUnparseable = fmt.Errorf("workspace: %w: the Claude config is not a JSON object and was left untouched", handlercontract.ErrStructural)

var trustWriteMu sync.Mutex

var claudeGlobalConfigPath = defaultClaudeGlobalConfigPath

func defaultClaudeGlobalConfigPath() string {
	if p := claudeConfigPathForWorker(); p != "" {
		return p
	}
	if dir := os.Getenv("CLAUDE_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("workspace: claudeGlobalConfigPath: UserHomeDir: %v", err))
	}
	return filepath.Join(home, ".claude.json")
}

func claudeConfigPathForWorker() string {
	return os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH")
}

// DefaultClaudeProjectsDir returns the directory where Claude Code keeps its
// per-project session transcripts. Precedence (first match wins):
//
//  1. HARMONIK_CLAUDE_PROJECTS_DIR — the full directory path. This is the test
//     seam. Without it a test reads the operator's real transcript store, which
//     was measured at 1.9 GB on the development machine and is empty on a fresh
//     one, so the same test gives two different answers on two machines.
//  2. CLAUDE_CONFIG_HOME/projects — Claude Code's own directory convention, the
//     same variable defaultClaudeGlobalConfigPath honours.
//  3. ~/.claude/projects — the production default.
//
// The sibling of defaultClaudeGlobalConfigPath: one answers "where is Claude's
// config file", this one answers "where are Claude's transcripts", and both
// have to be redirectable for a test run to be independent of the host.
func DefaultClaudeProjectsDir() string {
	if p := os.Getenv("HARMONIK_CLAUDE_PROJECTS_DIR"); p != "" {
		return p
	}
	if dir := os.Getenv("CLAUDE_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
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
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		worktreePath = resolved
	}
	cfgPath := claudeGlobalConfigPath()
	return ensureWorktreeTrustAt(worktreePath, cfgPath)
}

func ensureWorktreeTrustAt(worktreePath, cfgPath string) error {
	trusted, probeErr := alreadyTrustedAt(worktreePath, cfgPath)
	if probeErr != nil && !trustConfigDecodeErr(probeErr) {
		return probeErr
	}
	if trusted {
		return nil
	}

	trustWriteMu.Lock()
	defer trustWriteMu.Unlock()

	if trusted2, err2 := alreadyTrustedAt(worktreePath, cfgPath); err2 != nil && !trustConfigDecodeErr(err2) {
		return err2
	} else if trusted2 {
		return nil
	}

	lockDeadline := time.Now().Add(defaultTrustLockTimeout)

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
			var persisted bool
			persisted, err = alreadyTrustedAt(worktreePath, cfgPath)
			if err == nil && persisted {
				return nil
			}
		}

		if err != nil && !trustConfigDecodeErr(err) {
			return err
		}
		lastErr = err
	}

	if lastErr != nil {
		return lastErr
	}

	return fmt.Errorf("workspace: EnsureWorktreeTrust: %w: projects[%q].hasTrustDialogAccepted did not persist in %s after %d write attempts; "+
		"the most likely cause is a concurrent writer rewriting the shared config wholesale (live claude processes do this and do not honor our advisory lock)",
		handlercontract.ErrStructural, worktreePath, cfgPath, trustWriteMaxAttempts)
}

func trustConfigDecodeErr(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}

func readClaudeConfigMap(cfgPath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	switch {
	case err == nil:
		var cfg map[string]interface{}
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
			return nil, fmt.Errorf("workspace: EnsureWorktreeTrust: parse %s: %w", cfgPath, jsonErr)
		}
		if cfg == nil {
			cfg = make(map[string]interface{})
		}
		return cfg, nil
	case os.IsNotExist(err):
		return make(map[string]interface{}), nil
	default:
		return nil, fmt.Errorf("workspace: EnsureWorktreeTrust: read %s: %w", cfgPath, err)
	}
}

func claudeProjectsMap(cfg map[string]interface{}) (map[string]interface{}, error) {
	if raw, ok := cfg["projects"]; ok && raw != nil {
		projects, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("workspace: EnsureWorktreeTrust: ~/.claude.json projects field has unexpected type %T", raw)
		}
		return projects, nil
	}
	projects := make(map[string]interface{})
	cfg["projects"] = projects
	return projects, nil
}

func claudeProjectEntry(projects map[string]interface{}, worktreePath string) map[string]interface{} {
	if raw, ok := projects[worktreePath]; ok && raw != nil {
		if entry, ok := raw.(map[string]interface{}); ok {
			return entry
		}
	}
	return make(map[string]interface{})
}

func trustUpsertOnce(worktreePath, cfgPath string, lockTimeout time.Duration) error {
	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: open lockfile %s: %w", lockPath, err)
	}
	defer func() {
		if closeErr := lockFd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "workspace: EnsureWorktreeTrust: close lockfile", "err", closeErr, "path", lockPath)
		}
	}()

	if err := acquireExclusiveBounded(int(lockFd.Fd()), lockTimeout); err != nil {
		return err
	}

	cfg, err := readClaudeConfigMap(cfgPath)
	if err != nil {
		return err
	}

	projects, err := claudeProjectsMap(cfg)
	if err != nil {
		return err
	}
	projectEntry := claudeProjectEntry(projects, worktreePath)

	if t, ok := projectEntry["hasTrustDialogAccepted"].(bool); ok && t {
		return nil
	}

	projectEntry["hasTrustDialogAccepted"] = true
	projects[worktreePath] = projectEntry
	cfg["projects"] = projects

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: marshal: %w", err)
	}
	out = append(out, '\n')

	if err := atomicWriteWithParentFsync(cfgPath, out); err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: write %s: %w", cfgPath, err)
	}

	if trustPostWriteHook != nil {
		trustPostWriteHook(cfgPath)
	}

	return nil
}

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
	trusted, ok := entry["hasTrustDialogAccepted"].(bool)
	if !ok {
		return false, nil
	}
	return trusted, nil
}

func acquireExclusiveBounded(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
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

func pruneWorktreeTrustAt(worktreePath, cfgPath string) error {
	trustWriteMu.Lock()
	defer trustWriteMu.Unlock()

	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: sidecar lockfile path is derived from user's own config path
	if err != nil {
		return fmt.Errorf("workspace: PruneWorktreeTrust: open lockfile %s: %w", lockPath, err)
	}
	defer func() {
		if closeErr := lockFd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "workspace: PruneWorktreeTrust: close lockfile", "err", closeErr, "path", lockPath)
		}
	}()

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
