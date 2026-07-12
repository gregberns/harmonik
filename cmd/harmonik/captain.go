package main

// captain.go — `harmonik captain` (alias `harmonik start captain`): the
// first-class, NATIVE-Go, full-parity launcher for the Captain LLM session.
// It superseded — and then ES8 (hk-877k) retired — the old caller-side bash
// captain launcher (the former scripts/captain-tools bash entrypoint) and its
// minting+tmux dance (D1: no bash on the launch path, cross-platform, testable).
//
// PARITY (ES2 / hk-bcd0, plan plans/2026-06-20-easy-start-commands §3 + review A):
// the launcher does EVERYTHING the retired bash launcher did, natively:
//
//  1. project = --project or cwd; project-hash computed IN-PROCESS via
//     lifecycle.ComputeProjectHash (no `harmonik project-hash` shell-out, no
//     HK_PROJECT env var).
//  2. Launch into the HASHED namespace harmonik-<hash>-captain
//     (lifecycle.TmuxSessionName), NOT plain "captain", so reap/restart tooling
//     and probeCaptainSentinel (orphansweep.go:518) recognize the session.
//  3. Write .harmonik/cognition/captain.sentinel + captain.pid so the daemon
//     orphan sweep skips the captain while it is live (PL-006d ii). THIS is the
//     D6 fix: the old bare launcher wrote neither, so a captain launched via the
//     Go path was reaped on the next sweep.
//  4. Window-nesting: the captain claude runs in the "agent" window; the keeper
//     runs in a sibling "keeper" window of the SAME session (hk-z036), built via
//     the SHARED agentlaunch helper (review outcome A — same nesting+keeper-arm
//     the daemon's crew spawn uses; no third implementation).
//  5. Keeper WATCHER armed in the keeper window. The warn/act band is OPERATOR
//     CONFIG: the launcher passes NO baked-in band numbers — the keeper reads the
//     keeper: block in .harmonik/config.yaml (and REFUSES TO START if a required
//     value is unset). An explicit --warn-abs-tokens/--act-abs-tokens still flows
//     through. Plus the keeper-enable settings.json stanza wiring kept from hk-igek.
//  6. D7 idempotent pre-flight: if the target session already exists, the
//     launcher gates on AGENT-PANE LIVENESS. If the agent pane is dead/absent
//     (the keeper outlived a stopped agent) it REAPS the stale session and
//     recreates it rather than erroring with tmux "duplicate session". If the
//     agent pane is ALIVE (a real captain is running) it REFUSES — never kills,
//     never recreates — so re-running `start captain` cannot destroy a live
//     conversation by minting a fresh session-id (review fix on hk-bcd0).
//
// WHY a STABLE caller-minted --session-id is load-bearing: it is what lets the
// session-keeper's handoff → /clear → /session-resume cycle re-bind to the same
// conversation (mirrors the crew model — internal/daemon/crewstart.go
// resolveSessionID).
//
// This is a LAUNCHER, not a daemon: it never acquires the daemon pidfile lock,
// so it cannot collide on it (exit 5 is impossible from this path).
//
// SELF-HEAL RESPAWN (ES3 / hk-z1rj, LANDED): the --respawn-cmd seam is now wired
// to the `harmonik captain respawn` subcommand (captain_respawn.go) — the native
// Go replacement for the generated captain-respawn.sh. There is NO verified-
// restart wrapper: per review B, `harmonik keeper restart-now` already does the
// synchronous verified clear→resume in-process, so no captain-restart-verified.sh
// equivalent is generated or built.
//
// Bead refs: hk-ly0n (bare launcher), hk-igek (keeper-enable wiring),
// hk-bcd0 (this — native full parity D1/D6/D7).

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// captainSplashDismissDelay is the wait between the splash-dismiss Enter and the
// boot-seed paste (mirrors daemon.splashDismissDelay = 750ms).
const captainSplashDismissDelay = 750 * time.Millisecond

// captainLaunchRunFn is the seam tests inject to capture the assembled
// agent-window tmux new-session command without spawning a real session.
// Production passes runCaptainTmux. Kept (hk-ly0n) so existing argv tests on the
// agent-window launch continue to assert the exact `tmux new-session` argv.
type captainLaunchRunFn func(cmd *exec.Cmd) error

// keeperEnableFn is the seam tests inject to capture the keeper-enable call
// without touching the real ~/.claude/settings.json. Production passes
// runKeeperEnable (the testable core of `harmonik keeper enable`).
type keeperEnableFn func(cfg enableConfig, stdout, stderr io.Writer) int

// captainReapPriorWatchers is the hk-6629b launch-path reap hook (see
// watcherreap.go). A package var, not a runCaptainLaunchWithOps parameter, so
// the many existing call sites/tests of that function are unaffected; tests
// that care override this var directly and restore it via t.Cleanup.
var captainReapPriorWatchers reapPriorAgentWatchersFn = reapPriorAgentWatchers

// runCaptainTmux is the production run func for the agent-window launch: it
// actually runs the assembled `tmux new-session` command.
func runCaptainTmux(cmd *exec.Cmd) error { return cmd.Run() }

// captainTmuxOps is the seam for the orchestration steps that the single-command
// captainLaunchRunFn cannot express: the D7 existing-session pre-flight (list +
// reap), the sibling keeper-window creation, and reading the agent pane PID for
// captain.pid. Production is osCaptainTmuxOps (delegating to tmux.OSAdapter);
// tests inject a fake to drive the D7 branch and assert the keeper-window argv
// without a real tmux server.
type captainTmuxOps interface {
	// SessionExists reports whether a tmux session named sess is live. Used by
	// the D7 pre-flight to decide reap-then-recreate vs. plain create.
	SessionExists(ctx context.Context, sess string) (bool, error)
	// KillSession tears down a stale session (D7 reap). Idempotent.
	KillSession(ctx context.Context, sess string) error
	// SpawnKeeperWindow adds the sibling keeper window running the keeper argv
	// built from opts. Returns the window Outcome (Err non-nil on failure —
	// the captain WARNS but stays bootable, mirroring the bash launcher).
	SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome
	// AgentPanePID reads the PID of the captain agent window's active pane, for
	// captain.pid. Returns (0, err) when the pane PID can't be resolved.
	AgentPanePID(ctx context.Context, sess string) (int, error)
	// AgentPaneAlive reports whether the captain agent window's pane is backed by
	// a LIVE process — i.e. there is a real captain running, not just a keeper
	// window keeping the tmux session alive. Used by the D7 pre-flight to
	// distinguish "reap a stale session (agent dead)" from "refuse to clobber a
	// live captain". Resolves the agent pane PID and signal-0 probes it. Returns
	// (false, err) only when liveness could not be determined at all; a resolvable
	// but dead/absent pane returns (false, nil).
	AgentPaneAlive(ctx context.Context, sess string) (bool, error)
	// PasteSeedToAgentPane delivers the boot seed to the captain's agent pane
	// after launch so the captain knows to run `harmonik agent brief`. Best-effort:
	// failures WARN to stderr but never block the launch (mirrors crewstart.go
	// pasteCrewMission, T10/hk-02jsj).
	PasteSeedToAgentPane(ctx context.Context, sessionID, paneTarget string)
}

// osCaptainTmuxOps is the production captainTmuxOps backed by tmux.OSAdapter.
// The CLI uses the adapter DIRECTLY (no internal/daemon import) — the shared
// agentlaunch helper is built against the tmux interface, not the daemon, so the
// captain launcher does not drag in daemon deps (the ES2 review-A requirement).
type osCaptainTmuxOps struct {
	adapter ltmux.OSAdapter
}

func (o osCaptainTmuxOps) SessionExists(ctx context.Context, sess string) (bool, error) {
	sessions, err := o.adapter.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s == sess {
			return true, nil
		}
	}
	return false, nil
}

func (o osCaptainTmuxOps) KillSession(ctx context.Context, sess string) error {
	return o.adapter.KillSession(ctx, sess)
}

func (o osCaptainTmuxOps) SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome {
	return agentlaunch.SpawnKeeperWindow(ctx, o.adapter, opts)
}

func (o osCaptainTmuxOps) AgentPanePID(ctx context.Context, sess string) (int, error) {
	// The agent window holds the captain pane; resolve its first-pane PID.
	return o.adapter.WindowPanePID(ctx, ltmux.WindowHandle(sess+":"+ltmux.WindowAgent))
}

// AgentPaneAlive resolves the agent pane PID and signal-0 probes it. A signal-0
// kill checks process existence WITHOUT delivering a signal: nil => the process
// is alive; an error (ESRCH / permission) => not a live, killable process.
//
// If the agent window is absent (the session exists but has no "agent" window —
// the keeper-outlived-the-agent case the D7 reap targets), WindowPanePID returns
// ErrNoSession / a zero pid: that is reported as (false, nil) — NOT a hard error
// — so the caller treats it as "agent dead, safe to reap". A genuine probe
// failure (couldn't even resolve the pid for some other reason) returns
// (false, err) so the caller can decide conservatively.
func (o osCaptainTmuxOps) AgentPaneAlive(ctx context.Context, sess string) (bool, error) {
	pid, err := o.AgentPanePID(ctx, sess)
	if err != nil {
		// No agent window / pane PID unresolvable => treat as not-alive (reapable).
		// errors.Is(ErrNoSession) is the common keeper-outlived-agent shape.
		if errors.Is(err, ltmux.ErrNoSession) {
			return false, nil
		}
		return false, err
	}
	if pid <= 0 {
		return false, nil
	}
	// signal-0: existence probe, no signal delivered.
	if perr := syscall.Kill(pid, 0); perr != nil {
		return false, nil
	}
	return true, nil
}

// PasteSeedToAgentPane delivers the boot seed to the captain's agent pane via
// the bracketed-paste mechanism (mirrors crewstart.go pasteCrewMission).
//
// Message: "Please run `harmonik agent brief` and begin your operating loop."
// HARMONIK_AGENT is set by the launcher so brief auto-resolves the captain type.
//
// Best-effort: errors WARN to stderr but never block the launch (T10/hk-02jsj).
func (o osCaptainTmuxOps) PasteSeedToAgentPane(ctx context.Context, sessionID, paneTarget string) {
	// Dismiss the welcome splash before the paste (hk-rf4ux).
	if err := o.adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed splash dismiss: %v\n", err)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(captainSplashDismissDelay):
	}
	bufName := fmt.Sprintf("harmonik-%s-captain-boot", sessionID)
	const bootSeedMsg = "Please run `harmonik agent brief` and begin your operating loop.\n"
	if err := o.adapter.WriteToPane(ctx, bufName, paneTarget, []byte(bootSeedMsg)); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed paste: %v\n", err)
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(captainSplashDismissDelay):
	}
	if err := o.adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed submit: %v\n", err)
	}
}

// buildCaptainTmuxCmd assembles the exec.Cmd for the captain's agent-window
// session:
//
//	tmux new-session -d -s <session> -n agent -e HARMONIK_AGENT=<name> \
//	  claude --dangerously-skip-permissions --remote-control <name> --session-id <id>
//
// -n agent names the first window so the keeper can target "<session>:agent"
// (window-nesting, hk-z036). --dangerously-skip-permissions mirrors the retired
// bash launcher: a remote-control captain that hit a permission prompt would
// wedge unattended, so it is part of the launcher's correctness contract.
// Returning the fully-built *exec.Cmd (rather than running it inline) is what
// lets the test assert the exact argv via the injected run func.
//
// rcPrefix (hk-igpg) is the per-project Claude RC label prefix: the
// --remote-control LABEL is daemon.JoinRemoteControlName(rcPrefix, name) so it
// shows as "<prefix>-<name>" in the picker. Empty prefix ⇒ bare name (backward
// compatible). HARMONIK_AGENT stays BARE — the prefix is cosmetic, RC-label-only.
func buildCaptainTmuxCmd(name, tmuxSession, sessionID, rcPrefix string) *exec.Cmd {
	return exec.Command(
		"tmux", "new-session", "-d",
		"-s", tmuxSession,
		"-n", ltmux.WindowAgent,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--remote-control", daemon.JoinRemoteControlName(rcPrefix, name),
		"--session-id", sessionID,
	)
}

// runCaptainSubcommand is the main.go entry point for `harmonik captain` and
// `harmonik start captain`. It wires the production run + keeper-enable funcs and
// the production tmux ops.
func runCaptainSubcommand(subArgs []string) int {
	// `harmonik captain respawn …` (ES3 / hk-z1rj): the dead-pane self-heal
	// subverb the keeper's --respawn-cmd seam points at. Peel it off here so
	// `harmonik captain` (no subverb) and `harmonik start captain` keep launching.
	if len(subArgs) >= 1 && subArgs[0] == "respawn" {
		return runCaptainRespawnSubcommand(subArgs[1:])
	}
	return runCaptainLaunchWithOps(subArgs, runCaptainTmux, runKeeperEnable, osCaptainTmuxOps{adapter: ltmux.OSAdapter{}})
}

// buildCaptainKeeperConfig assembles the enableConfig used to wire keeper hooks
// for a freshly-launched captain. Mirrors runKeeperEnableEntry's resolution
// (auto-detected scripts dir, ~/.claude/settings.json) so the launcher and
// `harmonik keeper enable <name>` produce identical wiring. The captain is not a
// known-live agent, so no --yes-destructive gate applies.
func buildCaptainKeeperConfig(name, projectDir string) (enableConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return enableConfig{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return enableConfig{
		agentName:    name,
		projectDir:   projectDir,
		scriptsDir:   autoDetectScriptsDir(projectDir),
		settingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

// ensureBootAssets provisions the skills, scaffolds, context tiers, and AGENTS.md
// router that a captain or crew needs at boot. All steps are create-if-missing
// (force=false) so existing files are never overwritten. Non-fatal: failures WARN
// but never block the launch. Called on every start captain/crew to close the
// portability gap on foreign projects that have not run harmonik init (hk-2nmbq).
// Mirrors the keeper-scripts embed-and-extract approach (hk-ybmqp).
func ensureBootAssets(projectDir string, stdout, stderr io.Writer) {
	if code := provisionSkills(projectDir, false, stdout, stderr); code != 0 {
		fmt.Fprintf(stderr, "harmonik: warning: skill provisioning failed (code %d) — agent may lack .claude/skills/\n", code)
	}
	if code := provisionScaffolds(projectDir, false, stdout, stderr); code != 0 {
		fmt.Fprintf(stderr, "harmonik: warning: scaffold provisioning failed (code %d)\n", code)
	}
	if code := provisionContextTiers(projectDir, false, stdout, stderr); code != 0 {
		fmt.Fprintf(stderr, "harmonik: warning: context-tier provisioning failed (code %d)\n", code)
	}
	// renderAgentsMD substitutes $TARGET_BRANCH; read from config when available.
	targetBranch := "main"
	if pc, err := daemon.LoadProjectConfig(projectDir); err == nil && pc.Daemon.TargetBranch != "" {
		targetBranch = pc.Daemon.TargetBranch
	}
	if code := renderAgentsMD(projectDir, targetBranch, false, stdout, stderr); code != 0 {
		fmt.Fprintf(stderr, "harmonik: warning: AGENTS.md provisioning failed (code %d)\n", code)
	}
}

// runCaptainLaunch keeps the hk-ly0n/hk-igek signature for back-compat with the
// existing argv + keeper-enable tests. It delegates to runCaptainLaunchWithOps
// with the production tmux ops.
func runCaptainLaunch(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn) int {
	return runCaptainLaunchWithOps(subArgs, run, enableKeeper, osCaptainTmuxOps{adapter: ltmux.OSAdapter{}})
}

// captainTmuxSessionName resolves the tmux session name for the captain:
//   - an explicit --tmux value wins (operator override / back-compat);
//   - otherwise the HASHED namespace harmonik-<hash>-captain, computed in-process
//     from the realpath'd project dir (the retired bash launcher derived the same
//     "harmonik-${HASH}-captain" via `harmonik project-hash`, but with a shell-out).
//
// Resolving the realpath first matches lifecycle.ComputeProjectHash's contract
// (callers resolve symlinks before hashing) so the Go launcher and the daemon's
// orphan sweep agree on the session name.
func captainTmuxSessionName(explicitTmux, project string) (string, error) {
	if explicitTmux != "" {
		return explicitTmux, nil
	}
	realDir, err := filepath.EvalSymlinks(project)
	if err != nil {
		// Fall back to the un-resolved abs path: better a deterministic name than
		// a failed launch. EvalSymlinks only fails when a path component is
		// missing, which a valid project root should not hit.
		realDir = project
	}
	hash := lifecycle.ComputeProjectHash(realDir)
	return lifecycle.TmuxSessionName(hash, "captain"), nil
}

// runCaptainLaunchWithOps is the full-parity launch core. Split from
// runCaptainSubcommand so tests can inject (a) the agent-window run func, (b) the
// keeper-enable seam, and (c) the captainTmuxOps for the D7 pre-flight +
// keeper-window + pid steps — all WITHOUT a real tmux server.
//
// Exit codes:
//
//	0  — captain launched (a keeper-enable OR keeper-window failure only WARNS;
//	     sentinel/pid write failures also WARN — the captain stays bootable)
//	1  — flag/arg error, non-UUIDv4 --session-id, cwd resolution, D7 reap failure,
//	     a LIVE captain already running in the target session (REFUSE — no
//	     clobber), an ambiguous liveness probe, or the agent-window tmux launch
//	     itself failing.
func runCaptainLaunchWithOps(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn, ops captainTmuxOps) int {
	fs := flag.NewFlagSet("captain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity")
	tmuxFlag := fs.String("tmux", "", "tmux session name (default: harmonik-<project-hash>-captain)")
	projectFlag := fs.String("project", "", "project directory (default: current working directory)")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to launch with (minted when absent)")
	noKeeperFlag := fs.Bool("no-keeper", false, "skip wiring the keeper hooks into ~/.claude/settings.json")
	// Operator-required config: NO product-imposed default number. 0 = unset → the
	// flag injects nothing and the spawned keeper reads the operator's keeper: block
	// in .harmonik/config.yaml (refusing to start if a required value is missing).
	// An explicitly-passed value is still forwarded to the keeper window.
	warnAbsFlag := fs.Int64("warn-abs-tokens", 0, "keeper WARN band (absolute tokens); 0 = unset → use operator config")
	actAbsFlag := fs.Int64("act-abs-tokens", 0, "keeper ACT/restart band (absolute tokens); 0 = unset → use operator config")
	// hk-igpg: per-project Claude RC label prefix. Sentinel "\x00" distinguishes
	// "flag not passed" (→ fall back to daemon.remote_control_prefix from config)
	// from an explicit "--rc-prefix ''" (→ force a bare label).
	const rcPrefixUnset = "\x00"
	rcPrefixFlag := fs.String("rc-prefix", rcPrefixUnset, "per-project --remote-control label prefix (default: daemon.remote_control_prefix from .harmonik/config.yaml; empty = bare label)")

	if err := fs.Parse(subArgs); err != nil {
		// flag prints its own message; ErrHelp also lands here.
		return 1
	}

	name := *nameFlag
	if name == "" {
		fmt.Fprintln(os.Stderr, "harmonik captain: --name must not be empty")
		return 1
	}

	project := *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: cannot determine working directory: %v\n", err)
			return 1
		}
		project = wd
	}

	// hk-igpg: resolve the RC label prefix. An explicit --rc-prefix (including
	// empty) wins; otherwise fall back to daemon.remote_control_prefix from
	// .harmonik/config.yaml. A config-load error is non-fatal (WARN + bare label).
	rcPrefix := *rcPrefixFlag
	if rcPrefix == rcPrefixUnset {
		rcPrefix = ""
		if pc, perr := daemon.LoadProjectConfig(project); perr == nil {
			rcPrefix = pc.Daemon.RemoteControlPrefix
		} else {
			fmt.Fprintf(os.Stderr, "harmonik captain: could not load .harmonik/config.yaml for rc-prefix (%v) — launching with a bare --remote-control label\n", perr)
		}
	}

	tmuxSession, err := captainTmuxSessionName(*tmuxFlag, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: cannot resolve tmux session name: %v\n", err)
		return 1
	}

	// Resolve the session-id: validate a supplied one (reject non-UUIDv4), mint a
	// fresh UUIDv4 otherwise. keeper.IsPrimarySID is the canonical lowercase
	// UUIDv4 check the keeper itself trusts for PRIMARY identity (sessionid.go);
	// reusing it keeps the launcher and the watcher in lock-step.
	sessionID := *sessionIDFlag
	if sessionID == "" {
		sessionID = uuid.New().String()
	} else if !keeper.IsPrimarySID(sessionID) {
		fmt.Fprintf(os.Stderr, "harmonik captain: --session-id %q is not a canonical lowercase UUIDv4 "+
			"(interactive captain/crew sessions use UUIDv4; the keeper's clear→resume cycle requires it)\n", sessionID)
		return 1
	}

	ctx := context.Background()

	// D7 idempotent pre-flight: if the target session already exists, decide
	// between REAP-then-recreate (the keeper outlived a STOPPED agent) and REFUSE
	// (a LIVE captain is already running). tmux `new-session -s` on an existing
	// name errors "duplicate session" — the native launcher
	// must never throw that — but it must ALSO never blindly clobber a live
	// captain: doing so would kill the running conversation and recreate a fresh
	// session with a NEW session-id, destroying the keeper's clear→resume binding.
	//
	// The decision pivots on agent-pane liveness:
	//   - agent pane DEAD/absent (keeper window kept the session alive) → reap the
	//     whole session and recreate a fresh, correctly-bound agent+keeper pair.
	//   - agent pane ALIVE → REFUSE: do not kill, do not recreate; point the
	//     operator at the live session and exit non-destructively.
	//
	// A SessionExists failure WARNS but does not block (the create will surface a
	// real collision as a tmux error). A liveness-probe failure is treated
	// conservatively as "assume alive" — better to refuse than risk clobbering a
	// live captain on an ambiguous probe.
	if exists, lerr := ops.SessionExists(ctx, tmuxSession); lerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: could not check for an existing session %q: %v — proceeding\n", tmuxSession, lerr)
	} else if exists {
		alive, aerr := ops.AgentPaneAlive(ctx, tmuxSession)
		if aerr != nil {
			// Ambiguous probe: refuse rather than risk clobbering a live captain.
			fmt.Fprintf(os.Stderr, "harmonik captain: could not determine whether the captain in tmux session %q is live (%v); "+
				"refusing to reap it. Stop it first (or pass --tmux <name> to run a second one).\n", tmuxSession, aerr)
			return 1
		}
		if alive {
			fmt.Fprintf(os.Stderr, "harmonik captain: a live captain is already running in tmux session %q; "+
				"stop it first (or pass --tmux <name> to run a second one).\n", tmuxSession)
			return 1
		}
		fmt.Printf("captain: existing tmux session %q found with a DEAD/absent agent pane (keeper outlived a stopped agent) — reaping before recreate (D7)\n", tmuxSession)
		if kerr := ops.KillSession(ctx, tmuxSession); kerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: failed to reap stale session %q: %v\n", tmuxSession, kerr)
			return 1
		}
	}

	// hk-6629b: reap any prior `comms recv --agent <name> --follow` /
	// `subscribe --to <name> --follow` watcher process for this agent name,
	// REGARDLESS of liveness — a captain relaunched after /clear must never
	// leave its predecessor's watcher holding a daemon subscribe slot. This is
	// orthogonal to the D7 tmux-session pre-flight above (D7 gates on the
	// AGENT PANE's liveness; this reaps watcher PROCESSES by argv+identity,
	// live or dead, independent of any tmux session).
	captainReapPriorWatchers(name)

	// Provision boot assets (skills, scaffolds, context tiers, AGENTS.md router)
	// before launching so a foreign project (never run harmonik init) has the
	// files the agent reads at boot. Create-if-missing (force=false): existing
	// files are never overwritten. Non-fatal: failures WARN, never block launch.
	// Mirrors the keeper-scripts embed-and-extract approach (hk-ybmqp, hk-2nmbq).
	ensureBootAssets(project, os.Stdout, os.Stderr)

	// Wire keeper hooks BEFORE launching tmux so the new `claude` session reads
	// the statusLine + Stop + PreCompact stanzas at session start. A failure here
	// only WARNS — the captain must stay bootable even if scripts can't be found.
	if !*noKeeperFlag {
		cfg, cerr := buildCaptainKeeperConfig(name, project)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: %v\n", cerr)
			return 1
		}
		if rc := enableKeeper(cfg, os.Stdout, os.Stderr); rc != 0 {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper enable returned %d — launching anyway; "+
				"run `harmonik keeper enable %s` manually to wire keeper hooks\n", rc, name)
		}
	}

	// 1) Launch the captain agent window. The agent-window run func is the hk-ly0n
	//    test seam; the assembled command is `tmux new-session -d -s <session>
	//    -n agent ... claude ...`.
	cmd := buildCaptainTmuxCmd(name, tmuxSession, sessionID, rcPrefix)
	if rerr := run(cmd); rerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: launch tmux session %q: %v\n", tmuxSession, rerr)
		return 1
	}

	// 1.5) Paste the boot seed into the captain's agent pane so the captain runs
	//      `harmonik agent brief` as its first action (T10/hk-02jsj — symmetric seed
	//      paste mirroring crewstart.go pasteCrewMission). Best-effort: never blocks.
	agentPane := tmuxSession + ":" + ltmux.WindowAgent
	ops.PasteSeedToAgentPane(ctx, sessionID, agentPane)

	// 2) Write captain.sentinel + captain.pid to .harmonik/cognition/ so the
	//    daemon orphan sweep skips the captain session while it is live
	//    (PL-006d ii). THE D6 FIX. Best-effort: a write failure WARNS — the
	//    captain is already up, and the sweep also probes the live tmux session
	//    (probeCaptainSentinel PRIMARY path) so a missing pid is recoverable.
	if werr := writeCaptainSentinelAndPID(ctx, ops, project, tmuxSession); werr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: %v — the daemon orphan sweep may reap this captain "+
			"until the sentinel/pid is written; re-run the launcher or `harmonik supervise` to refresh\n", werr)
	}

	// 3) Arm the keeper WATCHER in a sibling "keeper" window with the real
	//    warn/act band (the parity gap the old bare launcher left open — settings
	//    stanzas alone never drive warn/act/restart). Built via the SHARED
	//    agentlaunch helper. Best-effort: a keeper-window failure WARNS — the
	//    captain agent window is already live.
	//
	//    --respawn-cmd seam (ES3 / hk-z1rj): the dead-pane self-heal respawn
	//    command. RespawnCmd is the `harmonik captain respawn …` invocation the
	//    keeper runs (via `sh -c`, watcher.go maybeRespawn) when the agent pane
	//    dies — it respawns ONLY the agent window with --resume <sid> (NOT
	//    --session-id: resume keeps the SAME conversation), keeper window survives,
	//    no dup keeper (hk-z036 / hk-opuv). The binary is resolved via
	//    os.Executable() so the keeper's `sh -c` finds the SAME harmonik binary.
	//    There is NO verified-restart wrapper (review B): keeper restart-now
	//    already does the synchronous verified work in-process; nothing is
	//    generated here for it.
	if !*noKeeperFlag {
		keeperBin, exErr := os.Executable()
		if exErr != nil {
			keeperBin = "harmonik" // fallback: rely on PATH
		}
		respawnCmd := captainRespawnCmdString(keeperBin, name, tmuxSession, sessionID, project)
		outcome := ops.SpawnKeeperWindow(ctx, agentlaunch.KeeperWindowOpts{
			KeeperBin:     keeperBin,
			AgentName:     name,
			Session:       tmuxSession,
			ProjectDir:    project,
			WarnOnly:      false, // captain is FORCE-CUT: full warn→act→restart band.
			WarnAbsTokens: *warnAbsFlag,
			ActAbsTokens:  *actAbsFlag,
			RespawnCmd:    respawnCmd, // hk-z1rj: `harmonik captain respawn …` (ES3).
		})
		if outcome.Err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper watcher window failed (%v) — launching anyway; "+
				"the captain has NO warn/act/restart watcher until you arm one manually with "+
				"`harmonik keeper --agent %s --tmux %s:%s` "+
				"(the band comes from the keeper: block in .harmonik/config.yaml — run "+
				"`harmonik keeper config --example` if it is unset)\n",
				outcome.Err, name, tmuxSession, ltmux.WindowAgent)
		}
	}

	fmt.Printf("captain launched: name=%q tmux=%q session_id=%s project=%q (keeper band from operator config)\n",
		name, tmuxSession, sessionID, project)
	if *noKeeperFlag {
		fmt.Printf("captain launched; keeper wiring skipped (--no-keeper) — run `harmonik keeper enable %s` and arm a watcher to wire warn/act.\n", name)
	} else {
		fmt.Printf("captain launched; keeper hooks wired + watcher armed in sibling '%s:%s' window (pass --no-keeper to skip).\n", tmuxSession, ltmux.WindowKeeper)
	}
	return 0
}

// writeCaptainSentinelAndPID writes .harmonik/cognition/captain.sentinel
// (schema_version=1, mirroring supervisor.sentinel) and captain.pid (the agent
// pane PID) so the daemon orphan sweep skips the live captain session (PL-006d
// ii). The path layout MUST match orphansweep.go's captainSentinelPath /
// captainPidfilePath (.harmonik/cognition/) or the sweep won't find them.
//
// The pid is resolved from the live agent pane via ops.AgentPanePID; on failure
// the sentinel is still written (the sweep's PRIMARY path probes the live tmux
// session, so a missing pid degrades gracefully to that probe) and the pid write
// is skipped with the error returned for a WARN.
func writeCaptainSentinelAndPID(ctx context.Context, ops captainTmuxOps, project, tmuxSession string) error {
	cognitionDir := filepath.Join(project, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		return fmt.Errorf("create cognition dir %q: %w", cognitionDir, err)
	}
	sentinelPath := filepath.Join(cognitionDir, "captain.sentinel")
	if err := os.WriteFile(sentinelPath, []byte("schema_version=1\n"), 0o644); err != nil {
		return fmt.Errorf("write captain.sentinel: %w", err)
	}

	pid, perr := ops.AgentPanePID(ctx, tmuxSession)
	if perr != nil || pid <= 0 {
		return fmt.Errorf("captain.sentinel written but could not resolve agent pane PID for captain.pid (%v)", perr)
	}
	pidPath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		return fmt.Errorf("write captain.pid: %w", err)
	}
	return nil
}
