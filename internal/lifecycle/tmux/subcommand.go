package tmux

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ExecFunc is the type of the function used to exec-replace the current
// process. In production this is syscall.Exec; in tests it is replaced with a
// no-op that records the call so the test can assert without actually replacing
// the test process.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement — step iv:
// "execve `tmux attach-session -t <session-name>`, replacing the process."
type ExecFunc func(argv0 string, argv []string, envv []string) error

// RunTmuxStart implements the `hk tmux-start` subcommand.
//
// It reads $TMUX from env, computes a tmux session name from projectDir,
// ensures the session exists, and exec-replaces the current process with
// `tmux attach-session -t <name>`.
//
// Parameters:
//   - projectDir: the harmonik project root (used to derive the session name
//     via SHA-256(realpath(projectDir)) per PL-006a). When empty, sessionName
//     override MUST be provided.
//   - sessionName: optional override for the session name. MUST carry the
//     `harmonik-<project_hash>-` prefix when projectDir is also set; if not,
//     RunTmuxStart returns exit code 24. When projectDir is empty any non-empty
//     sessionName is accepted as-is.
//   - stdout / stderr: output writers for operator-facing messages.
//   - execFn: the exec function (SyscallExec in production; SkipExecRecorder in tests).
//   - env: environment slice ([]string{"KEY=VAL"...}); nil means os.Environ().
//
// Return value is the exit code the caller MUST use when calling os.Exit.
//
// Exit codes per PL-028 refinement §5:
//   - 0  — $TMUX already set; operator is already inside tmux.
//   - 22 — tmux probe failed (binary missing or version < 3.0).
//   - 24 — any other unrecoverable failure.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement.
func RunTmuxStart(
	projectDir string,
	sessionName string,
	stdout io.Writer,
	stderr io.Writer,
	execFn ExecFunc,
	env []string,
) int {
	if env == nil {
		env = os.Environ()
	}

	if tmuxEnv := tmuxEnvLookup(env, "TMUX"); tmuxEnv != "" {
		if _, err := fmt.Fprintf(stdout, "hk tmux-start: already inside a tmux session (%s); nothing to do.\n", tmuxEnv); err != nil {
			return 24
		}
		return 0
	}

	var computedName string
	if sessionName != "" {
		if projectDir != "" {
			hash := tmuxStartHashDir(projectDir)
			prefix := "harmonik-" + hash + "-"
			if !strings.HasPrefix(sessionName, prefix) {
				if _, err := fmt.Fprintf(stderr,
					"hk tmux-start: --session-name %q does not carry required prefix %q\n",
					sessionName, prefix); err != nil {
					return 24
				}
				return 24
			}
		}
		computedName = sessionName
	} else {
		if projectDir == "" {
			if _, err := fmt.Fprintln(stderr, "hk tmux-start: project directory is required when --session-name is not provided"); err != nil {
				return 24
			}
			return 24
		}
		hash := tmuxStartHashDir(projectDir)
		computedName = "harmonik-" + hash + "-default"
	}

	tmuxBin, err := tmuxStartLookupBin(env)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "hk tmux-start: cannot locate tmux binary: %v\n", err); writeErr != nil {
			return 22
		}
		return 22
	}

	ctx := context.Background()
	adapter := OSAdapter{}.WithRunner(tmuxBinRunner{bin: tmuxBin})
	if err := adapter.ProbeTmux(ctx); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "hk tmux-start: tmux probe failed: %v\n", err); writeErr != nil {
			return 22
		}
		return 22
	}

	if err := adapter.EnsureSession(ctx, computedName, projectDir); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "hk tmux-start: failed to ensure tmux session %q: %v\n", computedName, err); writeErr != nil {
			return 24
		}
		return 24
	}

	argv := []string{"tmux", "attach-session", "-t", computedName}
	if execErr := execFn(tmuxBin, argv, env); execErr != nil {
		if !errors.Is(execErr, errTmuxStartExecSkipped) {
			if _, writeErr := fmt.Fprintf(stderr, "hk tmux-start: exec tmux attach-session: %v\n", execErr); writeErr != nil {
				return 24
			}
			return 24
		}
	}
	return 0
}

// SyscallExec is the production ExecFunc. It wraps syscall.Exec directly; on
// success the process image is replaced and this function never returns.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement step iv —
// "execve `tmux attach-session -t <session-name>`, replacing the process."
func SyscallExec(argv0 string, argv, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}

var errTmuxStartExecSkipped = errors.New("exec skipped (test stub)")

// SkipExecRecorder returns a test-stub ExecFunc that records the exec call
// arguments into *out and returns errTmuxStartExecSkipped so the test process
// is not replaced. RunTmuxStart treats errTmuxStartExecSkipped as a successful
// exec, so the returned exit code is 0.
//
// Usage in tests:
//
//	var recorded []string
//	code := tmux.RunTmuxStart(dir, "", os.Stdout, os.Stderr, tmux.SkipExecRecorder(&recorded), env)
func SkipExecRecorder(out *[]string) ExecFunc {
	return func(argv0 string, argv []string, _ []string) error {
		*out = append([]string{argv0}, argv...)
		return errTmuxStartExecSkipped
	}
}

// DefaultSessionName returns the canonical per-project daemon tmux session name
// for projectDir: "harmonik-<project_hash>-default" (PL-006a). This is the same
// name `hk tmux-start` creates by default, so a daemon that EnsureSessions this
// name attaches to the operator's session when launched via tmux-start, and
// creates its own dedicated session otherwise.
//
// hk-9vp51 / hk-15b83: callers MUST NOT spawn implementer windows into the
// ambient $TMUX session when that session is the supervisor's — see
// ResolveDaemonSpawnSession for the dispatch-time resolution that excludes it.
func DefaultSessionName(projectDir string) string {
	return "harmonik-" + tmuxStartHashDir(projectDir) + "-default"
}

// SupervisorSessionName returns the tmux session name for the auto-revive
// supervisor for the given project directory. Format: hk-<project_hash>-supervise
// (shortened from the legacy hk-<hash>-daemon-supervise form). The supervisor
// DELIBERATELY keeps the hk- prefix so it stays OUTSIDE the harmonik-<hash>-*
// namespace that the daemon's boot orphan-sweep prefix-kills (orphansweep.go,
// PL-019) — moving it into harmonik-<hash>-* would require a sweep-exclusion
// entry and widen the blast radius. Per the tmux-session-organization CONTRACT.
//
// The daemon, launched as a child of the supervisor, can inherit $TMUX pointing
// at this session; implementer windows MUST NOT spawn here (they would leak into
// the supervisor's session and a `grep harmonik-*-flywheel` would find "0
// sessions" — the symptom that mis-diagnosed hk-9vp51 as a launch wedge).
//
// hk-9vp51 fix-forward: ResolveDaemonSpawnSession excludes this session (and
// the flywheel session — see FlywheelSessionName) as spawn targets; every other
// ambient session is a valid, already-existing spawn target.
func SupervisorSessionName(projectDir string) string {
	return "hk-" + tmuxStartHashDir(projectDir) + "-supervise"
}

// FlywheelSessionName returns the tmux session name for the flywheel (shim)
// pane that the new `harmonik supervise start` supervisor creates.
// Format: harmonik-<project_hash>-flywheel (PL-019f, PL-006a).
//
// hk-u9ji: on supervisor-revive via DaemonWatchdog, the daemon is spawned
// with Setsid:true but inherits $TMUX from the flywheel pane's session. The
// revived daemon therefore sees liveSession="harmonik-<hash>-flywheel" and —
// before this fix — used the flywheel session as the spawn target (needEnsure
// false, no EnsureSession call), leaving harmonik-<hash>-default uncreated.
// Excluding the flywheel session forces the fallback to DefaultSessionName with
// needEnsure=true, causing EnsureSession to (re)create -default on every start
// including supervisor-revive.
func FlywheelSessionName(projectDir string) string {
	return "harmonik-" + tmuxStartHashDir(projectDir) + "-flywheel"
}

// ResolveDaemonSpawnSession decides which tmux session the daemon should spawn
// implementer windows into, given the live session it currently runs inside
// (liveSession, as returned by `tmux display-message -p '#{session_name}'`).
//
// hk-9vp51 fix-forward (option (a) — LOW RISK): keep dispatch-time live-session
// resolution — which ALWAYS resolves to a session that exists right now — and
// EXCLUDE system sessions that must not receive implementer windows. Concretely:
//
//   - liveSession is a normal session (operator's tmux-start session, an ambient
//     `harmonik` session, etc.) → use it verbatim. It provably exists at this
//     instant (the daemon is running inside it), so SpawnWindow can never hit
//     "session does not exist". needEnsure is false.
//   - liveSession is empty (display-message failed), the old per-project
//     supervisor session (SupervisorSessionName), OR the flywheel shim session
//     (FlywheelSessionName) → fall back to the deterministic per-project
//     DefaultSessionName and signal needEnsure=true so the caller EnsureSessions
//     it before constructing the substrate.
//
// The flywheel case (hk-u9ji): on supervisor-revive via DaemonWatchdog,
// Setsid:true detaches the daemon from the shim's process group but $TMUX is
// still inherited. The revived daemon's tmux display-message therefore returns
// the flywheel session name. Without excluding it the daemon would use the
// flywheel as its spawn target (needEnsure=false, no EnsureSession call),
// leaving harmonik-<hash>-default uncreated and causing a spawn outage.
//
// This is the only case where we depart from the live session. The fallback is
// ensured-and-kept-alive by the caller (a detached tmux session with a live
// shell persists; the #4 coordinator reaper only targets "-flywheel" sessions,
// never this "-default" one).
//
// Returns the chosen session name and whether the caller must EnsureSession it
// (true only for the fallback). The returned name is never a system session and
// is never empty.
func ResolveDaemonSpawnSession(projectDir, liveSession string) (session string, needEnsure bool) {
	live := strings.TrimSpace(liveSession)
	if live == "" || live == SupervisorSessionName(projectDir) || live == FlywheelSessionName(projectDir) {
		return DefaultSessionName(projectDir), true
	}
	return live, false
}

func tmuxStartHashDir(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	sum := sha256.Sum256([]byte(resolved))
	return fmt.Sprintf("%x", sum[:6]) // 6 bytes → 12 lowercase hex chars
}

func tmuxEnvLookup(env []string, name string) string {
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

type tmuxBinRunner struct {
	bin string
}

// Command returns exec.CommandContext with "tmux" swapped for the pinned binary.
func (r tmuxBinRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == "tmux" {
		name = r.bin
	}
	return exec.CommandContext(ctx, name, args...)
}

func tmuxStartLookupBin(env []string) (string, error) {
	pathEnv := tmuxEnvLookup(env, "PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, "tmux")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("tmux not found in PATH=%q", pathEnv)
}
