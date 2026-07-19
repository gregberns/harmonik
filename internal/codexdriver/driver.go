// Package codexdriver implements the structured Codex app-server driver — the
// SECOND handler.Substrate implementation, sitting alongside the tmux substrate
// and selectable at the daemon composition root (AIS-015).
//
// The driver owns the child's stdio directly (AIS-009): it spawns
// `codex app-server`, speaks the JSON-RPC 2.0 NDJSON wire through
// internal/codexwire, and drives the pure T5 input reactor
// (internal/codexinput) over the generic substrate.Run loop. tmux is never on
// this input path (no load-buffer / paste-buffer / send-keys — spec
// agent-input.md AIS-010 boundary).
//
// # Shape
//
//	SpawnWindow ──► child (via CommandRunner, AIS-016 remote seam)
//	  stdin  ◄── apptap.CaptureWriter ◄── effector writes (codexwire.Marshal)
//	  stdout ──► apptap.CaptureReader ──► scanner ──► codexwire.Parse ──► Event
//	  Events ──► substrate.Run(src, reactor.Step, effector) ──► Actions ──► IO
//
// The returned session satisfies BOTH handler.SubstrateSession and
// handler.InputPort, so the daemon routes input through handler.AsInputPort
// with no daemon-side change (AIS-001/AIS-002 seam).
//
// # Determinism
//
// ALL timing goes through substrate.ClockPort (RS-015): the reactor arms
// timers via Actions and this driver honors them with ClockPort sleeps that
// feed timer_fired Events back in. There is ZERO time.Sleep / time.After in
// this package (SC6 gate).
//
// # Twin-blindness (AIS-015)
//
// The driver has no awareness of a selection axis or of being under test:
// L2/L3 doubles substitute at the WIRE — a twin binary speaking the same
// NDJSON on stdio, injected via Options.Binary/Args or the CommandRunner.
//
// Spec refs: specs/agent-input.md AIS-009/010/015/016/017, AIS-INV-001;
// specs/handler-contract.md HC-069/070, HC-INV-008; design
// 04-design/driver-design.md §5 (read claude* as codex* per COORD c019).
package codexdriver

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/substrate"
)

// CommandRunner is the process-spawn seam (AIS-016). It is declared locally —
// structurally identical to the tmux package's CommandRunner — because
// codexdriver MUST NOT import internal/lifecycle/tmux (depguard). The
// composition root wires tmux.LocalRunner (which satisfies this interface
// structurally) for local runs and an SSH runner for remote workers (M4 owns
// the transport; this package only keeps the seam).
type CommandRunner interface {
	Command(ctx context.Context, name string, args ...string) *exec.Cmd
}

// RemoteCwdRunner is an OPTIONAL capability a CommandRunner may implement when it
// executes the child on a REMOTE host (an ssh transport). For such a runner the
// spawn working directory (SubstrateSpawn.Cwd) is a REMOTE worktree path: setting
// the LOCAL exec.Cmd.Dir to it fork/exec-ENOENTs the local ssh process, and
// without a remote `cd` the child would run in the ssh login $HOME, not the
// worktree (both hk-czb11). CommandInDir returns an *exec.Cmd whose REMOTE command
// runs in dir, leaving the LOCAL exec.Cmd.Dir unset (a box-A-valid default). A
// LOCAL runner does NOT implement this; spawn then sets exec.Cmd.Dir = Cwd as
// before (byte-identical to the pre-fix local path). Declared locally
// (structurally satisfied by tmux.SSHRunner) so codexdriver keeps its no-import-
// of-tmux depguard boundary.
type RemoteCwdRunner interface {
	CommandInDir(ctx context.Context, dir, name string, args ...string) *exec.Cmd
}

// localRunner is the default CommandRunner: plain exec.CommandContext.
type localRunner struct{}

func (localRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// Emission is one durable-event emission the reactor requested
// (ActionTypeEmit). The composition root's Options.Emit hook forwards these to
// the durable bus (the §8.21 registered agent_input_* / agent_launch_failure
// events); the driver itself never touches the bus.
type Emission struct {
	Type     codexinput.EmitType
	InputSeq uint64
	TurnID   string
	Reason   string
}

// Options configures a codex substrate. Zero values get safe defaults where
// noted; Binary is required unless every SpawnWindow call supplies Argv.
type Options struct {
	// Binary is the codex executable (e.g. "codex"); used with Args when
	// SubstrateSpawn.Argv is empty.
	Binary string
	// Args are the child arguments; default {"app-server"}.
	Args []string
	// Runner is the process-spawn seam (AIS-016). Default: local exec.
	Runner CommandRunner
	// Clock supplies ALL driver timing (RS-015). Default: substrate.SystemClock.
	Clock substrate.ClockPort
	// Config carries the reactor's bounded-liveness windows (AIS-INV-001).
	Config codexinput.Config
	// Emit, when non-nil, receives every durable-event emission the reactor
	// requested. Called from the driver loop goroutine; must not block.
	Emit func(Emission)
	// InCapture / OutCapture, when non-nil, receive a verbatim tee of the
	// stdin / stdout wire bytes (apptap invariant: transparent, lossless,
	// verbatim).
	InCapture  io.Writer
	OutCapture io.Writer
	// PreSpawn, when non-nil, runs immediately before every (re)spawn of the
	// app-server child — both the normal SpawnWindow launch and the resident
	// owner's respawn. It is the injection seam for ungraceful-kill recovery
	// (hk-160yb G4): the daemon composition root wires the codex stale-WAL guard
	// (internal/daemon.cleanCodexStaleWAL) here so a child that was SIGKILLed /
	// crashed does not fast-fail the NEXT launch on the stale state_*.sqlite-wal
	// it left behind. codexdriver MUST NOT import internal/daemon (layering), so
	// the guard is injected, never called directly. FAIL-CLOSED: a non-nil error
	// aborts the spawn rather than launching onto a known-stale state — better a
	// surfaced launch failure than a silent codex fast-fail.
	PreSpawn func(context.Context) error

	// Sandbox and ApprovalPolicy set the codex thread posture stamped on every
	// thread/start and thread/resume handshake (hk-5h759). Headless crew
	// orchestration needs a non-interactive posture — the driver auto-declines
	// any approval request it receives (no approval negotiation; see
	// session.go handleServerRequest), so under codex's default policy exec /
	// apply-patch prompts are declined and the crew's writes and commits never
	// land. The composition root sets Sandbox="danger-full-access" +
	// ApprovalPolicy="never" so codex runs non-interactively and its work lands.
	// That posture is safe ONLY inside a real isolation boundary — the daemon's
	// fail-closed guard (cmd/harmonik/substrate_select.go requireBoundary +
	// workloop codexRequireIsolationBoundary) refuses to launch a codex crew
	// with no enabled ssh worker bound. Empty (the zero value) OMITS the field
	// on the wire, leaving codex's own default posture — so a driver built
	// WITHOUT the composition root never silently runs danger-full-access.
	Sandbox        string
	ApprovalPolicy string
}

// codexSubstrate is the handler.Substrate implementation.
type codexSubstrate struct {
	opts Options
}

// NewCodexSubstrate constructs the structured Codex driver substrate. Mirrors
// the tmux shape (exported constructor returning handler.Substrate; private
// session type). The composition root selects tmux vs codexdriver by which
// value it wires into daemon.Config.Substrate (AIS-015 — never a runtime
// test-branch inside the driver).
func NewCodexSubstrate(opts Options) handler.Substrate {
	if opts.Runner == nil {
		opts.Runner = localRunner{}
	}
	if opts.Clock == nil {
		opts.Clock = substrate.SystemClock{}
	}
	if len(opts.Args) == 0 {
		opts.Args = []string{"app-server"}
	}
	return &codexSubstrate{opts: opts}
}

// SpawnWindow spawns the codex app-server child, wires the stdio splice, and
// starts the reactor loop. The spawn ctx bounds only the spawn itself; the
// child's lifetime is session-owned (Kill / process exit), not spawn-ctx-owned.
//
// SubstrateSpawn.Argv, when non-empty, overrides Options.Binary/Args.
// StdinDevNull is ignored: under the structured protocol the driver owns the
// child's stdin as the wire (AIS-009/AIS-010 — the /dev/null disposition
// belongs to the tmux/exec paths, not this one).
func (c *codexSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return c.spawn(ctx, in, "")
}

// spawn is the shared spawn body for SpawnWindow (fresh thread) and the resident
// owner's respawn path (resume). When resumeThreadID is non-empty the session's
// launch handshake issues `thread/resume <resumeThreadID>` instead of
// `thread/start`, re-attaching to the prior server-side thread after a child
// death (hk-160yb G1: reconnect via respawn→initialize→thread/resume, using the
// G2 wire method). resumeThreadID is otherwise the empty string and the path is
// byte-for-byte the pre-G1 fresh-thread launch.
func (c *codexSubstrate) spawn(ctx context.Context, in handler.SubstrateSpawn, resumeThreadID string) (handler.SubstrateSession, error) {
	// Ungraceful-kill recovery seam (G4): clean any stale WAL a killed/crashed
	// prior child left behind BEFORE launching. Fail-closed — never launch onto a
	// known-stale state.
	if c.opts.PreSpawn != nil {
		if err := c.opts.PreSpawn(ctx); err != nil {
			return nil, fmt.Errorf("codexdriver: pre-spawn guard: %w", err)
		}
	}

	argv := in.Argv
	if len(argv) == 0 {
		if c.opts.Binary == "" {
			return nil, fmt.Errorf("codexdriver: no argv and no Options.Binary")
		}
		argv = append([]string{c.opts.Binary}, c.opts.Args...)
	}

	// The child must outlive the spawn call: tie the exec ctx to a
	// session-owned cancel (released by Kill or by process exit), never to the
	// dispatch-scoped spawn ctx.
	procCtx, procCancel := context.WithCancel(context.Background())

	// Remote-cwd-aware spawn (hk-czb11). A remote transport (ssh) runs the child
	// ON THE WORKER, so in.Cwd is a REMOTE worktree path. Applying it as the LOCAL
	// exec.Cmd.Dir fork/exec-ENOENTs the local `ssh …` process, and without a
	// remote `cd` the child runs in the ssh login $HOME rather than the worktree.
	// When the runner advertises RemoteCwdRunner, apply the cwd REMOTELY and leave
	// the local exec.Cmd.Dir UNSET; a local runner keeps the direct
	// exec.Cmd.Dir = in.Cwd path (byte-identical to before).
	var cmd *exec.Cmd
	if rc, ok := c.opts.Runner.(RemoteCwdRunner); ok && in.Cwd != "" {
		// hk-okqyx: ssh does NOT forward the local process env (cmd.Env below),
		// so in.Env would never reach the remote codex. Deliver it via an
		// `env KEY=VAL … <binary> <args>` argv prefix the remote login-shell
		// `exec`s in place. cmd.Env below stays load-bearing for the LOCAL branch.
		name, remoteArgv := handler.RemoteExecArgv(in.Env, argv[0], argv[1:])
		cmd = rc.CommandInDir(procCtx, in.Cwd, name, remoteArgv...) //nolint:contextcheck // session-owned lifetime by design (see comment above)
	} else {
		cmd = c.opts.Runner.Command(procCtx, argv[0], argv[1:]...) //nolint:contextcheck // session-owned lifetime by design (see comment above)
		cmd.Dir = in.Cwd
	}
	if in.Env != nil {
		cmd.Env = in.Env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		procCancel()
		return nil, fmt.Errorf("codexdriver: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		procCancel()
		return nil, fmt.Errorf("codexdriver: stdout pipe: %w", err)
	}
	stderrRing := newRingWriter(stderrTailCap)
	cmd.Stderr = stderrRing

	if err := cmd.Start(); err != nil {
		procCancel()
		return nil, fmt.Errorf("codexdriver: start %q: %w", argv[0], err)
	}

	//nolint:contextcheck // captureDegradeLogger logs from the session-lifetime-owned tee; no request ctx to inherit (same rationale as runLoop).
	s := newCodexSession(c.opts, cmd, procCancel, stdin, stdout, stderrRing)
	// resumeThreadID is immutable for the session's life; set before start() so
	// the readLoop's handshake branch (handleResponse pendingInitialize) reads a
	// fully-published value with no race against the reactor goroutines.
	s.resumeThreadID = resumeThreadID
	s.start(ctx)
	return s, nil
}
