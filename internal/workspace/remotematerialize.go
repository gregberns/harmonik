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
const workerTrustUpsertProgram = `
import json, os, sys, tempfile
arg = sys.argv[1]
if len(arg) >= 2 and arg[0] == "'" and arg[-1] == "'":
    arg = arg[1:-1]
wt = os.path.realpath(arg)
cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
cfg = {}
try:
    with open(cfg_path) as f:
        cfg = json.load(f)
except FileNotFoundError:
    cfg = {}
if not isinstance(cfg, dict):
    cfg = {}
projects = cfg.get("projects")
if not isinstance(projects, dict):
    projects = {}
    cfg["projects"] = projects
entry = projects.get(wt)
if not isinstance(entry, dict):
    entry = {}
    projects[wt] = entry
if entry.get("hasTrustDialogAccepted") is True:
    sys.exit(0)
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
`
