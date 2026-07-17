package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/branching"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

// Config holds the startup configuration for the harmonik daemon.
//
// At MVH the struct is intentionally minimal: subsystem-specific fields are
// added by the per-registry beads (hk-8mup.62, hk-8i31.83) as each registry
// is wired into [Start].
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020 — internal/daemon is the
// composition root; Config is its public configuration surface.
type Config struct {
	// ProjectDir is the root directory of the harmonik project. It is the
	// directory that contains .beads/, .harmonik/, and the worktree parent.
	// Must be an absolute path resolved by the caller (cmd/harmonik) before
	// passing in. An empty string is only valid in unit tests that do not
	// exercise path-dependent behaviour.
	//
	// MVH_ROADMAP row #1 (hk-56ajv).
	ProjectDir string

	// WorkflowModeDefault is the daemon-level default workflow mode loaded once
	// at PL-005 step 0 per §PL-004a.  It is the second-lowest-precedence tier
	// of the four-tier resolution chain (execution-model.md §4.3 EM-012a):
	// per-bead label → per-project → daemon-default (this field) → built-in
	// fallback (dot, hk-30vlb).
	//
	// The zero value (empty string) is a startup error (fail-closed, hk-81n9r):
	// callers must set an explicit mode. Use [core.WorkflowModeDot] for the
	// standard default (the embedded standard-bead.dot). When the embedded DOT
	// graph fails to load, workloop demotes to review-loop as a safety floor
	// (EM-012a-FLOOR) — NEVER to single. Any unrecognised non-empty value is
	// also rejected at startup so the daemon fails fast rather than silently
	// degrading.
	//
	// The field is immutable for the daemon's lifetime; mid-run changes require
	// a daemon restart (or exec-replacement via harmonik upgrade per PL-027).
	//
	// Spec ref: specs/process-lifecycle.md §4.1 PL-004a; §4.2 PL-005 step 0.
	// Bead ref: hk-7om2q.8, hk-81n9r.
	WorkflowModeDefault core.WorkflowMode

	// LogWriter is the destination for structured daemon log output.
	// A nil LogWriter silences all log output (useful in tests).
	LogWriter io.Writer

	// JSONLLogPath is the absolute path for the durable JSONL event log.
	//
	// When non-empty, daemon.Start opens this file with O_CREATE|O_WRONLY|O_APPEND
	// and threads the resulting [eventbus.JSONLWriter] into the event bus so that
	// every Emit call appends a JSONL line. F-class (fsync-boundary) events are
	// fsynced before Emit returns per EV-016 / EV-016a.
	//
	// The canonical MVH path is <ProjectDir>/.harmonik/events/events.jsonl
	// (event-model.md §6.2). When empty, JSONL logging is disabled (useful for
	// unit tests that use an in-memory bus only).
	//
	// Spec ref: specs/event-model.md §4.4 EV-015, EV-016; §6.2 EV-020.
	// Bead ref: hk-8mup.63.
	JSONLLogPath string

	// BrPath is the absolute path to the `br` CLI binary.
	//
	// Must be non-empty when the work loop is active (i.e., ProjectDir is set).
	// Production callers resolve it via exec.LookPath("br") at startup.
	// When empty the work loop is skipped (unit-test mode without a bead ledger).
	//
	// Bead ref: hk-ecrxy.
	BrPath string

	// KerfPath is the absolute path to the `kerf` CLI binary.
	//
	// When non-empty the eager-refill path (EM-062/EM-063) calls
	// `kerf next --format=json --only=bead` to obtain refill candidates.
	// When empty or when kerf is not installed, eager-refill is disabled and
	// the daemon relies solely on items already in the queue.
	//
	// Production callers resolve it via exec.LookPath("kerf") at startup.
	// Tests that do not exercise eager-refill leave this field empty.
	//
	// Spec ref: specs/execution-model.md §4.13 EM-062, EM-063.
	// Bead ref: hk-9321v.
	KerfPath string

	// HandlerBinary is the executable to spawn for each bead dispatch.
	//
	// When empty the work loop defaults to "claude". The exploratory-testing wave
	// (EXPLORATORY_TESTING_PLAN.md §6) overrides this field with a twin binary
	// path so that test runs do not consume API credits.
	//
	// Bead ref: hk-ecrxy.
	HandlerBinary string

	// HandlerArgs are extra arguments appended to the handler binary invocation
	// for every bead dispatch.
	//
	// The work loop sets LaunchSpec.Args = HandlerArgs on each iteration. When
	// nil no extra arguments are passed. Tests may supply ["-c", "exit 0"] to
	// exercise the handler path without a real claude binary (hk-4e5b5).
	//
	// Bead ref: hk-4e5b5.
	HandlerArgs []string

	// HandlerEnv is the environment for handler subprocesses in "KEY=VALUE" form.
	//
	// When nil the child inherits no environment. Production callers MUST inject
	// at minimum HARMONIK_PROJECT_HASH (lifecycle.ProvenanceEnvVar). Tests may
	// supply a minimal environment or nil.
	//
	// Bead ref: hk-ecrxy.
	HandlerEnv []string

	// MaxConcurrent is the maximum number of beads the work loop may dispatch
	// concurrently. A value of zero is treated as 1, preserving MVH
	// single-threaded semantics for any caller that does not set the field
	// (zero-value compatibility).
	//
	// Ceiling enforcement lives in the work-loop scheduler (row 5, hk-e61c3.2),
	// NOT in the bus or adapter. Row 5 reads this field and gates goroutine
	// creation accordingly.
	//
	// The composition root (cmd/harmonik/main.go) exposes this as --max-concurrent.
	// Default: 1. Valid range: ≥1. Values >1 are inert until hk-e61c3.2 lands.
	//
	// Bead ref: hk-e61c3.1. POST_MVH_PARALLELISM_ROADMAP row 6.
	MaxConcurrent int

	// AgentReadyTimeout is the maximum duration the daemon waits for an
	// agent_ready event after launching a handler subprocess per HC-056.
	//
	// A zero value falls back to the defaultAgentReadyTimeout constant (150s as of
	// hk-5z1f0) declared in agentready.go. Operators may reduce this value in
	// environments with fast cold-start paths or increase it for slow disk-cache
	// warm-up or high-concurrency burst scenarios. The timeout is applied
	// per-dispatch (not per-daemon lifetime).
	//
	// On expiry: the session context is cancelled, the subprocess is reaped,
	// agent_failed{class=structural, sub_reason=agent_ready_timeout} is emitted,
	// and the bead is reopened (HC-056 steps 1–4). Wiring into the workloop
	// completion path lands in hk-gql20.14/.15.
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-gql20.18.
	AgentReadyTimeout time.Duration

	// RemoteAgentReadyTimeout is the agent_ready wait window applied to a
	// dispatch routed to a REMOTE (SSH worker) node instead of AgentReadyTimeout.
	//
	// A zero value falls back to defaultRemoteAgentReadyTimeout (210s as of
	// hk-96d7w) declared in agentready.go. Remote spawns clear reverse-SSH-tunnel
	// readiness on top of the claude cold-start itself and, for the reviewer
	// node, may compete with a resident implementer agent for CPU/disk on the
	// same worker — the separate, longer default covers that extra latency
	// without loosening the local timeout.
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-96d7w (LOCAL slice of hk-5z1f0).
	RemoteAgentReadyTimeout time.Duration

	// Substrate is the optional tmux substrate for handler.Launch.
	//
	// When non-nil it is injected into workLoopDeps.substrate so that each bead
	// dispatch spawns a new tmux window instead of forking a subprocess directly
	// via exec.CommandContext.
	//
	// The production composition root (cmd/harmonik/main.go) reads $TMUX, resolves
	// the current session name via tmux display-message, constructs a
	// daemon.NewTmuxSubstrate, and stores it here. When nil the work loop falls
	// back to exec.CommandContext (unit-test mode / non-tmux environments).
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	// Bead ref: hk-kqdpf.4.
	Substrate handler.Substrate

	// DaemonBinaryPath is the absolute path to the running harmonik binary,
	// resolved via os.Executable() at daemon startup (hk-kqdpf.6).
	//
	// It is materialized as the hook "command" field in every workspace's
	// .claude/settings.json so that Claude's hook-relay subprocess can be found
	// regardless of the tmux window's $PATH. If the daemon binary is run from a
	// non-installed path (e.g. /tmp/hk), a bare "harmonik" command would fail in
	// the tmux window — this field avoids that failure.
	//
	// MUST be non-empty in production; cmd/harmonik/main.go resolves it via
	// os.Executable() and fails fast if that call errors. When empty, the work
	// loop substitutes the literal string "harmonik" as a fallback for legacy
	// unit-test callers that do not set this field.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.1 CHB-003 (hook command field).
	// Bead ref: hk-kqdpf.6.
	DaemonBinaryPath string

	// CPRegistry is the daemon's ControlPoint registry loaded from policy YAML
	// at startup per specs/control-points.md §4.9.CP-043 and CP-045.
	//
	// When non-nil, the work loop uses it to resolve gate_ref values to Gate
	// ControlPoints during DOT workflow gate-node dispatch (hk-karlz). Both
	// mechanism-tagged gates (PolicyExpression evaluation) and cognition-tagged
	// gates (subprocess dispatch) are supported.
	//
	// The zero value (nil) is safe: gate nodes return a structural eval-failure
	// Outcome (status=FAIL) without crashing. Pass a populated core.Registry
	// (e.g. from S02PolicyEngine.Registry()) to enable real gate evaluation.
	//
	// Callers: cmd/harmonik/main.go SHOULD populate this from the project's
	// policy YAML before calling daemon.Start. Until policy YAML loading is
	// wired into the composition root, nil is the correct production value.
	//
	// Spec ref: specs/control-points.md §4.9.CP-043, §4.9.CP-045.
	// Bead ref: hk-karlz.
	CPRegistry core.Registry

	// ProjectCfg is the decoded .harmonik/config.yaml loaded once at startup
	// (EM-012b tier-2). Populated by Start via LoadProjectConfig; callers may
	// leave it zero-value for tests that do not exercise project-config resolution.
	//
	// The zero value (ProjectConfig{}) is safe: LookupAgent returns ("","") for
	// all agent types, causing resolution to fall through to tier 3 and tier 4.
	//
	// Spec ref: specs/execution-model.md §4.3 EM-012b — tier-2 slot.
	// Bead ref: hk-bfvk7.
	ProjectCfg ProjectConfig

	// BinaryCommitHash is the git commit hash of the running daemon binary,
	// injected at build time via -ldflags "-X main.commitHash=<sha>" and
	// forwarded here from the composition root (cmd/harmonik/main.go).
	//
	// It is emitted verbatim in the daemon_started event payload
	// (binary_commit_hash field, §8.7.1). When empty Start falls back to
	// "unknown" so that the field is always well-formed per the spec.
	//
	// The zero value ("") is safe for unit tests that do not care about the
	// stamped hash; they will see "unknown" in the emitted payload.
	//
	// Spec ref: specs/event-model.md §8.7.1 (daemon_started payload).
	// Bead ref: hk-mz0x4.
	BinaryCommitHash string

	// CancelOnQueueDrain, when non-nil, is called once after the queue
	// transitions to all-success and ClearQueue completes.  The cancel causes
	// the daemon context to expire so harmonik exits cleanly instead of
	// idle-spinning waiting for more work.
	//
	// Set by the `harmonik run <bead-id>` subcommand (hk-icecw) to implement
	// exit-on-empty semantics: a queue of one item terminates naturally after
	// CompleteAndUnlink + ClearQueue, and the cancel propagates through the
	// daemon context to runWorkLoop.
	//
	// The zero value (nil) is safe: the daemon continues running after the
	// queue drains, which is the normal daemon behaviour.
	//
	// Bead ref: hk-icecw.
	CancelOnQueueDrain context.CancelFunc

	// CancelOnQueueExit, when non-nil, is called once when the queue reaches
	// a terminal state — either all-success (after ClearQueue) OR
	// paused-by-failure (after Persist).  Together with CancelOnQueueDrain
	// this ensures harmonik run <bead-id> exits promptly on both outcome
	// paths instead of idle-spinning waiting for more work.
	//
	// The zero value (nil) is safe: the daemon continues running after the
	// queue exits, which is the normal daemon behaviour.
	//
	// Bead ref: hk-8jh26.
	CancelOnQueueExit context.CancelFunc

	// StopDispatchCtx, when non-nil, is the context checked by the work loop's
	// outer poll to decide whether to pull new beads. When this context is
	// cancelled the loop stops dispatching and waits for in-flight goroutines to
	// drain — but in-flight goroutines continue running on the main ctx.
	//
	// This separates "stop dispatching new work" (StopDispatchCtx) from "cancel
	// in-flight work" (ctx passed to daemon.Start). Without this split,
	// CancelOnQueueDrain/CancelOnQueueExit cancel the shared runCtx, which kills
	// in-flight reviewer goroutines (hk-2o2i9).
	//
	// When nil the work loop uses ctx (the daemon context) for both dispatch-halt
	// and in-flight lifetime, preserving backward-compatible behaviour.
	//
	// Bead ref: hk-2o2i9.
	StopDispatchCtx context.Context

	// SkipWALCheckpoint, when true, disables the advisory WAL-checkpoint
	// pre-flight that runs at PL-005 step 0 before the first brcli call.
	//
	// The pre-flight is non-fatal and transparent in production. This field
	// exists solely for unit tests that operate on fake or absent .beads
	// databases where spawning sqlite3 would be a no-op at best and
	// confusing at worst.
	//
	// Default (false): pre-flight runs when ProjectDir is set and
	// .beads/beads.db-wal exceeds 1 MB.
	//
	// Bead ref: hk-5dewt.
	SkipWALCheckpoint bool

	// SkipBrHistoryRotation, when true, disables the advisory .br_history/
	// rotation pre-flight that runs at PL-005 step 0 immediately after the
	// WAL-checkpoint pre-flight.
	//
	// The pre-flight is non-fatal and transparent in production. This field
	// exists solely for unit tests that operate on temp directories where
	// history-dir presence/absence is deliberately controlled and a rotation
	// run would interfere with fixture state.
	//
	// Default (false): pre-flight runs when ProjectDir is set, keeping the 20
	// most-recent .br_history/ entries and archiving the rest.
	//
	// Bead ref: hk-5dewt.
	SkipBrHistoryRotation bool

	// SkipRestartBackoff, when true, disables the persistent boot-record
	// exponential backoff applied at startup when the daemon has been restarted
	// rapidly within the last hour.
	//
	// The backoff is non-fatal and transparent in production. This field
	// exists solely for unit tests that must start the daemon without incurring
	// an artificial delay.
	//
	// Default (false): backoff applies when ProjectDir is set and the
	// boot-record at <ProjectDir>/.harmonik/cognition/restart-record.json
	// contains recent boot times within the configured restart-backoff window.
	//
	// Bead ref: hk-7t9g1.
	SkipRestartBackoff bool

	// SkipBeadsMergeDriverConfig, when true, disables the beads-union git driver
	// auto-config pre-flight that runs at startup to register
	// merge.beads-union.{name,driver} in .git/config if absent.
	//
	// The pre-flight is non-fatal and transparent in production. This field
	// exists solely for unit tests that operate on temp directories without a
	// real git repository, where `git config --local` would fail.
	//
	// Bead ref: hk-r0y1o.
	SkipBeadsMergeDriverConfig bool

	// NoAutoPull, when true, disables the br-ready fallback poll path in the
	// work loop so the daemon only dispatches work that arrives via the queue
	// surface (harmonik queue submit / append).  When false (the default), the
	// work loop falls back to polling `br ready` whenever no queue is loaded,
	// preserving backward-compatible single-bead dispatch.
	//
	// Use this when the flywheel topology (CL-013/070/071) is active and a Pi
	// cognition loop drives dispatch via `harmonik queue append` — in that mode
	// the daemon must NOT self-seed from br ready because Pi controls dispatch
	// timing.  Without this flag the only workaround is to keep `br ready`
	// empty or pre-seed a paused queue, both of which are fragile.
	//
	// The composition root (cmd/harmonik/main.go) exposes this as --no-auto-pull.
	// The zero value (false) preserves the existing backward-compatible behaviour.
	//
	// Bead ref: hk-exd7m.
	NoAutoPull bool

	// QueueStore, when non-nil, is used directly instead of creating a fresh
	// QueueStore inside daemon.Start.  The caller retains the pointer and can
	// inspect queue status after Start returns (Fix 2 of hk-8jh26).
	//
	// The zero value (nil) is safe: daemon.Start creates its own QueueStore
	// as before.
	//
	// Bead ref: hk-8jh26.
	QueueStore *QueueStore

	// HandlerPauseController, when non-nil, is wired into the work loop to
	// enable the skip-on-paused dispatch gate (hk-kac8g).  When nil the gate
	// is disabled: all items are dispatched regardless of handler pause state.
	//
	// Production callers (cmd/harmonik/main.go) construct a controller and wire
	// it here so `harmonik handler pause` can trip the gate mid-run.
	// Unit tests that do not exercise handler-pause behaviour may leave this nil.
	//
	// Bead ref: hk-kac8g.
	HandlerPauseController *HandlerPauseController

	// NotifyStream, when non-nil, receives one line per bead completion event
	// as each bead's run_completed or run_failed event lands.
	//
	// Format: "[hk-XXX] success (commit abcdef)" or "[hk-XXX] failed (reason: ...)".
	// Lines are written in real time; the stream is not closed by the daemon.
	//
	// Production callers set this to os.Stdout (--notify-stream default) or to
	// an open FIFO/file (--notify-stream=path). When nil no per-bead lines are
	// written (backward-compatible default).
	//
	// Bead ref: hk-ibilr.
	NotifyStream io.Writer

	// ConflictResolutionAttemptCap is the operator-configurable cap on
	// conflict-resolution re-dispatch attempts per merge-pending cycle, per
	// specs/workspace-model.md §4.6.WM-024 and specs/operator-nfr.md §4.3.
	//
	// The workspace manager dispatches a fresh conflict-resolver LaunchSpec up
	// to this many times before routing to merge_conflict_escalation per WM-023.
	// Valid range: [1, 10]. Out-of-range values are rejected at daemon startup.
	//
	// The zero value is treated as the built-in default (3) per WM-024:
	// "DEFAULT of THREE (3) attempts per merge-pending cycle." Zero means "not
	// set by operator; use default" — this preserves backward-compatible
	// behaviour for daemon configurations that do not set the field.
	//
	// Spec refs: specs/workspace-model.md §4.6 WM-024; specs/operator-nfr.md §4.3.
	// Bead ref: hk-8mwo.36.
	ConflictResolutionAttemptCap int

	// ReconciliationScanCadence is the interval for the RC-020a scheduled
	// background detector scan (dispatch point (c)).
	//
	// The zero value falls back to ReconciliationScanCadenceDefault (1 hour)
	// per reconciliation/spec.md §4.3 RC-020a and the operator-nfr.md §4.3
	// knob reconciliation_scan_cadence. Negative values are treated as zero
	// (same fallback). Operators may set a shorter interval (e.g., 15 min) for
	// high-commit-rate workloads; post-MVH cadence tuning is tracked in OQ-RC-004.
	//
	// The field is immutable for the daemon's lifetime; a daemon restart is
	// required to apply changes (change-takes-effect: next daemon start per
	// operator-nfr.md §4.3).
	//
	// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a — "Scheduled cadence."
	// Spec ref: specs/operator-nfr/config-inventory.md §2.18 reconciliation_scan_cadence.
	// Bead ref: hk-63oh.21.
	ReconciliationScanCadence time.Duration

	// SubscriptionTokenCeiling is the operator-supplied per-5h token budget for
	// the Claude subscription shared across all projects.  When non-zero the
	// bandwidth tuner (hk-ymav1) reads rolling token usage from
	// ~/.claude/projects/*/*.jsonl every 60 s and adjusts the runtime concurrency
	// ceiling accordingly:
	//
	//   effectiveMax = clamp(round(N_max × (ceiling − used) / ceiling), 1, N_max)
	//
	// where N_max is MaxConcurrent and used is the sum of input + output +
	// cache_creation tokens over the trailing 5 h window.
	//
	// Zero (the default) disables the tuner: MaxConcurrent is used as-is.
	// Start with a conservative value and raise until a 429 fires; per-tier limits
	// are not publicly documented so empirical tuning is required.
	//
	// The composition root (cmd/harmonik/main.go) exposes this as
	// --subscription-token-ceiling.
	//
	// Bead ref: hk-ymav1.
	SubscriptionTokenCeiling int64

	// TargetBranch is the branch that the daemon merges completed bead branches
	// into.  When empty the daemon defaults to "main".
	//
	// The composition root exposes this as --target-branch.  The three beads that
	// thread this value into mergeRunBranchToMain (target-branch threading,
	// start_from retarget, post-merge build gate) must be dispatched serially and
	// are tracked under codename:productization.
	//
	// Bead ref: hk-mkxw1.
	TargetBranch string

	// ProtectBranches is the set of branch names the daemon must never merge
	// into or overwrite.  Branches named here are silently excluded from any
	// merge target consideration; an attempt to set TargetBranch to a protected
	// branch is rejected at startup with an error.
	//
	// The composition root exposes this as --protect-branch (repeatable).
	//
	// The zero value (nil) means no additional protection beyond the daemon's
	// built-in safeguards.
	//
	// Bead ref: hk-mkxw1.
	ProtectBranches []string

	// ForbidUnprotectedDefault, when true, causes the daemon to refuse to start
	// if the repository's default branch (typically "main" or "master") is not
	// listed in ProtectBranches.  This is a safety guard for multi-project
	// deployments where accidental merges to the default branch must be
	// prevented.
	//
	// When false (the default) the daemon starts normally regardless of whether
	// the default branch appears in ProtectBranches.
	//
	// The composition root exposes this as --forbid-default-main.
	//
	// Bead ref: hk-mkxw1.
	ForbidUnprotectedDefault bool

	// DefaultHarness is the tier-4 (global) default harness for the harness-
	// selection precedence chain (bead > queue > node > global).
	//
	// When non-empty it MUST be a valid core.AgentType per AR-025. The daemon
	// validates this at startup; an unrecognised value is a startup error.
	//
	// The zero value (empty string) falls back to the built-in default:
	// core.AgentTypeClaudeCode. This preserves backward-compatible behaviour for
	// all existing daemon configurations that do not set the field.
	//
	// The composition root exposes this as --default-harness.
	//
	// Bead ref: hk-y01k6 [C4/T4].
	DefaultHarness core.AgentType

	// CodexBinary is the path to the codex executable used when the resolved
	// harness is core.AgentTypeCodex.
	//
	// When empty the codex launch-spec builder defaults to the bare name "codex",
	// which is resolved by the PATH of the tmux window at spawn time. An absolute
	// path avoids PATH-resolution ambiguity in controlled deployments.
	//
	// The zero value (empty string) is safe: buildCodexLaunchSpec normalises it
	// to "codex".
	//
	// The composition root exposes this as --codex-binary.
	//
	// Bead ref: hk-y01k6 [C4/T4].
	CodexBinary string

	// Workers is the remote-worker registry loaded from .harmonik/workers.yaml at
	// daemon startup (remote-substrate B4). The zero value (empty Config) means
	// local execution only. CLI flag overrides applied by the composition root
	// (--worker-host, --worker-enabled) take precedence over file values per the
	// flag > file > default chain.
	//
	// Bead ref: hk-rs-b4-bootwire-b44z.
	Workers workers.Config

	// WorkerRegistryObserver, when non-nil, is invoked ONCE at work-loop startup
	// with the live *workers.Registry the tmux dispatch path reads (or nil when
	// no worker is configured — NFR7). The composition root uses it to late-bind
	// that SAME registry into the Codex driver's runner-selection seam (M4-C3),
	// so a worker-selected HARMONIK_SUBSTRATE=codexdriver run routes its codex
	// process over SSHRunner — WITHOUT the driver ever learning about workers
	// (RS-017 twin-blindness: selection stays at the wire/root, driver stays
	// blind). No-op for the tmux substrate (observer is nil there).
	WorkerRegistryObserver func(*workers.Registry)

	// Runner is the CommandRunner used for remote-aware marker-file reads on the
	// DOT run path (hk-hd2w6). At runtime, local runs set it to nil (NFR7:
	// byte-identical local path) and remote runs override it with rbc.sshRunner
	// at dispatch time in beadRunOne. This field is a test-injection seam: tests
	// supply a tmux.RecordingRunner via Config.Runner to capture Command calls and
	// assert that gate-verdict.json, auto_status.json, review.json, and budget-
	// sentinel reads are ALL routed through the runner, not bare os.*.
	//
	// The zero value (nil) is safe for production: all Via functions fall back to
	// the local-FS path when runner is nil per their nil-guard.
	//
	// Bead ref: hk-hd2w6.
	Runner ltmux.CommandRunner
}

// daemonTestHooks carries test-only injection points that are absent from the
// production Config surface.  The zero value is safe for production use: all
// fields are nil and the hooks are no-ops.
//
// Tests use StartForTesting (internal/daemon/testopts_test.go) to supply these
// hooks via functional options; production callers go through Start which always
// passes a zero-value daemonTestHooks.
//
// Bead ref: hk-j192n.
type daemonTestHooks struct {
	// busObserver, when non-nil, is called with the event bus immediately after
	// all pre-Seal subscriptions have been registered and before bus.Seal() is
	// called.  Mirrors the former Config.TestOnlyBusObserver.
	//
	// Bead ref: hk-37zy8.
	busObserver func(bus eventbus.EventBus)

	// brAdapterFactory, when non-nil, replaces brcli.NewForProject at all three
	// call sites in startWithHooks.  Mirrors the former Config.TestOnlyBrAdapterFactory.
	//
	// Spec ref: specs/beads-integration.md §4.10 BI-031b.
	// Bead ref: hk-th378.
	brAdapterFactory func(brPath, projectDir string) (*brcli.Adapter, error)

	// spendMeterObserver, when non-nil, is called with the newly constructed
	// DaemonSpendMeter immediately after it has been subscribed to the bus (but
	// before bus.Seal).  Tests use this to override the meter's caps (via
	// ExportedSpendMeterSetMaxRunsPerDay / ExportedSpendMeterSetDailyCapBytes) so
	// they can trip the meter with a small number of synthetic events.
	//
	// Bead ref: hk-c7lxc.
	spendMeterObserver func(*DaemonSpendMeter)

	// worktreeFactory, when non-nil, replaces productionWorktreeFactory in
	// beadRunOne.  Tests use this to inject a pre-committing factory that
	// satisfies the no-commit guard (hk-mmh8f) without requiring the handler
	// binary to make git commits, avoiding concurrent-merge races in tests
	// that exercise concurrent dispatch (TestScenario_ConcurrentMultiQueue_*).
	//
	// The zero value (nil) falls back to productionWorktreeFactory.
	worktreeFactory func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)

	// mergeQ, when set via WithMergeQueue, OVERRIDES the production merge queue
	// (RSM-015) so a test can share/inspect the exclusion domain across concurrent
	// beadRunOne goroutines. The injected queue MUST already be Start()ed by the
	// test. The zero value (nil) leaves production's own queue (created in
	// newWorkLoopDeps, started in runWorkLoop) in place.
	mergeQ *mergeq.Queue
}

// newBrAdapter constructs a *brcli.Adapter using hooks.brAdapterFactory when set
// (test mode) or brcli.NewForProject in production.
//
// Centralising the call avoids three duplicate factory-selection blocks across
// the three brcli.NewForProject call sites in startWithHooks (hk-th378).
func newBrAdapter(hooks daemonTestHooks, brPath, projectDir string) (*brcli.Adapter, error) {
	if hooks.brAdapterFactory != nil {
		return hooks.brAdapterFactory(brPath, projectDir)
	}
	return brcli.NewForProject(brPath, projectDir)
}

// newDaemonHookStore constructs the daemon hook-session store (a composition of
// the pure internal/hook state machine) and wires the bus emitter used by the
// rate-limit routing path (hk-lqtzq). Factored out of startWithHooks so the
// composition root stays a single call (M5 slice 1, internal/hook extraction).
func newDaemonHookStore(bus eventbus.EventBus) *hookSessionStore {
	store := newHookSessionStore()
	store.SetEmitter(bus)
	return store
}

// loadStartupQueues runs the PL-005 step 8a per-queue load: it constructs the
// br adapter, settles the SQLite ledger (F40), enumerates .harmonik/queues/ via
// LoadQueueAtStartup with QM-002a/QM-002b reconciliation, installs each queue,
// and issues a defensive Wake so the work loop unblocks promptly.
//
// Only runs when both ProjectDir and BrPath are set (production mode); unit-test
// callers that omit either skip cleanly (returns nil). A forward-incompatible
// schema_version returns a fatal error (exit code 2 per QM-002); a br-adapter
// construction failure is non-fatal (classified + emitted per BI-031b) and the
// daemon proceeds without a queue.
//
// Extracted from startWithHooks (M5 slice 1) to shave the composition-root
// cognit; behaviour is byte-identical to the pre-extraction inline block.
//
// Spec ref: specs/queue-model.md §3.2 QM-002, §3.2a QM-002a.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
// Bead ref: hk-tigaf.3.
func loadStartupQueues(ctx context.Context, cfg Config, hooks daemonTestHooks, bus eventbus.EventBus, qs *QueueStore, daemonStartTime time.Time) error {
	if cfg.ProjectDir == "" || cfg.BrPath == "" {
		return nil
	}

	brAdapterForQueue, brAdapterErr := newBrAdapter(hooks, cfg.BrPath, cfg.ProjectDir)
	if brAdapterErr != nil {
		// Classify + emit divergence_inconclusive for BrSchemaMismatch per
		// BI-031b.  Non-fatal: daemon proceeds without a queue; queue-* ops
		// return errors until a queue is submitted.
		//
		// Spec ref: specs/beads-integration.md §4.10 BI-031b.
		// Bead ref: hk-th378.
		_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brAdapterErr, "br-new-for-project-queue", bus)
		return nil
	}

	// F40 (hk-n2y): run `br sync --flush-only` before QM-002a/QM-002b
	// reconciliation to ensure the SQLite ledger is settled. After a daemon
	// restart the database may be transiently locked by the previous process,
	// causing every `br show` call to return exit 3 with empty stdout for the
	// first ~31 items. A flush-only sync forces a full database round-trip,
	// clearing the lock so the subsequent ShowBead queries succeed without
	// spurious warnings. Non-fatal: on sync failure the reconciliation continues
	// with the pre-F40 degraded behaviour (ShowBead failures are warned and skipped).
	if syncErr := brAdapterForQueue.SyncFlushOnly(ctx); syncErr != nil {
		logW := cfg.LogWriter
		if logW == nil {
			logW = os.Stderr
		}
		fmt.Fprintf(logW, "warn: daemon startup: br sync --flush-only failed; QM-002b ShowBead queries may emit transient exit-3 warnings: %v\n", syncErr) //nolint:errcheck // best-effort stderr warning
	}

	loadedQueues, loadErr := lifecycle.LoadQueueAtStartup(
		ctx,
		cfg.ProjectDir,
		brAdapterForQueue,
		bus,
		nil, // slog.Default() is used when nil
		&lifecycle.QM002bReapConfig{
			Resetter:      brAdapterForQueue,
			IntentLogDir:  lifecycle.BeadsIntentsDir(cfg.ProjectDir),
			ProjectHash:   lifecycle.ComputeProjectHash(cfg.ProjectDir),
			DaemonStartNS: daemonStartTime.UnixNano(),
		},
	)
	if loadErr != nil {
		// ErrQueueSchemaUnsupported → fatal (exit code 2 per QM-002).
		return fmt.Errorf("daemon.Start: queue load: %w", loadErr)
	}
	for _, lq := range loadedQueues {
		qs.SetQueue(lq)
	}
	// Explicit wake after all startup queues are installed so the workloop
	// unblocks immediately if it reaches workloopIdleWait before any
	// submit/append signal arrives (hk-ekj wake-gap fix). SetQueue above already
	// fires the channel for each loaded queue, but a coalesced signal may have
	// been consumed between iterations; a defensive Wake() here ensures at least
	// one signal is present when the workloop first runs.
	if len(loadedQueues) > 0 {
		qs.Wake()
	}
	return nil
}

// Start is the composition-root entry point for the harmonik daemon.
//
// ctx controls the lifetime of the work loop. The caller is responsible for
// cancelling ctx when a clean shutdown is desired (e.g. via
// signal.NotifyContext). This makes the stop mechanism testable without sending
// OS signals to the test process (hk-7oz2f).
//
// It executes the deterministic startup sequence defined by
// specs/process-lifecycle.md §4.2 PL-005:
//
// Step 1 (PL-002, hk-iarcy): acquire the advisory pidfile lock at
// <ProjectDir>/.harmonik/daemon.pid. Returns an error immediately if another
// daemon holds the lock (lifecycle.ErrPidfileLocked → exit code 5 per PL-008a).
// Pidfile is released on return via defer.
//
// Step 0 (PL-005 / hk-8mup.63):
//   - Instantiates the RedactionRegistry (handlercontract.NewRedactionRegistry)
//     per HC-032. No seed patterns are registered at this scope; handlers
//     register their own patterns when they land.
//   - Opens the JSONL event log at cfg.JSONLLogPath (when non-empty) per
//     EV-015 / EV-016 (hk-8mup.63).
//   - Instantiates the EventBus (eventbus.NewBusImplWithWriter) with the
//     registry and writer per EV-035, EV-016.
//
// daemon_started event (§8.7.1, hk-iarcy): emitted after the bus is
// constructed. Payload: started_at, pid, binary_commit_hash.
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020, PL-020a, PL-005 step 0;
// §4.1 PL-002, PL-002a, PL-002b.
func Start(ctx context.Context, cfg Config) error {
	return startWithHooks(ctx, cfg, daemonTestHooks{})
}

// startWithHooks is the implementation of Start.  Production callers use Start
// which passes a zero-value daemonTestHooks.  Test callers use StartForTesting
// (internal/daemon/testopts_test.go) which supplies non-nil hook fields.
//
// acquirePidfile performs PL-002 step 1: acquire the advisory pidfile lock at
// <ProjectDir>/.harmonik/daemon.pid (hk-iarcy). It returns the acquired
// *lifecycle.Pidfile so the CALLER owns the defer Release() — the lock must be
// held for the whole daemon lifetime, so a helper-scope defer would release it
// immediately (the same lifetime trap as jsonlWriter). Returns (nil, nil) when
// ProjectDir is empty (unit-test mode); pidfile acquisition is skipped.
//
// AcquirePidfile constructs the path internally as <ProjectDir>/.harmonik/daemon.pid
// (PL-002b). Extracted from startWithHooks (giant-retirement boot-config B2).
func acquirePidfile(cfg Config) (*lifecycle.Pidfile, error) {
	if cfg.ProjectDir == "" {
		return nil, nil
	}
	// mkdir-p <ProjectDir>/.harmonik/ so AcquirePidfile can open the file.
	harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(harmonikDir, 0o755); mkErr != nil {
		return nil, fmt.Errorf("daemon.Start: mkdir-p .harmonik: %w", mkErr)
	}

	pid := os.Getpid()
	pgid := syscall.Getpgrp()
	// Generate a UUIDv7 as the daemon instance ID (PL-005 step 0).
	instanceUID, uidErr := uuid.NewV7()
	if uidErr != nil {
		return nil, fmt.Errorf("daemon.Start: generate instance ID: %w", uidErr)
	}

	pidfile, acquireErr := lifecycle.AcquirePidfile(cfg.ProjectDir, pid, pgid, instanceUID.String())
	if acquireErr != nil {
		return nil, fmt.Errorf("daemon.Start: pidfile: %w", acquireErr)
	}
	return pidfile, nil
}

// resolveBootConfig performs PL-005 step 0 config resolution + validation and
// mutates cfg in place: it validates the workflow mode BEFORE any I/O (PL-004a,
// seam-2 ordering, hk-81n9r), loads + merges the branching defaults and runs the
// fail-closed branch-protection checks (WM-005b/hk-sul12) via the pure bootconfig
// seam, validates the conflict-resolution attempt cap (WM-024), and loads the
// cached project config (EM-012b). Returns the resolved workflow mode and target
// branch. Extracted from startWithHooks (giant-retirement boot-config).
func resolveBootConfig(cfg *Config) (core.WorkflowMode, string, error) {
	workflowModeDefault := cfg.WorkflowModeDefault
	if err := bootconfig.ValidateWorkflowMode(workflowModeDefault); err != nil {
		return "", "", fmt.Errorf("daemon.Start: %w", err)
	}

	bootIn := bootconfig.Input{
		WorkflowMode:             workflowModeDefault,
		FlagTargetBranch:         cfg.TargetBranch,
		FlagProtectBranches:      cfg.ProtectBranches,
		ForbidUnprotectedDefault: cfg.ForbidUnprotectedDefault,
	}
	if cfg.ProjectDir != "" {
		branchingDefaults, branchingErr := branching.Load(cfg.ProjectDir)
		if branchingErr != nil {
			return "", "", fmt.Errorf("daemon.Start: load .harmonik/branching.yaml: %w", branchingErr)
		}
		bootIn.YAMLLandsOn = branchingDefaults.LandsOn
		bootIn.YAMLProtectBranches = branchingDefaults.ProtectBranches
	}
	resolvedBoot, resolveErr := bootconfig.Resolve(bootIn)
	if resolveErr != nil {
		return "", "", fmt.Errorf("daemon.Start: %w", resolveErr)
	}
	cfg.TargetBranch = resolvedBoot.TargetBranch
	cfg.ProtectBranches = resolvedBoot.ProtectBranches

	// WM-024: validate ConflictResolutionAttemptCap (zero → built-in default 3;
	// a non-zero value MUST be in [1, 10]). Fail fast on a misconfiguration.
	if cfg.ConflictResolutionAttemptCap != 0 {
		if err := workspace.ValidateConflictResolutionAttemptCap(cfg.ConflictResolutionAttemptCap); err != nil {
			return "", "", fmt.Errorf("daemon.Start: invalid conflict_resolution_attempt_cap %d: %w", cfg.ConflictResolutionAttemptCap, err)
		}
	}

	// EM-012b tier-2: load + cache .harmonik/config.yaml. A parse/schema error is
	// fatal; a missing file is a zero-value ProjectConfig (hk-bfvk7).
	if cfg.ProjectDir != "" {
		projectCfg, loadErr := LoadProjectConfig(cfg.ProjectDir)
		if loadErr != nil {
			return "", "", fmt.Errorf("daemon.Start: load .harmonik/config.yaml: %w", loadErr)
		}
		cfg.ProjectCfg = projectCfg
	}

	return workflowModeDefault, resolvedBoot.TargetBranch, nil
}

// runBootPreflights runs the boot-time pre-flight maintenance steps (PL-005
// step 0 supporting work) and returns the restart-backoff delay. Each step
// self-guards on ProjectDir + its skip flag so unit-test mode short-circuits
// cleanly. The delay is NOT slept here (hk-uzvt9: the sleep belongs after the
// socket bind). Extracted from startWithHooks (giant-retirement boot-config B2).
func runBootPreflights(ctx context.Context, cfg Config) time.Duration {
	// WAL-checkpoint pre-flight (hk-5dewt): if .beads/beads.db-wal exists and
	// exceeds 1 MB, run PRAGMA wal_checkpoint(TRUNCATE) via sqlite3 before the
	// first br write. Non-fatal; no-op when sqlite3 is not on PATH. A failure is
	// logged and swallowed — it never blocks startup.
	if cfg.ProjectDir != "" && !cfg.SkipWALCheckpoint {
		if err := runWALCheckpointPreflight(ctx, cfg.ProjectDir); err != nil {
			logBootPreflightWarn("WAL checkpoint", err)
		}
	}

	// .br_history/ rotation pre-flight (hk-5dewt): archive all but the 20
	// most-recent snapshots so per-write scan cost stays sub-second. Non-fatal.
	if cfg.ProjectDir != "" && !cfg.SkipBrHistoryRotation {
		if err := runBrHistoryRotationPreflight(ctx, cfg.ProjectDir, brHistoryRotationDefaultKeep); err != nil {
			logBootPreflightWarn(".br_history rotation", err)
		}
	}

	// Restart-backoff pre-flight (hk-7t9g1): record the boot and compute an
	// exponentially-increasing delay when the daemon has been restarted rapidly.
	// applyBootBackoff only records + returns the duration; the sleep is deferred.
	var bootBackoffDelay time.Duration
	if cfg.ProjectDir != "" && !cfg.SkipRestartBackoff {
		bootBackoffDelay = applyBootBackoff(ctx, cfg.ProjectDir, cfg.ProjectCfg.Daemon.RestartBackoff)
	}

	// Beads-union driver auto-config pre-flight (hk-r0y1o): register
	// merge.beads-union.{name,driver} in .git/config once per clone. Non-fatal.
	if cfg.ProjectDir != "" && !cfg.SkipBeadsMergeDriverConfig {
		ensureBeadsMergeDriver(ctx, cfg.ProjectDir)
	}

	return bootBackoffDelay
}

// logBootPreflightWarn writes a best-effort warning for a non-fatal boot
// pre-flight failure via the default logger, matching the structured-warning
// idiom used elsewhere in startup (e.g. the event-ID HWM warnings). Pre-flight
// errors never block boot.
func logBootPreflightWarn(step string, err error) {
	log.Printf("warn: daemon startup: %s pre-flight failed (non-fatal): %v", step, err)
}

// Bead ref: hk-j192n.
func startWithHooks(ctx context.Context, cfg Config, hooks daemonTestHooks) error {
	// Step 1 (PL-002, hk-iarcy): acquire the advisory pidfile lock.
	//
	// AcquirePidfile constructs the path internally as
	// <ProjectDir>/.harmonik/daemon.pid (PL-002b). The bead body described a
	// path under .harmonik/run/; the actual lifecycle.AcquirePidfile API uses
	// .harmonik/daemon.pid — the code wins per implementer-protocol §Path-discrepancy.
	// Follow-up: patch bead body / spec cross-ref for the .harmonik/run/ path form.
	//
	// Skip pidfile acquisition when ProjectDir is empty (unit-test mode).
	// The outer shell owns the defer so the lock is held for the whole daemon
	// lifetime (a defer inside acquirePidfile would release it immediately).
	pidfile, pidErr := acquirePidfile(cfg)
	if pidErr != nil {
		return pidErr
	}
	if pidfile != nil {
		defer func() {
			if relErr := pidfile.Release(); relErr != nil {
				log.Printf("warn: daemon.Start: pidfile release: %v", relErr)
			}
		}()
	}

	// Step 0 (PL-005): resolve + validate cross-subsystem boot config — workflow
	// mode (PL-004a), branching defaults + fail-closed branch protection
	// (WM-005b/hk-sul12), the conflict-cap (WM-024), and the cached project config
	// (EM-012b). Mutates cfg (TargetBranch/ProtectBranches/ProjectCfg) and returns
	// the resolved mode + target branch. Extracted for giant-retirement boot-config.
	workflowModeDefault, resolvedTargetBranch, cfgErr := resolveBootConfig(&cfg)
	if cfgErr != nil {
		return cfgErr
	}

	// Boot pre-flight maintenance (WAL checkpoint, .br_history rotation,
	// restart-backoff record, beads-union merge-driver config). Each step
	// self-guards on ProjectDir + its skip flag. Returns the restart-backoff
	// delay, which is NOT slept here — the actual sleep happens later, via
	// sleepBootBackoff, AFTER the socket-bind block (hk-uzvt9).
	bootBackoffDelay := runBootPreflights(ctx, cfg)

	// PL-005 step 0: construct the event bus + core registries (P4), then wire
	// every pre-Seal subscriber (P5). Split into constructBusAndRegistries plus
	// two subscriber-wiring helpers so each stays under the funlen/cyclop
	// ceilings; shared singletons thread through bootState. EV-009: every
	// Subscribe MUST run before bus.Seal() (kept in this shell, below).
	//
	// seam-1: constructBusAndRegistries RETURNS the JSONL writer so the OUTER
	// shell owns defer Close — a helper-scope defer would close the event log
	// before the work loop runs.
	bs := &bootState{cfg: cfg, hooks: hooks}
	jsonlWriter, busErr := bs.constructBusAndRegistries()
	if busErr != nil {
		return busErr
	}
	if jsonlWriter != nil {
		defer func() {
			if closeErr := jsonlWriter.Close(); closeErr != nil {
				log.Printf("warn: daemon.Start: JSONL writer close: %v", closeErr)
			}
		}()
	}
	if wireErr := bs.wireSpendAndQueueConsumers(); wireErr != nil {
		return wireErr
	}
	if wireErr := bs.wireWatchersAndObservers(ctx); wireErr != nil {
		return wireErr
	}

	// Local aliases for the shared singletons the residual shell (P6 seal +
	// startup events) still reads directly under their historical names.
	bus := bs.bus
	clockRegressionDetected := bs.clockRegressionDetected

	if sealErr := bus.Seal(); sealErr != nil {
		return fmt.Errorf("daemon.Start: seal bus: %w", sealErr)
	}

	// P6: post-Seal startup events (clock-regression degraded, stale-watch start,
	// daemon_started F-class landmark, supervisor-revival scan, daemon_config).
	// Returns the daemon start time threaded into the later phases. Extracted for
	// giant-retirement boot-config (B6 complexity reduction).
	daemonStartTime, startupErr := bs.emitStartupEvents(ctx, clockRegressionDetected, resolvedTargetBranch)
	if startupErr != nil {
		return startupErr
	}

	// Step 3 (PL-005 / PL-006, hk-60uvn): orphan sweep + in-flight-run reconcile,
	// BEFORE any socket or listener bind. Extracted into runStartupReconcile (and
	// three sub-helpers) for giant-retirement boot-config B4. Holds the single
	// ProjectDir guard internally; the only fatal path is the BI-024a br --version
	// handshake (exit code 8). Runs before loadStartupQueues (QM-002a ordering).
	if reconcileErr := bs.runStartupReconcile(ctx, daemonStartTime, resolvedTargetBranch); reconcileErr != nil {
		return reconcileErr
	}

	// hk-9ptu: proactive keepalive for the daemon-owned spawn-target session.
	//
	// On supervisor-revive (DaemonWatchdog path), the daemon falls back to the
	// deterministic "harmonik-<hash>-default" session (needEnsureSession=true in
	// main.go) and marks the substrate with WithSessionKeepalive.  A background
	// goroutine then periodically calls EnsureSession so the session is recreated
	// if it is killed externally between dispatches — complementing the reactive
	// hk-yaj self-heal in SpawnWindow that only fires when a SpawnWindow call
	// actually hits ErrNoSession.
	//
	// For the normal "live ambient session" path (needEnsureSession=false in
	// main.go) WithSessionKeepalive is NOT passed, so keepaliveEnabled=false and
	// RunSessionKeepalive returns immediately (no-op goroutine).
	if sk, ok := cfg.Substrate.(substrateWithKeepalive); ok {
		go sk.RunSessionKeepalive(ctx)
	}

	// PL-005 step 4 / step 8a + PL-003 (P9-P11): register adapters + hook store,
	// load persisted startup state (queues, handler-pause, decision-acks), and
	// bind the socket listener. Extracted into wireSocketListener + sub-helpers
	// for giant-retirement boot-config B5; the persistent singletons thread
	// through bootState for the work loop (P13).
	if socketErr := bs.wireSocketListener(ctx, daemonStartTime); socketErr != nil {
		return socketErr
	}

	// hk-uzvt9: apply the restart-backoff sleep computed above (bootBackoffDelay)
	// only now, AFTER the socket has been bound (or its bind goroutine started).
	// applyBootBackoff previously slept synchronously at ~L817, BEFORE the socket
	// block — a 30s/60s backoff delay blocked bind long enough for the
	// supervisor's 30s health-window to see no socket and revert to last-good
	// under rapid restart. The backoff throttles dispatch, not liveness, so the
	// sleep belongs after bind. sleepBootBackoff is a no-op when the delay is 0.
	sleepBootBackoff(ctx, bootBackoffDelay)

	// PL-005 step 4 (P13): build + inject the work-loop deps, start the background
	// loops, wire the StaleWatcher force-reap seams, then run the work loop and
	// block until ctx cancels or it exits. Skipped when BrPath is unset (unit-test
	// mode). Extracted into launchWorkLoop for giant-retirement boot-config B6.
	return bs.launchWorkLoop(ctx, daemonStartTime, bootBackoffDelay, workflowModeDefault)
}
