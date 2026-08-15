package workspace

// remotematerialize.go — SSH-aware variants of the three claude-launch
// materialization writes for remote-substrate runs (hk-z8ek).
//
// # Why this exists
//
// buildClaudeLaunchSpec materializes three per-launch artifacts into the run's
// worktree before the agent is spawned:
//
//  1. .claude/settings.json   — the hook-bridge config (MaterializeClaudeSettings)
//  2. .harmonik/agent-task.md — the per-launch task brief    (WriteAgentTask)
//  3. ~/.claude.json trust     — the worktree-trust entry      (EnsureWorktreeTrust)
//
// All three use box-A-local os.MkdirAll/os.WriteFile. For a LOCAL run that is
// correct: the worktree lives on box A's filesystem. For a REMOTE run (the bead
// is dispatched to an SSH worker) the worktree lives on the WORKER's filesystem,
// so a box-A-local write lands the hook config on the wrong machine — box A grows
// orphan files at the worker's mirror path and the worker's claude launches with
// NO hook installed, never dials the daemon socket, and times out at
// agent_ready_timeout (the hk-z8ek symptom).
//
// The *Via helpers below route each write THROUGH a tmux.CommandRunner so the
// content (generated on box A exactly as today) is written onto the WORKER's
// filesystem. A nil runner short-circuits to the existing box-A-local function,
// byte-for-byte unchanged (NFR7 — local runs MUST NOT change).
//
// # Remote-write mechanism
//
// The robust, content-agnostic pattern (already proven by the worker probe:
// gb-mbp has /usr/bin/base64 and a POSIX sh): base64-encode the file content on
// box A, then run on the worker through the runner:
//
//	sh -lc "mkdir -p '<dir>' && printf %s '<b64>' | base64 -d > '<file>'"
//
// base64 sidesteps all content quoting; only the directory and file paths are
// single-quoted (worktree paths are operator-sanctioned, never contain a single
// quote, but the helper escapes one anyway for safety). This mirrors the
// existing remote-command idiom in internal/transport/tunnel
// (tunnel.EnsureWorkerHarmonikDir) and internal/transport/codesync (the DD1
// fetch-base step), which all run `runner.Command(...).CombinedOutput()`.
//
// Spec refs:
//   - claude-hook-bridge.md §4.1 CHB-001..005 (settings), §4.11 CHB-028
//     (agent-task), §4.12 CHB-029 / workspace-model.md §4.7b WM-040b (trust).
//   - remote-substrate gap #7 + B7/B8 (SSH worktree + code-sync seam).
//
// Bead: hk-z8ek, hk-rs-phase1-qfn1

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// shellSingleQuote wraps s in single quotes safe for a POSIX sh command line,
// escaping any embedded single quote via the '\” idiom. Used only for the
// directory and file PATHS in the remote-write command; the file CONTENT is
// base64-encoded and never needs quoting.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeRemoteFile writes content to absPath on the host reached by runner,
// creating parent directories as needed. It is the single small remote
// file-write helper shared by the *Via materializers (hk-z8ek).
//
// The command issued is:
//
//	sh -lc "mkdir -p '<dir>' && printf %s '<base64(content)>' | base64 -d > '<absPath>'"
//
// runner MUST be non-nil (callers gate on a present runner before calling).
func writeRemoteFile(ctx context.Context, runner tmux.CommandRunner, absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	b64 := base64.StdEncoding.EncodeToString(content)
	script := fmt.Sprintf("mkdir -p %s && printf %%s %s | base64 -d > %s",
		shellSingleQuote(dir), shellSingleQuote(b64), shellSingleQuote(absPath))
	out, err := runner.Command(ctx, "sh", "-lc", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: writeRemoteFile %s: %w\nremote: %s", absPath, err, out)
	}
	return nil
}

// removeRemoteFile removes absPath on the host reached by runner via `rm -f`,
// which is a no-op when the file is absent (mirrors os.Remove's tolerate-missing
// use at the call sites, where the error is discarded). runner MUST be non-nil.
func removeRemoteFile(ctx context.Context, runner tmux.CommandRunner, absPath string) error {
	script := fmt.Sprintf("rm -f %s", shellSingleQuote(absPath))
	out, err := runner.Command(ctx, "sh", "-lc", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: removeRemoteFile %s: %w\nremote: %s", absPath, err, out)
	}
	return nil
}

// WriteFileVia writes content to absPath either onto the worker (runner != nil)
// or onto box A's local filesystem (runner == nil, byte-identical to a plain
// os.MkdirAll+os.WriteFile — NFR7). Generic content-agnostic sibling of
// WriteReviewTargetVia/WriteAgentTaskVia for callers with no dedicated payload
// type (e.g. the DOT cognition-gate task brief, hk-9fe2).
func WriteFileVia(ctx context.Context, runner tmux.CommandRunner, absPath string, content []byte, perm os.FileMode) error {
	if runner == nil {
		if err := os.MkdirAll(filepath.Dir(absPath), core.HarmonikDirMode); err != nil {
			return fmt.Errorf("workspace: WriteFileVia mkdir: %w", err)
		}
		return os.WriteFile(absPath, content, perm)
	}
	return writeRemoteFile(ctx, runner, absPath, content)
}

// RemoveFileVia removes absPath either on the worker (runner != nil) or on box
// A's local filesystem (runner == nil, byte-identical to os.Remove — NFR7).
// Generic content-agnostic sibling of RemoveReviewVerdictVia for callers with
// no dedicated path helper (e.g. the DOT cognition-gate verdict, hk-9fe2).
func RemoveFileVia(ctx context.Context, runner tmux.CommandRunner, absPath string) error {
	if runner == nil {
		return os.Remove(absPath)
	}
	return removeRemoteFile(ctx, runner, absPath)
}

// WriteReviewTargetVia writes the per-launch review-target.md either onto the
// worker (runner != nil) or onto box A's local filesystem (runner == nil,
// byte-identical to WriteReviewTarget — NFR7).
//
// On a REMOTE DOT-mode run the reviewer node runs in the WORKER's worktree, so a
// box-A-local WriteReviewTarget lands the reviewer brief on the wrong machine —
// box A grows an orphan .harmonik tree at the worker's mirror path and the worker
// reviewer never receives its instruction, idles, and writes no review.json
// ("reviewer node produced no verdict"). Routing the write through the runner
// mirrors WriteAgentTaskVia and puts the brief on the worker where the reviewer
// (and the paste-inject stat check) look for it.
//
// The content is generated by the SAME buildReviewTargetContent used by the local
// path, so the bytes are identical to what WriteReviewTarget writes today. Like
// WriteAgentTaskVia, the remote path always writes the current-iteration content
// (a remote worktree is created fresh per run; there is never a prior remote
// review-target.md to preserve).
func WriteReviewTargetVia(ctx context.Context, runner tmux.CommandRunner, payload ReviewTargetPayload) error {
	if runner == nil {
		return WriteReviewTarget(payload)
	}

	target := ReviewTargetPath(payload.WorkspacePath)
	content := buildReviewTargetContent(payload)
	if err := writeRemoteFile(ctx, runner, target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewTargetVia: %w", err)
	}
	return nil
}

// RemoveReviewVerdictVia removes any stale .harmonik/review.json either on the
// worker (runner != nil) or on box A's local filesystem (runner == nil,
// byte-identical to the os.Remove call it replaces — NFR7). Tolerates a missing
// file (rm -f / os.Remove of an absent path). Callers discard the error, as the
// stale-verdict cleanup is best-effort.
func RemoveReviewVerdictVia(ctx context.Context, runner tmux.CommandRunner, workspacePath string) error {
	verdictPath := ReviewVerdictPath(workspacePath)
	if runner == nil {
		return os.Remove(verdictPath)
	}
	return removeRemoteFile(ctx, runner, verdictPath)
}

// MaterializeClaudeSettingsVia writes the hook-bridge settings.json either onto
// the worker (runner != nil) or onto box A's local filesystem (runner == nil,
// byte-identical to MaterializeClaudeSettings — NFR7).
//
// For the REMOTE path the merge-with-existing semantics of CHB-004 are NOT
// reproduced: a freshly-created remote worktree never carries a pre-existing
// .claude/settings.json (the worktree is created clean from the base SHA per
// B7), so the bridge-only content is the correct and complete file. This avoids
// a remote read-merge round-trip; the settings content is identical to what the
// local "file absent" branch of MaterializeClaudeSettings produces.
//
// daemonBinaryPath MUST be the WORKER's harmonik path for a remote run (the
// hook "command" field is executed ON THE WORKER); the caller resolves it.
func MaterializeClaudeSettingsVia(ctx context.Context, runner tmux.CommandRunner, workspacePath, daemonBinaryPath, sessionLogPath string) error {
	if runner == nil {
		return MaterializeClaudeSettings(workspacePath, daemonBinaryPath, sessionLogPath)
	}

	settingsPath := ClaudeSettingsPath(workspacePath)
	merged := buildBridgeOnlySettings(daemonBinaryPath)
	delete(merged, "disableAllHooks") // parity with the local path (CHB-004)

	content, err := marshalSettings(merged)
	if err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettingsVia: marshal: %w", err)
	}
	if err := writeRemoteFile(ctx, runner, settingsPath, content); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettingsVia: %w", err)
	}
	return nil
}

// WriteAgentTaskVia writes the per-launch agent-task.md either onto the worker
// (runner != nil) or onto box A's local filesystem (runner == nil,
// byte-identical to WriteAgentTask — NFR7).
//
// The Body-non-empty validation matches WriteAgentTask (ErrTaskFileEmpty). The
// ReAttach short-circuit is intentionally NOT reproduced on the remote path: a
// remote worktree is created fresh per run, so there is never a prior remote
// agent-task.md to re-attach to; always writing the current (run, phase,
// iteration) content is correct.
func WriteAgentTaskVia(ctx context.Context, runner tmux.CommandRunner, workspacePath string, payload AgentTaskPayload) error {
	if runner == nil {
		return WriteAgentTask(workspacePath, payload)
	}
	if strings.TrimSpace(payload.Body) == "" {
		return fmt.Errorf("%w: payload.Body is empty for bead %q run %q",
			ErrTaskFileEmpty, payload.BeadID, payload.RunID)
	}

	target := AgentTaskPath(workspacePath)
	content := buildAgentTaskContent(payload)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: constructed content is empty for bead %q run %q",
			ErrTaskFileEmpty, payload.BeadID, payload.RunID)
	}
	if err := writeRemoteFile(ctx, runner, target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteAgentTaskVia: %w", err)
	}
	return nil
}

// EnsureWorktreeTrustVia pre-seeds the trust entry for worktreePath either in
// the WORKER's ~/.claude.json (runner != nil) or box A's (runner == nil,
// byte-identical to EnsureWorktreeTrust — NFR7).
//
// # Why the remote path is a single idempotent shell upsert
//
// Unlike settings.json and agent-task.md (worktree-relative writes), the trust
// entry lives in the worker user's HOME config (~/.claude.json), keyed by the
// ABSOLUTE worktree path AFTER realpath() normalization — Claude Code looks the
// key up under its own realpath() of the cwd. Box A cannot compute the worker's
// realpath() without a round-trip, and the daemon must NOT clobber a worker
// ~/.claude.json that may carry the operator's own projects/auth. So the remote
// path runs a small, idempotent, dependency-light shell upsert ON THE WORKER
// that (a) realpath-normalizes the worktree path on the worker itself, (b)
// read-merge-writes ~/.claude.json setting
// projects[<realpath>].hasTrustDialogAccepted = true, preserving every other
// key, and (c) is a no-op when already trusted.
//
// The upsert is performed by a small Python program (python3 is present on the
// macOS worker — verified by probe). Python's json module is stdlib, so no extra
// install is needed.
//
// # Python floor: 3.6
//
// The program uses f-strings in its lock-timeout diagnostic, which need python
// 3.6 or later. Everything else in it (os.replace, time.monotonic) needs only
// 3.3. The floor is safe on the worker this runs on: macOS first shipped a
// python3 at Catalina and it was 3.7, and every macOS since has shipped 3.8 or
// later. A worker below the floor also fails loudly rather than silently — the
// program is rejected at parse time with a SyntaxError on every launch, not on
// some rare branch. Rewriting the diagnostic with .format() would return the
// floor to 3.3 at the cost of a message that is harder to read and easier to get
// wrong, which is the wrong trade for a floor nothing can reach.
//
// # What is interpolated and what rides argv
//
// The worktree path rides ARGV — the program reads it from sys.argv[1] and it is
// never interpolated into the program text, so it needs no escaping here.
//
// The config path and the lock budget ARE interpolated into the program text, by
// workerConfigProgramPrelude. They go in through %q, which emits a Go
// double-quoted literal whose escaping python's lexer reads the same way, so a
// path with a quote, a backslash or a newline in it stays one string literal and
// cannot close the literal and inject statements.
//
// The program text itself never reaches a shell: it rides stdin. The single-quote
// wrap that an earlier version of this comment described belonged to a retired
// `python3 -c <prog>` form.
func EnsureWorktreeTrustVia(ctx context.Context, runner tmux.CommandRunner, worktreePath string) error {
	if runner == nil {
		return EnsureWorktreeTrust(worktreePath)
	}

	// The Python program is fed to `python3 - <worktreePath>` ON STDIN, NOT via
	// `python3 -c <prog>`. This is load-bearing for the REMOTE (SSH) path:
	// tmux.SSHRunner produces `ssh <host> -- python3 -c <prog> <path>`, and the
	// ssh client space-JOINS those argv tokens into one remote command string that
	// the worker's LOGIN SHELL re-splits on whitespace. A multi-line `-c` program
	// is shredded by that re-split — python's `-c` receives only the first
	// whitespace token ("Argument expected for the -c option"; the rest run as
	// stray shell commands: `import: command not found`), so the upsert never
	// executes and the worker's ~/.claude.json never gets the worktree key
	// (hk-gglt: untrusted per-run worktree → trust/bypass modal → no_commit).
	//
	// Piping the program on stdin to `python3 -` sidesteps the re-split entirely:
	// the program bytes never appear on the remote command line. The worktree path
	// is the one argv token that DOES traverse the command line; it is a harmonik
	// per-run worktree path (a UUID run-id dir) that never contains whitespace, so
	// it survives the remote shell's word-splitting as a single sys.argv[1]. The
	// program defensively strips a surrounding pair of single quotes (a no-op for
	// the bare path; tolerant should a caller ever pre-quote it). It then
	// realpath-normalizes the path on the worker (mirrors EnsureWorktreeTrust's
	// filepath.EvalSymlinks) and upserts
	// ~/.claude.json["projects"][<realpath>]["hasTrustDialogAccepted"] = true,
	// writing atomically via a temp file + os.replace and preserving all other
	// keys. It is a no-op (no rewrite) when the entry is already trusted.
	cmd := runner.Command(ctx, "python3", "-", worktreePath)
	cmd.Stdin = bytes.NewReader([]byte(workerTrustUpsertProgram(claudeConfigPathForWorker(), defaultTrustLockTimeout)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if workerConfigLockTimedOut(err) {
			return fmt.Errorf("%w\nremote: %s", ErrTrustLockTimeout, out)
		}
		if workerConfigUnparseable(err) {
			return fmt.Errorf("%w\nremote: %s", ErrTrustConfigUnparseable, out)
		}
		return fmt.Errorf("workspace: EnsureWorktreeTrustVia %s: %w\nremote: %s", worktreePath, err, out)
	}
	return nil
}

// workerConfigLockTimeoutExit is the exit status the worker programs use for a
// lock-acquire timeout, so the Go caller can tell that failure apart from every
// other way python can exit non-zero and report it as the same structural error
// the in-process path reports (ErrTrustLockTimeout).
//
// 75 is EX_TEMPFAIL from sysexits.h — a temporary failure, try again later —
// which is what a contended lock is. It cannot be confused with ssh's own
// failure status (255) on a remote run, and ssh passes the remote command's exit
// status through unchanged, so the code survives the round trip.
const workerConfigLockTimeoutExit = 75

// workerConfigUnparseableExit is the exit status the worker programs use when
// the config on disk is not JSON, or is JSON but not an object. The Go caller
// turns it into ErrTrustConfigUnparseable.
//
// It exists because the alternative the program used to take was to treat an
// unreadable config as an absent one and write a fresh file over it, which
// discards every other project entry and every other top-level key on that
// worker. The in-process path has always refused that (readClaudeConfigMap
// returns a parse error and ensureWorktreeTrustAt reports it rather than
// overwriting), and the two paths write the same shape of file, so the remote
// one refuses too.
//
// A torn read is the expected way to reach this, not a corrupt disk: the writer
// this whole subsystem defends against rewrites the shared config wholesale and
// does not take our lock, so a reader can land mid-rewrite and see truncated
// JSON. The in-process path retries such a read within its budget before giving
// up. The remote program has no retry loop to hang that on, so it fails and
// says why — a launch that stops is recoverable, and a worker whose config was
// silently rebuilt is not.
//
// 65 is EX_DATAERR from sysexits.h — the input data was incorrect. Like 75 it
// cannot be confused with ssh's own 255, and ssh passes it through unchanged.
const workerConfigUnparseableExit = 65

// workerConfigLockTimedOut reports whether err is a worker program exiting with
// the lock-timeout status.
func workerConfigLockTimedOut(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == workerConfigLockTimeoutExit
}

// workerConfigUnparseable reports whether err is a worker program exiting with
// the unparseable-config status.
func workerConfigUnparseable(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == workerConfigUnparseableExit
}

// workerConfigProgramPrelude returns the head of the worker program that writes
// the SHARED Claude config: the trust upsert. It resolves the config path, and it
// defines the bounded lock acquire, the config read, and the atomic write that
// program uses.
//
// # Where the config path comes from
//
// The program applies the same three-step precedence as
// defaultClaudeGlobalConfigPath — HARMONIK_CLAUDE_CONFIG_PATH, then
// CLAUDE_CONFIG_HOME, then ~/.claude.json. The steps differ only in WHICH MACHINE
// evaluates them, and that split is the point:
//
//   - cfgPathForWorker is step 1, evaluated HERE and baked in as a value
//     (claudeConfigPathForWorker). It is harmonik's own test seam:
//     internal/testhelpers/hermetic points it at a temp file, and before this the
//     python program ignored it and locked and rewrote the operator's REAL
//     ~/.claude.json — three internal/daemon tests blocked about 50s each on that
//     lock, and the failure named the dispatch path instead (hk-g8d5x). Passing
//     it as a value rather than re-deriving it on the far side is what stops the
//     two ends disagreeing about which file they mean.
//   - Steps 2 and 3 are evaluated ON THE WORKER, in the worker's own environment.
//     CLAUDE_CONFIG_HOME is Claude Code's variable and it describes the box it is
//     set on; the claude that later reads this file is the WORKER's, and it reads
//     the WORKER's copy. ~ is the worker user's home, which box A cannot resolve.
//
// So a real remote run is unchanged even when the daemon box exports
// CLAUDE_CONFIG_HOME — which docs/live-twin-testing.md requires it to do. Baking
// box A's CLAUDE_CONFIG_HOME into the program instead would point the worker at a
// directory that need not exist there, and the program would die on the missing
// lock file and fail the launch.
//
// The residual case, stated plainly: an operator who exports
// HARMONIK_CLAUDE_CONFIG_PATH and dispatches to a real remote worker does send a
// box-A path across. That variable is an explicit instruction to write exactly
// that file, so obeying it is right; every setter of it in this repo runs the
// program on the box that set it.
//
// # Why the lock wait is bounded
//
// An unbounded flock(LOCK_EX) lets ONE stuck holder starve every later run with
// no diagnostic: the program never returns, the launch-spec build never
// finishes, and the run dies at some later deadline that blames another
// subsystem. lockTimeout is the same budget the in-process sibling
// acquireExclusiveBounded uses, polled at the same trustLockRetryInterval, and
// the message it prints names the lock file and how to find the holder.
//
// The program needs python 3.6 or later — see EnsureWorktreeTrustVia for the
// floor and why it is safe.
func workerConfigProgramPrelude(cfgPathForWorker string, lockTimeout time.Duration) string {
	return fmt.Sprintf(workerConfigProgramPreludeTemplate,
		cfgPathForWorker,
		lockTimeout.Seconds(),
		trustLockRetryInterval.Seconds(),
		workerConfigUnparseableExit,
		workerConfigLockTimeoutExit,
	)
}

const workerConfigProgramPreludeTemplate = `
import errno, fcntl, json, os, sys, tempfile, time

# Step 1 of the config-path precedence, decided by the caller and baked in as a
# value. "" means the caller had nothing to send, so this machine — the one that
# owns the file — resolves steps 2 and 3 from its own environment.
cfg_path = %[1]q
if not cfg_path:
    cfg_home = os.environ.get("CLAUDE_CONFIG_HOME", "")
    if cfg_home:
        cfg_path = os.path.join(cfg_home, ".claude.json")
    else:
        cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
lock_path = cfg_path + ".lock"
lock_timeout = %[2]v
lock_interval = %[3]v

def load_cfg():
    # Two cases legitimately mean "start fresh", and both hold nothing worth
    # keeping: a MISSING file, and a literal "null" body. Every other way this
    # read can fail means a file exists with content we cannot understand, and
    # the only safe move is to leave it alone. Returning {} for those would send
    # the caller on to write a fresh config over the top, discarding every other
    # project entry and every other key on this machine. The in-process sibling
    # draws the line in the same place; see readClaudeConfigMap and
    # ErrTrustConfigUnparseable.
    try:
        with open(cfg_path) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        return {}
    except ValueError:
        sys.stderr.write(
            f"harmonik: {cfg_path} is not valid JSON, so the trust entry was not "
            f"written and the file was left exactly as it is. A partial read of "
            f"another writer's rewrite looks like this and clears on its own; a "
            f"file that stays unreadable needs a look.\n"
        )
        sys.exit(%[4]d)
    if cfg is None:
        # A literal "null" body parses without error and holds nothing, so there
        # is nothing to lose by treating it as empty. The in-process sibling
        # makes the same exception for the same reason; see readClaudeConfigMap.
        # Every OTHER non-object -- an array, a string, a number -- is a file
        # with content we do not understand, and overwriting it would discard it.
        return {}
    if not isinstance(cfg, dict):
        sys.stderr.write(
            f"harmonik: {cfg_path} holds a JSON {type(cfg).__name__} where an "
            f"object is required, so the trust entry was not written and the file "
            f"was left exactly as it is.\n"
        )
        sys.exit(%[4]d)
    return cfg

def acquire_bounded(fd):
    # Same shape as internal/workspace acquireExclusiveBounded: retry the
    # NON-blocking LOCK_EX until it succeeds or the budget runs out, then fail
    # loudly. Only EWOULDBLOCK counts as contention, exactly as the Go side has
    # it; every other errno (EBADF, EACCES, ...) is a real fault and is raised at
    # once rather than retried for the whole budget and then misreported as a
    # lock timeout. Two differences from the Go side that do not change the
    # outcome: this uses a monotonic clock, so a wall-clock step cannot stretch
    # or shrink the budget, and its deadline test fires AT the deadline where Go
    # fires just after it — at most one poll interval apart.
    deadline = time.monotonic() + lock_timeout
    while True:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return
        except OSError as exc:
            if exc.errno != errno.EWOULDBLOCK:
                raise
            if time.monotonic() >= deadline:
                sys.stderr.write(
                    f"workspace: write-lock acquire timed out after {lock_timeout}s "
                    f"(contended {cfg_path})\n"
                    f"lock file: {lock_path}\n"
                    f"another process still holds this lock. Find it with: lsof {lock_path}\n"
                    f"a stale claude, harmonik daemon or go test binary is the usual holder.\n")
                sys.exit(%[5]d)
            time.sleep(lock_interval)

def write_cfg(cfg):
    d = os.path.dirname(cfg_path) or "."
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".claude.json.tmp-")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(cfg, f, indent=2)
            f.write("\n")
        os.replace(tmp, cfg_path)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise
`

// workerTrustUpsertProgram builds the python3 program (fed on STDIN to
// `python3 -`, NOT via -c — see EnsureWorktreeTrustVia for why) that
// idempotently upserts the worktree-trust entry in the worker's Claude config
// (~/.claude.json unless the precedence below names another file).
// It mirrors ensureWorktreeTrustAt's contract: realpath-normalize the key, set
// projects[key].hasTrustDialogAccepted = true, preserve all other content, write
// atomically, and skip the rewrite when already trusted.
//
// The config path and the lock budget come from workerConfigProgramPrelude —
// read its comment for which machine resolves which step of the config path, and
// why the wait is bounded. Both are baked into the program TEXT, which rides stdin, so the argv
// the worker sees (`python3 - <worktreePath>`) is the same as it ever was.
//
// # Cross-process lost-update safety (concurrent-slot race)
//
// Under max_slots>1 the daemon launches several remote runs at once and EACH
// spawns this program against the SAME worker ~/.claude.json. The naive
// read-modify-write below (read cfg, add only THIS run's worktree key,
// os.replace) is a classic lost-update race: two copies both read the config
// BEFORE either writes, each adds only its own key to its in-memory copy, and the
// last os.replace CLOBBERS the other's key. The clobbered run's worktree is then
// NOT trusted → Claude Code shows the folder-trust dialog → the launch hangs →
// agent_ready never fires → the run stalls (and --dangerously-skip-permissions
// does NOT suppress that trust dialog). A prior run PROVED this: 5 concurrent
// unlocked writers → only 1 worktree survived trusted.
//
// The fix mirrors the LOCAL Go writer's contract (see
// claudetrust_hkbfvby_test.go — sidecar lockfile, lock-free fast path, LOCK_EX
// write path): the read-modify-write is made atomic across processes with an
// fcntl.flock(LOCK_EX) held on a SIDECAR lockfile (~/.claude.json.lock) — NOT on
// ~/.claude.json itself, because os.replace() swaps the inode out from under any
// lock held on the config file, which is unsound. The exclusive lock is acquired
// BEFORE the read and held through os.replace(), so each writer sees the previous
// writer's committed keys and merges onto them; no update is lost.
//
// The already-trusted fast path stays cheap: it probes the config WITHOUT the
// lock and exits 0 when the key is already trusted (mirroring the local writer's
// mtime/quick-read fast path). Only a run that must WRITE takes the lock; and
// because a concurrent writer may have trusted this same key between the probe
// and the lock acquisition, the program RE-READS the config under the lock and
// re-checks the fast-path condition before writing.
func workerTrustUpsertProgram(cfgPathForWorker string, lockTimeout time.Duration) string {
	return workerConfigProgramPrelude(cfgPathForWorker, lockTimeout) + workerTrustUpsertProgramBody
}

const workerTrustUpsertProgramBody = `
arg = sys.argv[1]
if len(arg) >= 2 and arg[0] == "'" and arg[-1] == "'":
    arg = arg[1:-1]
wt = os.path.realpath(arg)

def is_trusted(cfg):
    projects = cfg.get("projects")
    if not isinstance(projects, dict):
        return False
    entry = projects.get(wt)
    return isinstance(entry, dict) and entry.get("hasTrustDialogAccepted") is True

# Fast path: probe WITHOUT the lock; a no-op when already trusted.
if is_trusted(load_cfg()):
    sys.exit(0)

# Write path: hold LOCK_EX on the sidecar lockfile across the whole
# read-modify-write so concurrent writers never lose each other's keys.
lock_fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
try:
    acquire_bounded(lock_fd)
    # Re-read UNDER the lock: another writer may have trusted this key (or added
    # other keys) between the lock-free probe and acquiring the lock.
    cfg = load_cfg()
    if is_trusted(cfg):
        sys.exit(0)
    projects = cfg.get("projects")
    if not isinstance(projects, dict):
        projects = {}
        cfg["projects"] = projects
    entry = projects.get(wt)
    if not isinstance(entry, dict):
        entry = {}
        projects[wt] = entry
    entry["hasTrustDialogAccepted"] = True
    write_cfg(cfg)
finally:
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_UN)
    except OSError:
        pass
    os.close(lock_fd)
`

// PrepareIsolatedClaudeConfigDirVia provisions a PRIVATE, per-launch Claude Code
// config directory for a REMOTE run ON THE WORKER and returns its worker-absolute
// path, ready to be exported to the spawned process as CLAUDE_CONFIG_DIR
// (hk-qxvc2). It is the SSH-aware sibling of PrepareIsolatedClaudeConfigDir: a nil
// runner delegates to the in-process (box-A-local) function, byte-identical to
// today (NFR7); a non-nil runner performs the SAME preparation on the worker.
//
// # WARNING — the LOCAL sibling was reverted (hk-8juwz)
//
// The claude:LOCAL isolation this mirrors is GONE: relocating CLAUDE_CONFIG_DIR
// moves the whole ~/.claude surface, and a local claude launched that way reported
// "Not logged in · Please run /login" (plus it lost ~/.claude/settings.json's
// skipDangerousModePermissionPrompt and parked on the bypass modal). The REMOTE
// path here is untouched but has NOT been live-tested for the same auth defect —
// if a worker's claude ever stalls at agent_ready or reports "Not logged in",
// suspect this first.
//
// # Why the remote path is needed
//
// Claude Code >= 2.1.214 renders a first-run onboarding/theme modal at Stage 1
// (BEFORE SessionStart) unless the config it reads records onboarding as complete.
// The LOCAL path once isolated a private config dir for this reason, but that was
// reverted (see the WARNING above) — a local claude now reads the operator's shared
// ~/.claude.json, and on claude v2.1.217 that does not reproduce the modal. On the
// REMOTE path the isolation was originally missing: the
// worker's claude read the worker's SHARED ~/.claude.json, whose modal-dismissing
// state can be perturbed by concurrent processes / theme/trust read-modify-writes,
// so claude wedged on the modal BEFORE the SessionStart hook fired → agent_ready
// never dialed back → deterministic agent_ready_timeout (the hk-qxvc2 stall;
// claude-only + remote-only). Isolating the config on the worker too closes it
// (and the fresh-worker case, hk-g5wkt).
//
// # Mechanism (mirrors EnsureWorktreeTrustVia)
//
// The preparation runs as a python3 program fed ON STDIN to `python3 - <worktree>`
// (NOT via -c — see EnsureWorktreeTrustVia for the SSH argv-resplit hazard), with
// the worktree path as the single argv token. On the worker the program:
//
//  1. mkdir -p <worktree>/.harmonik/claude-config (0o700).
//  2. Seeds <dir>/.claude.json by COPYING the WORKER's OWN ~/.claude.json (the
//     worker's onboarded config is the correct modal-dismisser for a process
//     running ON the worker — box A's config is irrelevant there). If that source
//     is missing/unreadable/corrupt, it falls back to a minimal onboarding-complete
//     config (firstStartTime only) — same best-effort fallback as the local path.
//  3. Upserts the worktree-trust entry
//     (projects[<realpath(worktree)>].hasTrustDialogAccepted = true) INTO the
//     isolated config, realpath-normalizing the key ON THE WORKER so it matches
//     claude's own realpath() of its cwd once the config is relocated.
//  4. Writes atomically (temp file + os.replace, 0o600).
//
// Unlike the shared ~/.claude.json writers this needs NO cross-process flock: the
// isolated dir is private to ONE worktree, so no other launch races it.
//
// The returned worker-absolute path is computed in Go from the worker-absolute
// workspacePath (filepath.Join, same as the local variant and the other *Via
// path builders) — the program does not echo it back. On the seed program's
// failure the error is propagated so the caller does NOT exec claude (an
// un-isolated launch re-wedges on the modal), mirroring the local fatal posture.
func PrepareIsolatedClaudeConfigDirVia(ctx context.Context, runner tmux.CommandRunner, workspacePath string) (string, error) {
	if runner == nil {
		return PrepareIsolatedClaudeConfigDir(workspacePath)
	}

	cmd := runner.Command(ctx, "python3", "-", workspacePath)
	cmd.Stdin = bytes.NewReader([]byte(workerIsolatedConfigProgram))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDirVia %s: %w\nremote: %s", workspacePath, err, out)
	}
	// The worker-absolute path of the isolated dir mirrors the local layout
	// (<worktree>/.harmonik/claude-config); workspacePath is already the
	// worker-absolute worktree path for a remote run, so filepath.Join yields the
	// worker path CLAUDE_CONFIG_DIR must carry (same idiom as ClaudeSettingsPath).
	return filepath.Join(workspacePath, ".harmonik", isolatedClaudeConfigDirName), nil
}

// workerIsolatedConfigProgram is the python3 program (fed on STDIN to `python3 -`,
// NOT via -c — see EnsureWorktreeTrustVia for why) that provisions the isolated
// per-launch Claude config dir ON THE WORKER: mkdir the dir under the worktree,
// seed <dir>/.claude.json from the WORKER's own ~/.claude.json (or a minimal
// onboarding-complete fallback), and upsert the realpath-normalized worktree-trust
// entry — mirroring PrepareIsolatedClaudeConfigDir + ensureWorktreeTrustAt. The
// fallback firstStartTime literal MUST stay in sync with fallbackFirstStartTime in
// claudeconfigdir_hk8juwz.go (injected here so there is a single source of truth).
//
// No flock is taken: the isolated dir is private to ONE worktree (unlike the
// shared ~/.claude.json that workerTrustUpsertProgram must lock), so there is no
// concurrent writer to lose-update against. The dest is written atomically via a
// temp file + os.replace so a reader never sees a half-written config.
var workerIsolatedConfigProgram = fmt.Sprintf(`
import json, os, sys, tempfile
arg = sys.argv[1]
if len(arg) >= 2 and arg[0] == "'" and arg[-1] == "'":
    arg = arg[1:-1]
config_dir = os.path.join(arg, ".harmonik", "claude-config")
os.makedirs(config_dir, mode=0o700, exist_ok=True)
try:
    os.chmod(config_dir, 0o700)
except OSError:
    pass
dest = os.path.join(config_dir, ".claude.json")
src = os.path.join(os.path.expanduser("~"), ".claude.json")

def load_src():
    try:
        with open(src) as f:
            cfg = json.load(f)
    except (FileNotFoundError, ValueError, OSError):
        return None
    return cfg if isinstance(cfg, dict) else None

cfg = load_src()
if cfg is None:
    cfg = {"firstStartTime": %q}

wt = os.path.realpath(arg)
projects = cfg.get("projects")
if not isinstance(projects, dict):
    projects = {}
    cfg["projects"] = projects
entry = projects.get(wt)
if not isinstance(entry, dict):
    entry = {}
    projects[wt] = entry
entry["hasTrustDialogAccepted"] = True

d = os.path.dirname(dest) or "."
fd, tmp = tempfile.mkstemp(dir=d, prefix=".claude.json.tmp-")
try:
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(cfg, f, indent=2)
        f.write("\n")
    os.replace(tmp, dest)
except BaseException:
    try:
        os.unlink(tmp)
    except OSError:
        pass
    raise
`, fallbackFirstStartTime)
