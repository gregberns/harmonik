package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/sessioncapture"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
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
// The third return value, requireIsolationBoundary, is true ONLY on the
// codexdriver path: a codex app-server crew runs with a permissive sandbox
// posture (danger-full-access) that is safe solely inside a real isolation
// boundary — an enabled remote ssh worker IS that boundary. It is the signal the
// daemon's fail-closed guard keys off (hk-5h759): with it set, the work loop
// REFUSES to launch a codex run that has no worker bound (which would otherwise
// fall through codexWorkerRoutingRunner.Command to LocalRunner and run codex
// UNSANDBOXED on the daemon host). False for the tmux path (no such posture).
func selectSubstrate(tmuxSub handler.Substrate, codexBinary string) (sub handler.Substrate, bindRegistry func(*workers.Registry), requireIsolationBoundary bool) {
	if os.Getenv(substrateSelectEnv) != "codexdriver" {
		return tmuxSub, nil, false
	}
	router := &codexWorkerRoutingRunner{requireBoundary: true}
	opts, _ := codexSubstrateOptions(codexBinary, router)
	return codexdriver.NewCodexSubstrate(opts), router.setRegistry, true
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

	// requireBoundary makes this runner FAIL CLOSED (hk-5h759). Set true on the
	// codexdriver path (a codex crew runs danger-full-access, safe ONLY inside an
	// enabled ssh worker/container). When set and no enabled ssh worker is bound,
	// Command REFUSES rather than falling through to LocalRunner — which would run
	// codex UNSANDBOXED on the daemon host. This is the authoritative, race-free
	// enforcement point: it evaluates the SAME predicate that decides ssh-vs-local
	// AT spawn time, so it closes the TOCTOU window a caller-side admission check
	// alone cannot (a worker disabled between admission and spawn is caught here).
	requireBoundary bool
}

// refusedIsolationBoundaryArgv0 is a deliberately non-existent binary whose PATH
// NAME is the diagnostic. When a codex crew requires an isolation boundary but
// none is bound, codexWorkerRoutingRunner.Command returns a Command pointing at
// it: exec.Start fails fast and codexdriver.SpawnWindow surfaces the refusal
// (with this path in the error) instead of running codex unsandboxed locally.
const refusedIsolationBoundaryArgv0 = "/nonexistent/harmonik-REFUSED-codex-danger-full-access-requires-enabled-ssh-isolation-boundary-hk5h759"

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
		Runner: runner, // M4-C3: per-run worker-routing runner (SSHRunner remote / LocalRunner local)
		Clock:  substrate.SystemClock{},
	}
	sess := openCaptureSession()
	if sess != nil {
		opts.InCapture = sess.Input()
		opts.OutCapture = sess.Output()
	}
	return opts, sess
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
