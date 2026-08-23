package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/sessioncapture"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

const substrateSelectEnv = "HARMONIK_SUBSTRATE"

func tmuxSubstrateSelected() bool {
	return os.Getenv(substrateSelectEnv) != "codexdriver"
}

const (
	captureDirEnv  = "HARMONIK_CAPTURE_DIR"
	captureKeepEnv = "HARMONIK_CAPTURE_KEEP"    // retention keep-N (optional; int)
	captureAgeEnv  = "HARMONIK_CAPTURE_MAX_AGE" // age-prune bound (optional; Go duration)
)

func selectSubstrate(tmuxSub handler.Substrate, codexBinary string) (sub handler.Substrate, bindRegistry func(*workers.Registry), reviewerSubstrate handler.Substrate) {
	if tmuxSubstrateSelected() {
		return tmuxSub, nil, tmuxSub
	}
	router := &codexWorkerRoutingRunner{requireBoundary: false}
	opts, _ := codexSubstrateOptions(codexBinary, router)
	return codexdriver.NewCodexSubstrate(opts), router.setRegistry, tmuxSub
}

type codexWorkerRoutingRunner struct {
	// reg is the live worker registry, late-bound by the daemon. nil until
	// bound, and stays nil when no worker is configured. That used to mean
	// LOCAL codex, byte-identical to the pre-M4 hardcoded LocalRunner path
	// (NFR7) — it no longer does when requireBoundary is set; see below.
	reg atomic.Pointer[workers.Registry]

	// requireBoundary would make this runner FAIL CLOSED (hk-5h759): when set and
	// no enabled ssh worker is bound, Command REFUSES rather than falling through
	// to LocalRunner.
	//
	// hk-5vapm intended this to be inert: it called this field "the authoritative,
	// race-free enforcement point", which it is not — there is no daemon-side
	// counterpart, and an auditor reading the old wording would have concluded that
	// unsandboxed codex launches are refused somewhere they are not.
	//
	// hk-tckw3.1 Step 3a dropped the fence deliberately: D4 scrapped the ssh worker
	// that was the only thing able to supply the boundary, so arming this would
	// stop codex launching rather than isolate it. Codex containment comes from the
	// srt sandbox (hk-scaj0) instead.
	//
	// selectSubstrate above passes `false` per the 2026-07-23 operator decision, so
	// the composition-root runner never refuses. The refusal logic below is retained
	// only for the explicit ssh-worker path (and its tests), which construct their
	// own runner with requireBoundary: true.
	requireBoundary bool
}

const refusedIsolationBoundaryArgv0 = "/nonexistent/harmonik-REFUSED-codex-danger-full-access-requires-enabled-ssh-isolation-boundary-hk5h759"

const (
	codexHeadlessSandbox        = "danger-full-access"
	codexHeadlessApprovalPolicy = "never"
)

func (r *codexWorkerRoutingRunner) setRegistry(reg *workers.Registry) {
	r.reg.Store(reg)
}

// Command selects the per-run spawn transport. When a worker is bound, enabled
// (health-gated + live-disable via the shared registry), and reachable over
// ssh, the codex process is spawned on that worker via SSHRunner{Host}. Any
// other state (no registry bound, no worker, disabled/unhealthy worker,
// non-ssh transport) falls through to LocalRunner — byte-identical local codex
// (NFR7). The composition root passes requireBoundary: false (2026-07-23 operator
// decision), so the codexdriver path takes this local fallthrough. requireBoundary
// is retained only for the explicit ssh-worker path and its tests: when set, the
// same states are refused instead, by returning a command at
// refusedIsolationBoundaryArgv0 so the spawn fails closed rather than running
// codex unsandboxed on the daemon host.
//
// Slot capacity accounting stays owned by the daemon's dispatch gate
// (workloop SelectWorker/ReleaseSlot, which runs for every dispatched run);
// this runner mirrors only the host decision via the non-reserving
// WorkerSnapshot peek.
func (r *codexWorkerRoutingRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if reg := r.reg.Load(); reg != nil {
		if w := reg.WorkerSnapshot(); w != nil && w.Enabled && w.Transport == "ssh" {
			return ltmux.SSHRunner{
				Host: w.Host,
				Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
			}.Command(ctx, name, args...)
		}
	}
	if r.requireBoundary {
		return exec.CommandContext(ctx, refusedIsolationBoundaryArgv0)
	}
	return ltmux.LocalRunner{}.Command(ctx, name, args...)
}

// CommandInDir is the RemoteCwdRunner (hk-czb11) analog of Command: it applies
// the spawn working directory correctly for the routed transport. Without it the
// codex driver's RemoteCwdRunner type-assert would fail against this router (the
// composition-root runner wired into codexdriver.Options.Runner) and fall back to
// setting the LOCAL exec.Cmd.Dir — which for an ssh-routed run is the REMOTE
// worktree path, fork/exec-ENOENTing the local ssh process.
//
//   - ssh worker bound: delegate to SSHRunner.CommandInDir — the cwd is applied
//     ON THE WORKER (cd … && exec …) and the local exec.Cmd.Dir is left unset.
//   - fail-closed (requireBoundary, no ssh route): return the refusal argv0
//     exactly as Command does; dir is irrelevant (exec.Start fails immediately).
//   - LOCAL fallback: LocalRunner runs box-A-locally, so dir is a LOCAL path —
//     set it as exec.Cmd.Dir here, because the driver's spawn leaves Dir unset on
//     the RemoteCwdRunner branch (this method owns applying it for local runs).
//
// Mirrors Command's routing decision exactly (same WorkerSnapshot peek, same
// per-run non-multiplexed SSHRunner opts). Refs: hk-czb11.
func (r *codexWorkerRoutingRunner) CommandInDir(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	if reg := r.reg.Load(); reg != nil {
		if w := reg.WorkerSnapshot(); w != nil && w.Enabled && w.Transport == "ssh" {
			return ltmux.SSHRunner{
				Host: w.Host,
				Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
			}.CommandInDir(ctx, dir, name, args...)
		}
	}
	if r.requireBoundary {
		return exec.CommandContext(ctx, refusedIsolationBoundaryArgv0)
	}
	cmd := ltmux.LocalRunner{}.Command(ctx, name, args...)
	cmd.Dir = dir
	return cmd
}

func codexSubstrateOptions(codexBinary string, runner codexdriver.CommandRunner) (codexdriver.Options, *sessioncapture.Session) {
	if codexBinary == "" {
		codexBinary = "codex"
	}
	opts := codexdriver.Options{
		Binary: codexBinary,
		// hk-daegv: force the sandbox posture at app-server LAUNCH via a codex
		// config override — NOT only per-thread. codex app-server (0.142/0.144) does
		// not honor the thread/start `sandbox` field for the exec seatbelt; it runs
		// its config default (workspace-write). Under workspace-write the worktree's
		// real git dir (<repo>/.git/worktrees/<id>/ — a PARENT of the worktree
		// writable-root) is denied, so codex's own `git commit` fails ("Operation
		// not permitted") AND its /bin/zsh exec_command spawn fails (hk-wwyse, same
		// seatbelt) — the turn silently no-ops and only the daemon fallback commits.
		// `-c sandbox_mode="<posture>"` overrides ~/.codex/config.toml and applies to
		// the exec seatbelt. Safe ONLY inside the isolation boundary the fail-closed
		// guard enforces (danger-full-access = no seatbelt), set here at the
		// composition root alongside Sandbox/requireBoundary. One override restores
		// BOTH facets: .git-writable commit and working shell-spawn.
		Args:   []string{"app-server", "-c", `sandbox_mode="` + codexHeadlessSandbox + `"`},
		Runner: runner, // M4-C3: per-run worker-routing runner (SSHRunner remote / LocalRunner local)
		Clock:  substrate.SystemClock{},
		// hk-5h759: headless crew-orchestration posture. The driver auto-declines
		// approval requests (no approval negotiation), so under codex's default
		// policy exec/apply-patch prompts are declined and the crew's writes and
		// commits never land. danger-full-access + never make codex run
		// non-interactively so its work lands. This posture is SAFE ONLY inside
		// the isolation boundary the fail-closed guard enforces — set here at the
		// composition root alongside requireBoundary (selectSubstrate), never
		// baked into the driver: a driver built without this leaves codex's
		// default posture, so it can never silently run danger-full-access.
		Sandbox:        codexHeadlessSandbox,
		ApprovalPolicy: codexHeadlessApprovalPolicy,
		// hk-daegv: codex app-server 0.142.0 under ChatGPT auth does NOT honor the
		// danger-full-access posture above — it runs the effective workspace-write
		// seatbelt whose only writable root is the worktree cwd. A linked worktree's
		// git common dir (<repo>/.git) lives OUTSIDE that root, so codex's OWN
		// `git commit` fails EPERM and only the daemon fallback commits. Wire the
		// composition-root hook that adds the git common dir to the thread's
		// runtimeWorkspaceRoots so codex's own commit lands. Kept ALONGSIDE the
		// `-c sandbox_mode` override and Sandbox/ApprovalPolicy (harmless
		// forward-intent for a codex build that does honor danger-full-access).
		WritableRoots: codexWorktreeWritableRoots,
	}
	sess := openCaptureSession()
	if sess != nil {
		opts.InCapture = sess.Input()
		opts.OutCapture = sess.Output()
	}
	return opts, sess
}

func codexWorktreeWritableRoots(worktreeCwd string) []string {
	if worktreeCwd == "" {
		return nil
	}
	roots := []string{worktreeCwd}
	if gitCommon := codexGitCommonDir(worktreeCwd); gitCommon != "" {
		roots = append(roots, gitCommon)
	}
	return roots
}

func codexGitCommonDir(worktreeCwd string) string {
	marker := "/" + workspace.DefaultWorktreeRoot + "/" // "/.harmonik/worktrees/"
	idx := strings.LastIndex(worktreeCwd, marker)
	if idx < 0 {
		return ""
	}
	return worktreeCwd[:idx] + "/.git"
}

func openCaptureSession() *sessioncapture.Session {
	dir := os.Getenv(captureDirEnv)
	if dir == "" {
		return nil // opt-in; capture off by default (design §2, AIS-014)
	}
	cfg := sessioncapture.Config{
		WorkspacePath: dir,
		// One corpus dir per composition-root substrate; the session id is
		// monotone-by-open-time so retention (keep-N by mtime) prunes oldest.
		SessionID: "codexdriver-" + time.Now().UTC().Format("20060102T150405.000000000"),
	}
	if n, err := strconv.Atoi(os.Getenv(captureKeepEnv)); err == nil && n > 0 {
		cfg.KeepN = n
	}
	if d, err := time.ParseDuration(os.Getenv(captureAgeEnv)); err == nil && d > 0 {
		cfg.MaxAge = d
	}
	sess, err := sessioncapture.Open(context.Background(), cfg)
	if err != nil {
		log.Printf("harmonik: live session capture disabled (open failed): %v", err)
		return nil
	}
	return sess
}
