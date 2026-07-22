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
// existing remote-command idiom in internal/daemon (ensureWorkerHarmonikDir,
// fetchBaseOnWorker) which all run `runner.Command(...).CombinedOutput()`.
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
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
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
// The upsert is performed by a tiny Python one-liner (python3 is present on the
// macOS worker — verified by probe). Python's json module is stdlib, so no extra
// install is needed; the worktree path is passed via argv (not interpolated into
// the script) so it needs no escaping beyond the single-quote wrap of the script
// body itself.
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
	cmd.Stdin = bytes.NewReader([]byte(workerTrustUpsertProgram))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrustVia %s: %w\nremote: %s", worktreePath, err, out)
	}
	return nil
}

// workerTrustUpsertProgram is the python3 program (fed on STDIN to `python3 -`,
// NOT via -c — see EnsureWorktreeTrustVia for why) that idempotently upserts the
// worktree-trust entry in the worker's ~/.claude.json. It mirrors
// ensureWorktreeTrustAt's contract: realpath-normalize the key, set
// projects[key].hasTrustDialogAccepted = true, preserve all other content, write
// atomically, and skip the rewrite when already trusted.
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
const workerTrustUpsertProgram = `
import fcntl, json, os, sys, tempfile
arg = sys.argv[1]
if len(arg) >= 2 and arg[0] == "'" and arg[-1] == "'":
    arg = arg[1:-1]
wt = os.path.realpath(arg)
cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
lock_path = cfg_path + ".lock"

def load_cfg():
    try:
        with open(cfg_path) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        return {}
    except ValueError:
        return {}
    return cfg if isinstance(cfg, dict) else {}

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
    fcntl.flock(lock_fd, fcntl.LOCK_EX)
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

// EnsureClaudeThemeVia pre-seeds the top-level "theme" key in the config where
// Claude Code will read it (the WORKER's ~/.claude.json for a remote run; box-A's
// for a local run), suppressing the first-run theme-selection modal (hk-oga33).
// It is the theme-modal analogue of EnsureWorktreeTrustVia, and mirrors its
// transport: local (runner == nil) delegates to the in-process EnsureClaudeTheme;
// remote runs the theme upsert as a python3 program fed ON STDIN (never via -c —
// see EnsureWorktreeTrustVia for the SSH argv-resplit hazard). Theme is a GLOBAL
// key (no worktree argument), so the program takes no argv and is a lock-free
// no-op once any launch has seeded it.
func EnsureClaudeThemeVia(ctx context.Context, runner tmux.CommandRunner) error {
	if runner == nil {
		return EnsureClaudeTheme()
	}
	cmd := runner.Command(ctx, "python3", "-")
	cmd.Stdin = bytes.NewReader([]byte(workerThemeUpsertProgram))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: EnsureClaudeThemeVia: %w\nremote: %s", err, out)
	}
	return nil
}

// workerThemeUpsertProgram is the python3 program (fed on STDIN to `python3 -`,
// NOT via -c) that idempotently seeds ~/.claude.json["theme"] = "dark" when it is
// absent/null/empty on the worker. It mirrors workerTrustUpsertProgram's
// concurrency contract — lock-free fast path when already set; a bounded
// LOCK_EX sidecar-flock read-modify-write only when a mutation is needed; atomic
// temp-file + os.replace; preserve all other keys; never clobber an operator's
// explicit theme. The "dark" literal MUST stay in sync with claudeDefaultTheme in
// claudetrust_wm040b.go.
const workerThemeUpsertProgram = `
import fcntl, json, os, sys, tempfile
cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
lock_path = cfg_path + ".lock"

def load_cfg():
    try:
        with open(cfg_path) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        return {}
    except ValueError:
        return {}
    return cfg if isinstance(cfg, dict) else {}

def theme_set(cfg):
    t = cfg.get("theme")
    return isinstance(t, str) and t != ""

# Fast path: probe WITHOUT the lock; a no-op when the theme is already set.
if theme_set(load_cfg()):
    sys.exit(0)

# Write path: hold LOCK_EX on the sidecar lockfile across the read-modify-write so
# concurrent writers (incl. the trust upsert) never lose each other's keys.
lock_fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
try:
    fcntl.flock(lock_fd, fcntl.LOCK_EX)
    cfg = load_cfg()
    if theme_set(cfg):
        sys.exit(0)
    cfg["theme"] = "dark"
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
finally:
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_UN)
    except OSError:
        pass
    os.close(lock_fd)
`
