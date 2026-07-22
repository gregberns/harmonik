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

// substrateSelectEnv is the composition-root substrate-selection axis
// (AIS-015): tmux hosting by default; the structured Codex app-server driver
// (internal/codexdriver) by explicit opt-in only. Selection is by which value
// is WIRED into daemon.Config.Substrate here at the root — never a runtime
// test-branch inside a driver (RS-017), and the driver itself is blind to this
// axis (twin-blindness: L2/L3 doubles substitute at the wire).
//
// Value "codexdriver" selects the structured driver. Anything else (including
// unset) keeps the tmux substrate — the safe pre-bake default.
const substrateSelectEnv = "HARMONIK_SUBSTRATE"

// Live-capture selection (AIS-013/AIS-014, m2-4-capture-tee design §2). Capture
// is OPT-IN and OFF by default: it engages only when HARMONIK_CAPTURE_DIR names
// a workspace root under which the corpus lands at
// ${dir}/.harmonik/sessions/${session_id}/ (WM §4.7). It applies only to the
// structured Codex driver — the tmux/Claude path has no raw child stdio to tee
// (design §0, AIS-011). AIS-INV-002: capture is NEVER load-bearing, so an open
// failure degrades to uncaptured and never blocks substrate selection.
const (
	captureDirEnv  = "HARMONIK_CAPTURE_DIR"
	captureKeepEnv = "HARMONIK_CAPTURE_KEEP"    // retention keep-N (optional; int)
	captureAgeEnv  = "HARMONIK_CAPTURE_MAX_AGE" // age-prune bound (optional; Go duration)
)

// selectSubstrate applies the AIS-015 selection axis: it returns tmuxSub
// unless HARMONIK_SUBSTRATE=codexdriver explicitly opts in to the structured
// Codex driver. codexBinary is the codex executable (--codex-binary flag /
// default) used when a LaunchSpec supplies no argv.
//
// The spawn seam stays remote-capable (AIS-016): the driver takes the same
// CommandRunner shape as the tmux path. For the Codex path the injected runner
// is a per-run worker-routing runner (M4-C3): a healthy selected worker routes
// the codex process to that worker over SSHRunner; zero/disabled workers stay
// byte-identical LOCAL (NFR7). See codexWorkerRoutingRunner.
//
// The second return value is a worker-registry observer the daemon MUST invoke
// once at work-loop startup with the SAME live registry the tmux dispatch path
// reads (daemon.Config.WorkerRegistryObserver). It late-binds that registry
// into the Codex runner so selection is per-run and shares the tmux path's
// health/live-disable state — WITHOUT the driver ever learning about workers
// (RS-017 twin-blindness: selection lives at the composition root, not the
// driver). It is nil for the tmux path (nothing to bind).
// reviewerSubstrate is always tmuxSub so a claude (SessionIDMinted) reviewer
// runs on tmux/claude, not the codex app-server driver (hk-qxvc2).
//
// hk-5vapm: this used to return a third value, requireIsolationBoundary, meant
// as the signal a daemon-side fail-closed guard would key off to refuse a codex
// run with no ssh worker bound. IT IS GONE, and two things about it are worth
// recording because the comments that described it outlived the design.
//
// First, hk-tckw3.1 Step 1 dropped the fence deliberately (plan section 3a). D4
// scrapped ssh-per-node, so nothing can supply the boundary the fence demanded
// — leaving it armed would not sandbox codex, it would only stop codex running
// at all. D3 then put local codex on danger-full-access, the same host posture
// claude already ran under, so this path is no more permissive than the default
// it was singled out from. Both production callers had already been discarding
// the value; it was always false and unparam flagged it.
//
// Second, what the surviving comments say about enforcement. Nothing refuses an
// unsandboxed codex launch today: the daemon-side guard is gone and
// codexWorkerRoutingRunner.requireBoundary below, though its refusal logic is
// live code, is never set true. An auditor reading the old comments here and in
// internal/codexdriver would have concluded an enforcement point still existed,
// so they are corrected rather than carried forward.
//
// The history behind that, stated correctly because an earlier version of this
// comment got it wrong and called the daemon half a phantom: the fence was
// FULLY BUILT and then FULLY REMOVED, on purpose. hk-5h759 (c2633a95) shipped
// both halves — `codexWorkerRoutingRunner{requireBoundary: true}` here at the
// composition root AND a `deps.codexRequireIsolationBoundary` refusal in
// internal/daemon/workloop.go with Config plumbing behind it. hk-tckw3.1 Step 1
// (d59d5d32) deleted both, per plan section 3a. So "no such symbol in the tree"
// describes today's tree, not the design: it is the residue of a deliberate,
// operator-directed removal, and reading it as "hk-5h759 never enforced
// anything" is wrong in the opposite direction.
//
// Containment for codex comes from harmonik's own srt sandbox (hk-scaj0), a
// different mechanism entirely, so removing this dead signal forecloses nothing.
func selectSubstrate(tmuxSub handler.Substrate, codexBinary string) (sub handler.Substrate, bindRegistry func(*workers.Registry), reviewerSubstrate handler.Substrate) {
	if os.Getenv(substrateSelectEnv) != "codexdriver" {
		return tmuxSub, nil, tmuxSub
	}
	router := &codexWorkerRoutingRunner{requireBoundary: false}
	opts, _ := codexSubstrateOptions(codexBinary, router)
	return codexdriver.NewCodexSubstrate(opts), router.setRegistry, tmuxSub
}

// codexWorkerRoutingRunner is the composition-root CommandRunner (M4-C3) that
// makes the Codex driver's spawn seam worker-selectable PER-RUN. It satisfies
// codexdriver.CommandRunner structurally and is injected as Options.Runner.
//
// Mechanism — late-binding hook (NOT boot-time construction): the Codex
// substrate is built ONCE at daemon boot (selectSubstrate), whereas the tmux
// path picks SSHRunner{Host} PER-RUN from the live worker registry
// (workloop.go SelectWorker + SSHRunner{Host}). A construction-time runner pick
// would freeze the Codex substrate to a single host for the daemon's whole
// lifetime and could never react to live worker enable/disable (FR12) or the
// boot health probe. So the routing decision is deferred to Command() time,
// exactly like the tmux path re-selects every run. The registry pointer is
// late-bound (setRegistry) by the daemon AFTER it builds the live registry, so
// Codex reads the SAME registry the tmux path reads — no second registry, no
// duplicated health check.
//
// RS-017: the driver stays BLIND — it only ever calls Runner.Command(); all
// worker logic lives here at the wire/root, never inside internal/codexdriver.
type codexWorkerRoutingRunner struct {
	// reg is the live worker registry, late-bound by the daemon. nil until
	// bound (and stays nil when no worker is configured) ⇒ LOCAL codex,
	// byte-identical to the pre-M4 hardcoded LocalRunner path (NFR7).
	reg atomic.Pointer[workers.Registry]

	// requireBoundary would make this runner FAIL CLOSED (hk-5h759): when set and
	// no enabled ssh worker is bound, Command REFUSES rather than falling through
	// to LocalRunner.
	//
	// NOTHING SETS IT TRUE ANY MORE (hk-5vapm). The refusal logic below is live
	// code but is unreachable in production. This comment used to say "Set true on
	// the codexdriver path" and to call this "the authoritative, race-free
	// enforcement point": that WAS true when hk-5h759 (c2633a95) wrote it — the
	// composition root really did construct this runner with requireBoundary: true
	// — and it stopped being true at hk-tckw3.1 Step 1 (d59d5d32), which flipped it
	// to false. It is corrected rather than carried forward, because an auditor
	// reading the old wording would conclude that unsandboxed codex launches are
	// refused somewhere today. They are not.
	//
	// hk-tckw3.1 Step 1 dropped the fence deliberately: D4 scrapped the ssh worker
	// that was the only thing able to supply the boundary, so arming this would
	// stop codex launching rather than isolate it. Codex containment comes from the
	// srt sandbox (hk-scaj0) instead. The field and its logic are left in place
	// (inert) per the codex-first plan; a P3 removes the runner wholesale.
	requireBoundary bool
}

// refusedIsolationBoundaryArgv0 is a deliberately non-existent binary whose PATH
// NAME is the diagnostic. When a codex crew requires an isolation boundary but
// none is bound, codexWorkerRoutingRunner.Command returns a Command pointing at
// it: exec.Start fails fast and codexdriver.SpawnWindow surfaces the refusal
// (with this path in the error) instead of running codex unsandboxed locally.
const refusedIsolationBoundaryArgv0 = "/nonexistent/harmonik-REFUSED-codex-danger-full-access-requires-enabled-ssh-isolation-boundary-hk5h759"

// codexHeadlessSandbox / codexHeadlessApprovalPolicy are the operator-bound
// (hk-5h759) codex thread posture for headless crew orchestration: run codex
// non-interactively with full workspace access so its writes and commits land.
// This posture is safe ONLY inside the isolation boundary enforced by the
// fail-closed guard (requireBoundary above). NOTE (hk-5vapm): nothing ever sets
// requireBoundary true, so this refusal path is unreachable in production. There is
// no daemon-side counterpart -- earlier comments here named a workloop
// codexRequireIsolationBoundary that does not exist anywhere in the tree.
const (
	codexHeadlessSandbox        = "danger-full-access"
	codexHeadlessApprovalPolicy = "never"
)

// setRegistry late-binds the live worker registry. Wired to
// daemon.Config.WorkerRegistryObserver so the daemon hands over the SAME
// *workers.Registry its tmux dispatch path uses. A nil registry (no worker
// configured, NFR7) leaves the router on the LOCAL path.
func (r *codexWorkerRoutingRunner) setRegistry(reg *workers.Registry) {
	r.reg.Store(reg)
}

// Command selects the per-run spawn transport. When a worker is bound, enabled
// (health-gated + live-disable via the shared registry), and reachable over
// ssh, the codex process is spawned on that worker via SSHRunner{Host}. Any
// other state (no registry bound, no worker, disabled/unhealthy worker,
// non-ssh transport) falls through to LocalRunner — byte-identical local codex
// (NFR7).
//
// Slot capacity accounting stays owned by the daemon's dispatch gate
// (workloop SelectWorker/ReleaseSlot, which runs for every dispatched run);
// this runner mirrors only the host decision via the non-reserving
// WorkerSnapshot peek.
func (r *codexWorkerRoutingRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if reg := r.reg.Load(); reg != nil {
		if w := reg.WorkerSnapshot(); w != nil && w.Enabled && w.Transport == "ssh" {
			// Mirror the tmux path's per-run SSHRunner opts (workloop.go
			// hk-zexsj): a dedicated, non-multiplexed connection per command
			// avoids the ControlMaster truncation family.
			return ltmux.SSHRunner{
				Host: w.Host,
				Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
			}.Command(ctx, name, args...)
		}
	}
	if r.requireBoundary {
		// FAIL CLOSED (hk-5h759): a codex crew requires an isolation boundary but
		// none is ssh-routable here (no registry / no worker / disabled / non-ssh
		// transport). Refuse rather than fall through to LocalRunner, which would
		// run codex danger-full-access UNSANDBOXED on the daemon host. Return a
		// Command whose argv0 does not exist: exec.Start fails immediately and the
		// refusal (with the diagnostic path) propagates up through SpawnWindow.
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

// codexSubstrateOptions builds the structured-driver Options and, when live
// capture is opted in (HARMONIK_CAPTURE_DIR), wires the sessioncapture corpus
// writers into Options.InCapture/OutCapture — the M2-4 production tee (AIS-013).
// Without this wiring the tee is INERT (the writers stay nil and apptap tees to
// nothing). It returns the *sessioncapture.Session so a caller MAY Close it;
// nil session means capture is disabled or could not be established.
//
// AIS-INV-002 (capture never aborts the run): a capture-open failure is logged
// once and swallowed — the driver is returned uncaptured, never an error.
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

// codexWorktreeWritableRoots is the composition-root hook wired into
// codexdriver.Options.WritableRoots (hk-daegv). Given the session's worktree cwd
// it returns the absolute paths codex stamps as the thread's
// `runtimeWorkspaceRoots` (the workspace-write writable roots).
//
// It ALWAYS includes the worktree cwd itself (runtimeWorkspaceRoots REPLACES the
// thread's roots — dropping the cwd would make the worktree unwritable) and, when
// the cwd matches harmonik's linked-worktree layout, the repo's git COMMON dir
// (<repo>/.git). The git common dir holds objects/refs and worktrees/<id>/ and
// lives OUTSIDE the worktree writable root, so without it codex's OWN `git commit`
// fails EPERM under 0.142.0's effective workspace-write seatbelt (see WritableRoots
// doc). An empty cwd, or a cwd not under the worktree root, adds no git dir and
// leaves the behavior unchanged (degrades gracefully).
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

// codexGitCommonDir derives the git COMMON dir (<repo>/.git) of a harmonik linked
// worktree from its path (hk-daegv). A worktree lives at
// <repo>/<worktreeRoot>/<name> (worktreeRoot default ".harmonik/worktrees"); its
// common dir is <repo>/.git. Returns "" when the path does not match that layout
// (e.g. an overridden worktree root, or a non-worktree cwd) — the caller then adds
// no git dir.
//
// Uses plain "/" string ops, NOT filepath: the cwd may be a REMOTE (ssh worker)
// POSIX path, so the derivation must not depend on the local OS path separator.
func codexGitCommonDir(worktreeCwd string) string {
	marker := "/" + workspace.DefaultWorktreeRoot + "/" // "/.harmonik/worktrees/"
	idx := strings.LastIndex(worktreeCwd, marker)
	if idx < 0 {
		return ""
	}
	return worktreeCwd[:idx] + "/.git"
}

// openCaptureSession opens a live-capture corpus session when opted in, else
// returns nil. Off by default; failures degrade to uncaptured (AIS-INV-002).
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
		// AIS-INV-002: never load-bearing — log once, proceed uncaptured.
		log.Printf("harmonik: live session capture disabled (open failed): %v", err)
		return nil
	}
	return sess
}
