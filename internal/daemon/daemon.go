package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/agentmanifest"
	"github.com/gregberns/harmonik/internal/branching"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sentinel"
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
		defer func() { _ = pidfile.Release() }()
	}

	// Step 0 (PL-005): bootstrap cross-subsystem registries.

	// PL-004a: resolve and cache workflow_mode_default once at step 0.
	//
	// The zero value (empty string) is now a startup error (fail-closed per
	// hk-81n9r). Callers must set an explicit mode; use core.WorkflowModeDot
	// for the recommended default. Any unrecognised non-empty value is also
	// rejected so the daemon fails fast rather than silently using a wrong mode.
	//
	// Bead ref: hk-7om2q.8, hk-81n9r.
	//
	// Ordering (seam-2, load-bearing): ValidateWorkflowMode runs BEFORE
	// branching.Load below, so the empty-mode misconfig (hk-81n9r)
	// short-circuits before any I/O, byte-identical to the pre-seam block.
	workflowModeDefault := cfg.WorkflowModeDefault
	if err := bootconfig.ValidateWorkflowMode(workflowModeDefault); err != nil {
		return fmt.Errorf("daemon.Start: %w", err)
	}

	// WM-005b / hk-sul12: resolve project-level branching defaults from
	// .harmonik/branching.yaml (precedence CLI flag > branching.yaml > built-in),
	// resolve the target branch ("" → "main"), and run the fail-closed
	// branch-protection checks (two hard-error cases before any socket bind). The
	// pure resolution lives in the bootconfig sub-package; the daemon performs
	// only the branching.Load I/O here and threads the loaded values in.
	//
	// Bead ref: hk-zl4sl, hk-sul12.
	bootIn := bootconfig.Input{
		WorkflowMode:             workflowModeDefault,
		FlagTargetBranch:         cfg.TargetBranch,
		FlagProtectBranches:      cfg.ProtectBranches,
		ForbidUnprotectedDefault: cfg.ForbidUnprotectedDefault,
	}
	if cfg.ProjectDir != "" {
		branchingDefaults, branchingErr := branching.Load(cfg.ProjectDir)
		if branchingErr != nil {
			return fmt.Errorf("daemon.Start: load .harmonik/branching.yaml: %w", branchingErr)
		}
		bootIn.YAMLLandsOn = branchingDefaults.LandsOn
		bootIn.YAMLProtectBranches = branchingDefaults.ProtectBranches
	}
	resolvedBoot, resolveErr := bootconfig.Resolve(bootIn)
	if resolveErr != nil {
		return fmt.Errorf("daemon.Start: %w", resolveErr)
	}
	cfg.TargetBranch = resolvedBoot.TargetBranch
	cfg.ProtectBranches = resolvedBoot.ProtectBranches
	resolvedTargetBranch := resolvedBoot.TargetBranch

	// WM-024: validate ConflictResolutionAttemptCap at startup.
	//
	// The zero value is treated as the built-in default (3) — operators who do
	// not set the field get three attempts per merge-pending cycle (WM-024).
	// A non-zero value MUST be in [1, 10]; values outside this range are
	// rejected here so the daemon fails fast rather than silently misconfiguring
	// the workspace manager (operator-nfr.md §4.3).
	//
	// Bead ref: hk-8mwo.36.
	if cfg.ConflictResolutionAttemptCap != 0 {
		if err := workspace.ValidateConflictResolutionAttemptCap(cfg.ConflictResolutionAttemptCap); err != nil {
			return fmt.Errorf("daemon.Start: invalid conflict_resolution_attempt_cap %d: %w", cfg.ConflictResolutionAttemptCap, err)
		}
	}

	// EM-012b tier-2: load .harmonik/config.yaml once at startup and cache in cfg.
	// A parse error or unsupported schema_version causes the daemon to refuse to start
	// (loud failure per bead spec; operators must fix the config before restarting).
	// A missing file is silently treated as "no project config" (zero-value ProjectConfig).
	//
	// Bead ref: hk-bfvk7.
	if cfg.ProjectDir != "" {
		projectCfg, cfgErr := LoadProjectConfig(cfg.ProjectDir)
		if cfgErr != nil {
			return fmt.Errorf("daemon.Start: load .harmonik/config.yaml: %w", cfgErr)
		}
		cfg.ProjectCfg = projectCfg
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
		defer func() { _ = jsonlWriter.Close() }()
	}
	if wireErr := bs.wireSpendAndQueueConsumers(); wireErr != nil {
		return wireErr
	}
	if wireErr := bs.wireWatchersAndObservers(ctx); wireErr != nil {
		return wireErr
	}

	// Local aliases so the remaining composition-root phases (P6-P13, extracted
	// in later slices) read the shared singletons under their historical names.
	bus := bs.bus
	qs := bs.qs
	handlerPauseCtrl := bs.handlerPauseCtrl
	sharedRunRegistry := bs.sharedRunRegistry
	staleWatcher := bs.staleWatcher
	quiesceArbiter := bs.quiesceArbiter
	subscribeHub := bs.subscribeHub
	pollGate := bs.pollGate
	tunerBackstop := bs.tunerBackstop
	clockRegressionDetected := bs.clockRegressionDetected

	if sealErr := bus.Seal(); sealErr != nil {
		return fmt.Errorf("daemon.Start: seal bus: %w", sealErr)
	}

	// EV-002c: emit daemon_degraded{reason=clock_regression} when the wall
	// clock was behind the persisted HWM by more than 1 second at startup.
	// Non-fatal: the generator already synthesises IDs ahead of the clock;
	// this event is an observability signal for operators.
	if clockRegressionDetected {
		degradedPayload := core.DaemonDegradedPayload{
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:     core.DaemonDegradedReasonClockRegression,
		}
		if degradedBytes, marshalErr := json.Marshal(degradedPayload); marshalErr == nil {
			_ = bus.Emit(context.Background(), core.EventTypeDaemonDegraded, degradedBytes)
		}
	}

	// Start the stale-watch background goroutine after Seal so the bus is in
	// live-delivery mode (EV-009 sealed bus semantics). The goroutine runs until
	// ctx is cancelled. Non-fatal: if context is already cancelled the goroutine
	// exits immediately.
	//
	// Bead ref: hk-wkzlc.
	staleWatcher.StartWatcher(ctx)

	// Emit daemon_started (§8.7.1, hk-iarcy): F-class event marking the
	// startup landmark for post-crash-window detection (EV-023).
	// binary_commit_hash: forwarded from cfg.BinaryCommitHash which is stamped
	// at build time via -ldflags "-X main.commitHash=<sha>" (hk-mz0x4).
	// Falls back to "unknown" when the caller does not set the field (unit tests,
	// unstamped builds) to keep the envelope well-formed per the spec.
	binaryCommitHash := cfg.BinaryCommitHash
	if binaryCommitHash == "" {
		binaryCommitHash = "unknown"
	}
	daemonStartTime := time.Now().UTC()
	startedPayload := core.DaemonStartedPayload{
		StartedAt:        daemonStartTime.Format(time.RFC3339),
		PID:              os.Getpid(),
		BinaryCommitHash: binaryCommitHash,
	}
	payloadBytes, marshalErr := json.Marshal(startedPayload)
	if marshalErr != nil {
		return fmt.Errorf("daemon.Start: marshal daemon_started payload: %w", marshalErr)
	}
	if emitErr := bus.Emit(context.Background(), core.EventTypeDaemonStarted, payloadBytes); emitErr != nil {
		return fmt.Errorf("daemon.Start: emit daemon_started: %w", emitErr)
	}

	// hk-rnkuy: emit supervisor_revival when the prior daemon session lacked a
	// daemon_shutdown event (SIGKILL / OOM / panic). daemon_started is F-class
	// (fsynced above) so the current session is already in the log before we scan.
	// Non-fatal: startup continues regardless of detection or emit outcome.
	if cfg.JSONLLogPath != "" {
		detectAndEmitSupervisorRevival(context.Background(), cfg.JSONLLogPath, bus)
	}

	// hk-sul12: emit daemon_config stating the resolved merge-target and active
	// branch-protection policy. Emitted immediately after daemon_started so the
	// resolved config is observable before any dispatch work begins.
	// hk-mptxw (F8): also serialise workflow_mode, max_concurrent, no_auto_pull
	// so config drift across daemon restarts is visible in the event log.
	// Non-fatal: a marshal or emit failure does not block startup.
	if cfgPayload := (core.DaemonConfigPayload{
		TargetBranch:             resolvedTargetBranch,
		ProtectBranches:          cfg.ProtectBranches,
		ForbidUnprotectedDefault: cfg.ForbidUnprotectedDefault,
		WorkflowMode:             string(cfg.WorkflowModeDefault),
		MaxConcurrent:            cfg.MaxConcurrent,
		NoAutoPull:               cfg.NoAutoPull,
	}); cfgPayload.Valid() {
		if cfgBytes, cfgMarshalErr := json.Marshal(cfgPayload); cfgMarshalErr == nil {
			_ = bus.Emit(context.Background(), core.EventTypeDaemonConfig, cfgBytes)
		}
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

	// Step 4 (hk-ecrxy): register adapters and launch the work loop.
	//
	// AdapterRegistry: construct, register ClaudeCodeAdapter for core.AgentTypeClaudeCode,
	// seal.  The sealed registry is forwarded into handler.NewHandler as a latent
	// seam for post-MVH adapter-selection (hk-gql20.16).  Construct and seal here
	// to satisfy PL-020a composition-root ordering.
	adapterReg := handlercontract.NewAdapterRegistry()
	if regErr := handler.Register(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register ClaudeCodeAdapter: %w", regErr)
	}
	if regErr := handler.RegisterCodex(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register CodexAdapter: %w", regErr)
	}
	if regErr := handler.RegisterPi(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register PiAdapter: %w", regErr)
	}
	// Seal the registry: no further adapters.
	// The first ForAgent call would seal it anyway; explicit seal here makes the
	// ordering contract observable.
	claudeCodeAdapter, forAgentErr := adapterReg.ForAgent(core.AgentTypeClaudeCode)
	if forAgentErr != nil {
		// ForAgent only fails if no adapter is registered — that would be a bug
		// in the Register call above; treat as fatal.
		return fmt.Errorf("daemon.Start: seal adapter registry: %w", forAgentErr)
	}

	// HC-014a: inject the ClaudeCode adapter into the handler-pause controller
	// so it can call Diagnose on pause-trip and Resume.
	//
	// SetAdapter is called after the registry is sealed and before any event
	// consumers fire (bus is not yet sealed at this point).
	//
	// Spec: specs/handler-contract.md §4.3a HC-014a.  Bead: hk-tvsl7.
	handlerPauseCtrl.SetAdapter(claudeCodeAdapter)

	// Construct the hook-session store once at the composition root (hk-gql20.21).
	// The same instance is forwarded to RunSocketListener (as HookRelayHandler)
	// and into workLoopDeps so the work loop can call WaitForOutcome in the
	// completion path (hk-gql20.22).
	//
	// Spec ref: specs/claude-hook-bridge.md §4.10 CHB-025.
	//
	// The pure session-store state machine lives in internal/hook; newDaemonHookStore
	// composes it with the bus emitter used by the rate-limit routing path
	// (hk-lqtzq). bus.Seal has already been called at this point; the emitter is
	// used for Emit calls (delivery, not subscription), which are valid post-Seal.
	hookStore := newDaemonHookStore(bus)

	// PL-005 step 8a (QM-002 / QM-002a): load per-queue files at startup BEFORE
	// the socket listener or work loop start.  LoadQueueAtStartup first runs the
	// NQ-A2 legacy migration (.harmonik/queue.json → .harmonik/queues/main.json),
	// then enumerates .harmonik/queues/ and loads each queue with QM-002a + QM-002b
	// reconciliation.  Only runs when both ProjectDir and BrPath are set
	// (production mode); unit-test callers that omit one or both skip cleanly.
	//
	// A forward-incompatible schema_version causes a fatal return with exit-code-2
	// semantics per QM-002.  Corrupt but parseable files produce a warning and are
	// skipped (daemon proceeds without that queue).
	//
	// Spec ref: specs/queue-model.md §3.2 QM-002, §3.2a QM-002a.
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
	// Bead ref: hk-tigaf.3.
	if loadErr := loadStartupQueues(ctx, cfg, hooks, bus, qs, daemonStartTime); loadErr != nil {
		return loadErr
	}

	// PL-005 step 8a (hk-m0k0a): wire the persistence function into the
	// HandlerPauseController (constructed pre-Seal above) and load any persisted
	// handler state from .harmonik/handler-state.json.
	//
	// The controller was constructed above (pre-Seal) with a nil persistFn so
	// that HandlerPausePolicyGoroutine.Subscribe could reference it before Seal.
	// Here we patch in the real persistFn (when ProjectDir is set) and then seed
	// the controller from disk.
	//
	// LoadHandlerPauseState seeds the controller with any paused handlers that
	// survived the last daemon run, ensuring "paused status MUST persist across
	// restarts" per specs/handler-pause.md §8.3 HP-008 (QM-055 analog).
	//
	// A forward-incompatible schema_version causes a fatal return (exit code 2).
	// File absent → all handlers default live (no-op).
	//
	// Spec ref: specs/handler-pause.md §3.5.
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
	// Bead ref: hk-m0k0a, hk-37zy8.
	if cfg.ProjectDir != "" {
		harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
		handlerPauseCtrl.SetPersistFn(MakeHandlerPausePersistFn(harmonikDir))
		if loadErr := LoadHandlerPauseState(context.Background(), harmonikDir, handlerPauseCtrl); loadErr != nil {
			return fmt.Errorf("daemon.Start: handler-state.json load: %w", loadErr)
		}
	}

	// decisionBlocker is the daemon-singleton DecisionBlocker populated by
	// LoadDecisionAckState (EV-043a).  Declared here so the socket listener
	// (future: decision-ack handler) and the workloop share the same instance.
	//
	// Spec ref: specs/event-model.md §4.12 EV-043a.
	// Bead ref: hk-pbmsq.
	decisionBlocker := NewDecisionBlocker()
	if cfg.ProjectDir != "" {
		if loadErr := LoadDecisionAckState(context.Background(), cfg.ProjectDir, decisionBlocker); loadErr != nil {
			return fmt.Errorf("daemon.Start: decision_acks load: %w", loadErr)
		}
	}

	// opPauseCtrl is the daemon-singleton OperatorPauseController. Constructed
	// inside the ProjectDir block (where the bus is available and Sealed) and
	// consumed by both the socket listener (handles operator-pause/resume ops)
	// and the workloop (br-ready dispatch gate). Declared here so both blocks
	// share the same variable scope.
	//
	// Bead ref: hk-ry8q1.
	var opPauseCtrl *OperatorPauseController

	// concurrencyCtrl is the daemon-singleton ConcurrencyController. Initialised
	// inside the ProjectDir block and injected into both the HandlerAdapter (so
	// queue-set-concurrency RPCs can update the ceiling) and the workloop deps (so
	// the dispatch gate reads the live value on every tick). Declared here so
	// both blocks share the same variable scope.
	//
	// Bead ref: hk-ohiaf.
	var concurrencyCtrl *ConcurrencyController

	// queueHandlerAdapter holds the concrete *queue.HandlerAdapter (when one was
	// constructed in the socket block) so the work-loop block can wire the live
	// worker-toggle func into it once deps.workerRegistry exists (hk-xjbvi). The
	// registry is built inside newWorkLoopDeps (after the socket block), so unlike
	// the concurrency setter — which is wired pre-listener — the worker toggle is
	// wired just after deps is built; a worker-set-enabled RPC that races the few
	// microseconds before that gets a clean "no worker registry wired" error and
	// is retried, never a panic. Nil in unit-test mode (no socket / no adapter).
	var queueHandlerAdapter *queue.HandlerAdapter

	// drainDet is the daemon-singleton DrainDetector. Constructed inside the
	// ProjectDir/BrPath block and reused by both the quiesce arbiter (P1-c)
	// and the state handler (hk-gv04 P2-a). Nil in unit-test mode.
	var drainDet *DrainDetector

	// crewHandler is the daemon-singleton crew-start/stop handler. Constructed
	// inside the ProjectDir block (for the socket listener) and also injected into
	// the workloop deps so the schedule tick can fire spawn-crew actions through
	// the same HandleCrewStart path (codename:schedule, hk-0es). Declared here so
	// both blocks share scope; nil in unit-test mode (no ProjectDir / socket).
	var crewHandler CrewHandler

	// crewIdleReaper (SD-3, hk-s2eac): tears down a crew whose bound queue has
	// completed and stayed idle past a short grace window, reclaiming its
	// slot. Constructed alongside crewHandler (needs it as the stop seam);
	// started post-Seal beside quiesceArbiter.Start. Nil in unit-test mode
	// (no ProjectDir / socket).
	var crewIdleReaper *CrewIdleReaper

	// branchReapWatcher (hk-2i36s, follow-up to hk-fpjxi): periodically reaps
	// merged/orphaned run/* and worktree-agent-* branches so the on-demand
	// `harmonik gc branches` tool isn't the only caller. Constructed alongside
	// crewIdleReaper; started post-Seal beside quiesceArbiter.Start. Nil in
	// unit-test mode (no ProjectDir).
	var branchReapWatcher *BranchReapWatcher

	// PL-003 / CHB-025 (hk-tjl40): bind the Unix-domain socket so hook-relay
	// subprocesses can deliver outcome_emitted envelopes to the daemon.
	//
	// Only bind when ProjectDir is set; unit-test callers that omit ProjectDir
	// skip the socket (no path to bind). The socket listener runs concurrently
	// with the work loop and shuts down on the same ctx.
	//
	// QueueHandler: queue.NewHandlerAdapter wired when BrPath is set. A nil
	// QueueHandler causes all queue-* ops to return -32099 (no queue loaded).
	//
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 3a; §4.1 PL-003.
	if cfg.ProjectDir != "" {
		sockPath := filepath.Join(cfg.ProjectDir, ".harmonik", "daemon.sock")
		// hk-ta6dg: a too-long sockPath is a PERMANENT bind failure (unlike the
		// transient stale-socket race removeStaleSocket already handles), and it
		// silently defeats the reverse-tunnel readiness gate for remote runs (the
		// gate only probes the worker-side TCP listener, never this local path —
		// see internal/lifecycle/socketpathlimit.go). PL-003 deliberately keeps
		// socket-bind errors non-fatal to daemon.Start (many callers/tests rely on
		// that, including short-lived test harnesses whose t.TempDir() path is
		// itself long enough to trip this on macOS), so this is a loud diagnostic,
		// not an abort — the reverse-tunnel call site is where a too-long path
		// actually blocks a run, and that path fails loud per-run.
		if lenErr := lifecycle.ValidateSocketPathLength(sockPath); lenErr != nil {
			fmt.Fprintf(os.Stderr, "daemon.Start: %v\n", lenErr)
		}
		// .harmonik/ was already created above (pidfile block), but when
		// ProjectDir is set with BrPath="" (test mode skipping pidfile) we still
		// need the dir. MkdirAll is idempotent.
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(filepath.Dir(sockPath), 0o755); mkErr != nil {
			return fmt.Errorf("daemon.Start: mkdir-p .harmonik (socket): %w", mkErr)
		}

		// Construct the QueueHandler adapter. Nil when BrPath is unset (unit-test
		// mode); RunSocketListener accepts nil and returns -32099 for queue-* ops.
		// qs and bus are threaded in so the adapter can update the in-memory
		// QueueStore and emit events after each persist (hk-4ukkq, hk-lzs8r,
		// hk-peucr).
		var queueHandler QueueHandler
		if cfg.BrPath != "" {
			brAdapterForHandler, brHandlerErr := newBrAdapter(hooks, cfg.BrPath, cfg.ProjectDir)
			if brHandlerErr != nil {
				// Classify + emit divergence_inconclusive for BrSchemaMismatch per
				// BI-031b.  Non-fatal: socket handler proceeds without queue support;
				// queue-* ops return errors until a queue is submitted.
				//
				// Spec ref: specs/beads-integration.md §4.10 BI-031b.
				// Bead ref: hk-th378.
				_ = brcli.BrErrReconciliationCategoryWithEmit(context.Background(), brHandlerErr, "br-new-for-project-handler", bus)
			} else {
				adapter := queue.NewHandlerAdapter(newBRQueueLedger(brAdapterForHandler), cfg.ProjectDir, qs, bus)
				// Wire the global --max-concurrent so submit can default a queue's
				// per-queue Workers count (QM-066) and warn on oversubscription
				// (hk-tigaf.4 NQ-B1). cfg.MaxConcurrent zero → 1 inside the adapter.
				adapter.SetGlobalMaxConcurrent(cfg.MaxConcurrent)
				queueHandler = adapter
				// Retain the concrete adapter so the work-loop block can wire the
				// live worker-toggle func once deps.workerRegistry exists (hk-xjbvi).
				queueHandlerAdapter = adapter

				// Wire the SS-INV-005 veto gate into the quiesce arbiter (P1-c,
				// hk-zqb3): non-force `harmonik sleep` is refused when GatherDrainFacts
				// reports dispatchable or in-flight work that would be stranded.
				drainDet = NewDrainDetector(brAdapterForHandler, brAdapterForHandler, newBRQueueLedger(brAdapterForHandler), sharedRunRegistry, qs, cfg.ProjectDir)
				quiesceArbiter.SetDrain(drainDet)
			}
		}

		// Construct the OperatorPauseController so the socket listener can handle
		// operator-pause / operator-resume ops (hk-ry8q1). The controller emits
		// lifecycle events on the bus; must be constructed after Seal so the bus
		// is ready to deliver events.
		//
		// Bead ref: hk-ry8q1.
		opPauseCtrl = NewOperatorPauseController(bus)

		// Create the ConcurrencyController and wire it into the HandlerAdapter so
		// queue-set-concurrency RPCs can update the ceiling at runtime (hk-ohiaf).
		concurrencyCtrl = NewConcurrencyController(cfg.MaxConcurrent)
		if ha, ok := queueHandler.(*queue.HandlerAdapter); ok {
			ha.SetConcurrencyFuncs(concurrencyCtrl.Get, concurrencyCtrl.Set)
			// hk-vfeeo: wire spawn cap so set-concurrency can detect requests
			// that would oversubscribe the substrate's session ceiling.
			if ss, ok := cfg.Substrate.(substrateWithSpawnCap); ok {
				ha.SetSpawnCapFunc(ss.SpawnCapSize)
			}
			// hk-omvan: wire the live spawn-cap resize setter (when the
			// substrate supports it) so set-concurrency RAISES the cap to
			// satisfy an oversubscribing request instead of refusing it.
			if ss, ok := cfg.Substrate.(substrateWithSpawnCapSetter); ok {
				ha.SetSpawnCapSetFunc(ss.SetSpawnCap)
			}
		}

		// Start the bandwidth tuner when --subscription-token-ceiling is set
		// (hk-ymav1).  The tuner reads rolling 5h token usage from Claude Code
		// transcripts and adjusts concurrencyCtrl on every 60s tick.
		// normalised MaxConcurrent (zero → 1) is used as the N_max ceiling so the
		// tuner and the static gate share the same scale.
		if cfg.SubscriptionTokenCeiling > 0 {
			maxN := cfg.MaxConcurrent
			if maxN <= 0 {
				maxN = 1
			}
			homeDir, homeDirErr := os.UserHomeDir()
			if homeDirErr == nil {
				tuner := NewBandwidthTuner(concurrencyCtrl, maxN, cfg.SubscriptionTokenCeiling, homeDir)
				tuner.SetGate(pollGate)       // SS-007: OFF at INACTIVE (hk-w6q7)
				tunerBackstop.SetTuner(tuner) // arm the pre-Seal backstop subscriber
				go tuner.Run(ctx)
			}
		}

		// commsSendHandler emits agent_message events on behalf of CLI callers.
		// NewCommsSendHandler returns nil if bus does not implement CommsMessageEmitter
		// (e.g. test stubs), in which case comms-send ops return an error response.
		// Bead ref: hk-nbrmf (comms-send T4).
		commsSendHandler := NewCommsSendHandler(bus)
		// Wire comms-recv deps (T8, hk-nnwaa): two INDEPENDENT cursor stores +
		// events JSONL path. SetRecvDeps is a no-op when commsSendHandler is nil
		// (bus stub case).
		//
		// hk-8xspi (B1): a plain one-shot `comms recv --agent` poll and a
		// `--follow`/`--wait` live session no longer share a cursor — draining
		// one never advances the other, so a poller can no longer be starved by
		// a follow/wait watcher's consumption (or vice versa). The live store is
		// shared between the handler's catch-up drain (CommsRecvRequest.Live)
		// and the SubscribeHub (hk-tafd4) so a follow/wait session's drain and
		// its live tail stay on one continuous cursor. Separate on-disk
		// directories keep the two cursor namespaces from ever colliding.
		if impl, ok := commsSendHandler.(*commsSendHandlerImpl); ok && cfg.ProjectDir != "" {
			pollCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors")
			liveCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors-live")
			pollCursorStore := NewCursorStore(pollCursorDir)
			liveCursorStore := NewCursorStore(liveCursorDir)
			impl.SetRecvDeps(pollCursorStore, liveCursorStore, cfg.JSONLLogPath)
			subscribeHub.SetCommsCursorStore(liveCursorStore)
		}

		// Construct the C2 crew-start/stop handler (c2-spec.md §3.1).
		// Spec ref: docs/plans/captain/05-specs/c2-spec.md.
		// Bead ref: hk-5tg5o.
		// Assigned to the function-scope var (declared above) so the workloop deps
		// block can reuse it for spawn-crew scheduled actions (hk-0es).
		// rcPrefix (hk-igpg): the per-project Claude RC label prefix, read from the
		// cached .harmonik/config.yaml daemon block (loaded at Start, ~L745). Empty =
		// bare label. Cosmetic only — crew identity keys stay bare.
		// Wire the keeper probe (hk-qgfme). The event bus satisfies crewKeeperEventBus
		// directly (eventbus.EventBus.Emit). For comms we need EmitAgentMessage, which
		// lives on the optional CommsMessageEmitter capability — type-assert the bus,
		// mirroring the quiesceCommsBus pattern above.
		var crewCommsEmitter crewKeeperCommsBus
		if ce, ok := bus.(crewKeeperCommsBus); ok {
			crewCommsEmitter = ce
		}
		crewHandler = NewCrewHandler(
			cfg.HandlerBinary, cfg.ProjectDir, cfg.ProjectCfg.Daemon.RemoteControlPrefix, cfg.Substrate, opPauseCtrl,
			WithKeeperProbe(cfg.ProjectCfg.Keeper, bus, crewCommsEmitter),
		)

		// SD-3 (hk-s2eac): wire the idle-completed-crew reaper now that both
		// the queue store and the crew stop seam exist. Started post-Seal,
		// below, alongside quiesceArbiter.Start.
		crewIdleReaperAgentsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "agents")
		crewIdleReaper = NewCrewIdleReaper(CrewIdleReaperConfig{
			ProjectDir: cfg.ProjectDir,
			Queues:     qs,
			Stopper:    crewHandler,
			// GATE-0 (hk-dy5gw): a persistent oversight role (admiral, watch —
			// manifest lifecycle.persistent: true) is never reclaimed. The manifest
			// property is the durable source of truth; a load error reads as
			// non-persistent so an ordinary bead-crew is unaffected.
			PersistentType: func(typeName string) bool {
				tf, err := agentmanifest.Load(crewIdleReaperAgentsDir, typeName)
				if err != nil {
					return false
				}
				return tf.Manifest.Lifecycle.Persistent
			},
		})

		// hk-2i36s: wire the periodic branch reaper now that cfg.ProjectDir is
		// known to be a git repository root. Started post-Seal, below, alongside
		// crewIdleReaper.StartWatcher.
		branchReapWatcher = NewBranchReapWatcher(BranchReapWatcherConfig{
			RepoDir: cfg.ProjectDir,
		})

		// Build the live state handler (hk-gv04 P2-a: `harmonik state`).
		// drainDet may be nil if ProjectDir was empty above — LiveStateBuilder
		// tolerates that and sets read_quality.unsure=true in the response.
		stateBuilder := NewLiveStateBuilder(sharedRunRegistry, qs, drainDet, concurrencyCtrl, cfg.MaxConcurrent, cfg.ProjectDir, cfg.ProjectCfg.Keeper)
		stateHandler := NewLiveStateSocketHandler(stateBuilder)

		dashBuilder := NewDashboardBuilder(stateBuilder, cfg.ProjectDir, cfg.JSONLLogPath)
		dashHandler := NewLiveDashboardSocketHandler(dashBuilder)

		// Start the poll-gate goroutine (SS-007, hk-w6q7 P2-b): evaluates the
		// fleet ActivityLabel every pollGateInterval and gates StaleWatcher and
		// BandwidthTuner when INACTIVE.  Must start after stateBuilder is ready.
		startPollGate(ctx, pollGate, stateBuilder)

		// Non-fatal: socket bind errors do not abort the daemon (PL-003 intent;
		// the absence of the socket is observable externally). Drain the done
		// channel to avoid goroutine leaks; error is discarded per the same
		// reasoning as defer ln.Close() discards errors in RunSocketListener.
		socketDone := make(chan error, 1)
		go func() {
			socketDone <- RunSocketListenerWithDashboard(ctx, sockPath, &noopRequestHandler{}, hookStore, subscribeHub, opPauseCtrl, commsSendHandler, crewHandler, quiesceArbiter, stateHandler, dashHandler, queueHandler)
		}()
		go func() { <-socketDone }() // drain: non-fatal; socket bind error discarded (see comment above)
	}

	// hk-uzvt9: apply the restart-backoff sleep computed above (bootBackoffDelay)
	// only now, AFTER the socket has been bound (or its bind goroutine started).
	// applyBootBackoff previously slept synchronously at ~L817, BEFORE the socket
	// block — a 30s/60s backoff delay blocked bind long enough for the
	// supervisor's 30s health-window to see no socket and revert to last-good
	// under rapid restart. The backoff throttles dispatch, not liveness, so the
	// sleep belongs after bind. sleepBootBackoff is a no-op when the delay is 0.
	sleepBootBackoff(ctx, bootBackoffDelay)

	// Skip the work loop when BrPath is not configured (unit-test mode).
	if cfg.BrPath != "" {
		deps, depsErr := newWorkLoopDeps(cfg, bus, workflowModeDefault, adapterReg, hookStore)
		if depsErr != nil {
			return fmt.Errorf("daemon.Start: work loop deps: %w", depsErr)
		}

		// FW1 (hk-y9fn): init sentinel governor deps from config.
		// A non-nil governorState signals to FW2 (wire-Evaluate) that the governor
		// is wired; DaemonStartedAt seeds the cold-start warmup gate (spec §1.4).
		// hk-drygf (FIX-B): liveness_no_progress_n is a REQUIRED operator key with
		// no compiled default. When the operator HAS a .harmonik/config.yaml, an
		// absent key (GovernorConfig returns *ErrMissingLivenessNoProgressN) — or a
		// read error — fails the daemon load loud rather than silently running with
		// the G-liveness gate disabled (the live hk-drygf bug). When no config.yaml
		// exists at all (fresh project / unit-test bootstrap), the operator has not
		// opted into sentinel config: keep the prior behaviour (governor wired but
		// the gate disabled) instead of refusing to start.
		if cfg.ProjectDir != "" {
			sentinelCfg, sentinelErr := digest.LoadSentinelConfig(cfg.ProjectDir)
			if sentinelErr != nil {
				return fmt.Errorf("daemon.Start: sentinel config: %w", sentinelErr)
			}
			governorCfg, govErr := sentinelCfg.GovernorConfig()
			if govErr != nil {
				// Fail loud only when the operator actually has a config.yaml;
				// absence means "sentinel not configured", not "misconfigured".
				configPath := filepath.Join(cfg.ProjectDir, ".harmonik", "config.yaml")
				if _, statErr := os.Stat(configPath); statErr == nil {
					return fmt.Errorf("daemon.Start: governor config: %w", govErr)
				}
				// No config.yaml: leave governorCfg zero-valued (gate disabled).
			}
			deps.governorCfg = governorCfg
			deps.governorState = &sentinel.GovernorState{
				DaemonStartedAt: daemonStartTime,
			}
			// FW2 (hk-z1lr): store mode and Phase-2 classes so the per-tick
			// governor evaluate block can guard on mode and compute HasUndeployedTail.
			deps.sentinelMode = sentinelCfg.Mode
			deps.sentinelPhase2Classes = sentinelCfg.Phase2Classes()
		}

		// C1 boot-seed (hk-o50hy): populate emittedEpics from the durable event log
		// so a restart does not re-emit epic_completed for an already-completed epic
		// (AC-5). When cfg.JSONLLogPath is empty (unit-test mode), the empty map
		// from newWorkLoopDeps is retained — the in-process guard still works for
		// the session.
		if cfg.JSONLLogPath != "" {
			seed := make(map[core.BeadID]struct{})
			for ev := range eventbus.ScanAfter(cfg.JSONLLogPath, core.EventID{}) {
				if core.EventType(ev.Type) != core.EventTypeEpicCompleted {
					continue
				}
				var pl core.EpicCompletedPayload
				if err := json.Unmarshal(ev.Payload, &pl); err != nil || !pl.Valid() {
					continue
				}
				seed[pl.EpicID] = struct{}{}
			}
			deps.emittedEpics = seed
			deps.emittedEpicsMu = &sync.Mutex{}
		}

		// AC1 boot-seed (hk-3ndb): load the durable follow-up ledger so a daemon
		// restart does not re-emit staged beads already created in a prior session
		// (flywheel-motion.md §5.4 B guardrail 4). When followUpLedgerPath is empty
		// (unit-test mode) the empty map from newWorkLoopDeps is retained.
		if deps.followUpLedgerPath != "" {
			if ledger, loadErr := loadFollowUpLedger(deps.followUpLedgerPath); loadErr != nil {
				fmt.Fprintf(os.Stderr, "daemon.Start: load follow-up ledger: %v\n", loadErr)
			} else {
				deps.followUpLedger = ledger
			}
		}

		// Inject the QueueStore singleton so the work loop can pull from the
		// active queue (queue-pull dispatch path per execution-model.md §7.4 TS-1).
		//
		// Spec ref: specs/queue-model.md §9.1 QM-060; specs/execution-model.md §7.4.
		deps.queueStore = qs
		// Wire the wake channel so queue-submit RPCs immediately unblock the
		// workloop's idle sleep (hk-24xn1).
		deps.submitWakeC = qs.WakeCh()

		// Wire the generic recurring-job surface (codename:schedule, hk-0es).
		// Load .harmonik/schedules.json into a single-writer store; the workloop
		// runScheduleTick fires due jobs each poll. spawn-crew actions reuse the
		// crewHandler (HandleCrewStart path) so subscription-billing guards apply by
		// construction; command actions inherit deps.handlerEnv (no credential keys).
		// A present-but-unparseable file is fatal so the operator notices early; an
		// absent file is a normal empty store.
		scheduleStore := schedule.NewStore(cfg.ProjectDir)
		if loadErr := scheduleStore.Load(); loadErr != nil {
			return fmt.Errorf("daemon.Start: load schedule store: %w", loadErr)
		}
		ensureOpsMonitorSchedule(scheduleStore, cfg.ProjectCfg.Opsmonitor)
		ensureCtxWatchdogSchedule(scheduleStore, cfg.ProjectCfg.Watchdog.Enabled)
		ensureWatchLivenessSchedule(scheduleStore, cfg.ProjectCfg.Watch, deps.daemonBinaryPath)
		deps.scheduleStore = scheduleStore
		deps.scheduleWakeC = scheduleStore.WakeCh()
		// Wire the schedule store into the quiesce arbiter so `harmonik sleep`
		// suspends all enabled jobs and `harmonik wake --all` restores them
		// symmetrically (hk-xjr1n).
		quiesceArbiter.SetScheduleStore(scheduleStore)
		deps.crewHandler = crewHandler // may be nil in unit-test mode (no socket)
		deps.commsWhoQuerier = shellCommsWho(deps.daemonBinaryPath, cfg.ProjectDir)
		deps.commsSend = shellCommsSend(deps.daemonBinaryPath, cfg.ProjectDir)

		// Inject the HandlerPauseController so the dispatcher skip-on-paused gate
		// (hk-kac8g) can consult pause state before claiming each item.
		// nil → gate disabled; pre-hk-kac8g behaviour preserved for callers that
		// don't set the field.
		deps.handlerPauseController = cfg.HandlerPauseController

		// Inject the drain-cancel so harmonik run <bead-id> exits after the queue
		// completes (hk-icecw). The zero value (nil) preserves normal daemon behaviour.
		deps.cancelOnQueueDrain = cfg.CancelOnQueueDrain

		// Inject the exit-cancel so harmonik run <bead-id> exits on both
		// all-success AND paused-by-failure outcomes (hk-8jh26 Fix 1).
		// The zero value (nil) preserves normal daemon behaviour.
		deps.cancelOnQueueExit = cfg.CancelOnQueueExit

		// Inject the stop-dispatch context (hk-2o2i9): when set, the work loop's
		// outer poll checks this context for dispatch-halt instead of the main ctx,
		// so CancelOnQueueDrain/Exit do not kill in-flight reviewer goroutines.
		// The zero value (nil) falls back to ctx (backward-compat).
		deps.stopDispatchCtx = cfg.StopDispatchCtx

		// Inject the HandlerPauseController so the work loop can gate dispatch
		// on handler pause state (hk-m0k0a).
		deps.handlerPauseController = handlerPauseCtrl

		// Inject the OperatorPauseController so the workloop can gate br-ready
		// dispatch when an operator pause is active (hk-ry8q1). nil when
		// ProjectDir was not set (unit-test mode without a socket).
		deps.operatorPauseCtrl = opPauseCtrl

		// Inject the DecisionBlocker so the workloop can gate dispatch for beads
		// blocked by an unacknowledged decision_required event (EV-043, EV-043a).
		// Always non-nil in production; unit-test callers that do not exercise
		// decision-blocking leave this at the always-unblocked default.
		//
		// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
		// Bead ref: hk-pbmsq.
		deps.decisionBlocker = decisionBlocker

		// Inject the ConcurrencyController so the dispatch gate reads the live
		// ceiling on every tick (hk-ohiaf). nil falls back to the static
		// maxConcurrent field (unit-test mode / legacy callers).
		deps.concurrencyCtrl = concurrencyCtrl

		// Wire the live worker enable/disable toggle (hk-xjbvi). The work loop's
		// deps.workerRegistry — built by newWorkLoopDeps from .harmonik/workers.yaml
		// — is the SAME registry the dispatch path reads via SelectWorker, so a
		// `harmonik worker enable <name>` RPC flips selectability with no restart.
		// The closure captures that exact registry pointer; SetEnabledByName mutates
		// it under the registry mutex. A nil registry (no workers.yaml) yields a
		// clean "no such worker configured" error rather than a panic.
		if queueHandlerAdapter != nil {
			workerReg := deps.workerRegistry
			queueHandlerAdapter.SetWorkerToggleFunc(func(name string, enabled bool) (string, error) {
				if workerReg == nil {
					return "", fmt.Errorf("no such worker %q: no remote worker configured (.harmonik/workers.yaml is empty)", name)
				}
				return workerReg.SetEnabledByName(name, enabled)
			})
		}

		// Inject the shared RunRegistry so the work loop and the
		// HandlerPausePolicyGoroutine operate on the same in-flight snapshot.
		// The policy goroutine calls Registry.snapshotWithKeys() at pause time;
		// using the same instance as the work loop ensures the freeze-list reflects
		// actual in-flight runs rather than an empty registry.
		//
		// Bead ref: hk-37zy8.
		deps.runRegistry = sharedRunRegistry

		// Inject the test-only worktree factory when set via WithWorktreeFactory.
		// Nil (the default) falls back to productionWorktreeFactory inside beadRunOne.
		if hooks.worktreeFactory != nil {
			deps.worktreeFactory = hooks.worktreeFactory
		}

		// Inject the test-only merge-queue override when set via WithMergeQueue.
		// Nil (the default) keeps production's own queue from newWorkLoopDeps
		// (RSM-015), so production merges stay serialised through the domain.
		if hooks.mergeQ != nil {
			deps.mergeQ = hooks.mergeQ
		}

		// hk-bk33: spawn-substrate readiness gate for post-boot re-dispatch.
		// When a restart-backoff delay was applied and the substrate exposes a
		// readiness probe, start a goroutine that calls ProbeSpawnReady and closes
		// a channel when it returns. runWorkLoop waits on this channel before the
		// first dispatch tick, preventing spurious agent_ready_timeout on
		// QM-002a-reverted beads re-dispatched right after a restart-backoff boot.
		if bootBackoffDelay > 0 {
			if prober, ok := cfg.Substrate.(substrateSpawnReadier); ok {
				readyCh := make(chan struct{})
				go func() {
					defer close(readyCh)
					_ = prober.ProbeSpawnReady(ctx)
				}()
				deps.spawnSubstrateReadyCh = readyCh
			}
		}

		quiesceArbiter.Start(ctx)

		// SD-3 (hk-s2eac): start the idle-completed-crew reaper. crewIdleReaper
		// is non-nil here (constructed above, same cfg.ProjectDir != "" block).
		crewIdleReaper.StartWatcher(ctx)

		// hk-2i36s: start the periodic branch reaper. branchReapWatcher is
		// non-nil here (constructed above, same cfg.ProjectDir != "" block).
		branchReapWatcher.StartWatcher(ctx)

		// Emit the composition-root wiring audit log when HARMONIK_DEBUG_WIRING=1
		// is set in the operator environment.  All 31 wiring points have been
		// established at this point; the log is a stable diff surface for catching
		// silent drops between daemon versions.
		//
		// Bead ref: hk-4mupj.
		logCompositionRoot(cfg.LogWriter)

		// RC-020a dispatch point (c): start the scheduled detector cadence goroutine.
		//
		// The goroutine runs until ctx is cancelled and emits
		// reconciliation_started{trigger:"scheduled-hourly"} on each tick, followed
		// by a Cat 3c bead-ledger scan and a Class B orphan repair pass
		// (hk-m3ydd: beads in_progress with no queue record are reset to open).
		// The default interval is 1 h (ReconciliationScanCadenceDefault); operators
		// may override via cfg.ReconciliationScanCadence
		// (operator-nfr.md §4.3 reconciliation_scan_cadence).
		//
		// Bead ref: hk-63oh.21, hk-m3ydd.
		StartReconciliationScheduler(ctx, ReconciliationSchedulerConfig{
			ProjectDir:   cfg.ProjectDir,
			BrPath:       cfg.BrPath,
			TargetBranch: "", // defaults to "main" inside the scheduler
			Interval:     cfg.ReconciliationScanCadence,
			Emitter:      bus,
			LogWriter:    cfg.LogWriter,
		})

		// WR3 (hk-jn3u): recurring worker-report poll. The boot health check
		// (buildWorkerRegistry, B6) probes each enabled worker once at startup;
		// this drives workers.CollectReport on a report_interval ticker so worker
		// resource + problem reports (WR1/WR2/WR4) actually flow during operation.
		//
		// Phase-1 OBSERVABILITY ONLY: it emits worker_report events on a timer and
		// does NOT touch SelectWorker, max_slots, or dispatch. RunReportLoop is
		// off-by-default — it returns immediately (no ticker armed) when the
		// registry is nil / no worker is enabled, so a deployment with no
		// workers.yaml behaves byte-identically. It runs in its own goroutine bound
		// to the shutdown ctx; a slow/failing CollectReport is logged and dropped,
		// never wedging the work loop.
		var reportEmit workers.EmitFunc
		if bus != nil {
			reportEmit = bus.Emit
		}
		go workers.RunReportLoop(ctx, cfg.Workers, deps.workerRegistry, workers.ProductionRunnerForWorker, reportEmit)

		// Use the caller-supplied ctx to drive a clean shutdown. The production
		// caller (cmd/harmonik/main.go) passes a signal.NotifyContext so that
		// Ctrl-C / SIGTERM cancels the work loop without sending signals into
		// the test process (hk-7oz2f).
		// hk-mdus1: wire the StaleWatcher force-reap watchdog seams now that
		// `deps` (queueStore, emitter) is fully built. Two-phase because the
		// watcher is constructed and started (StartWatcher) far earlier, before
		// workLoopDeps exists.
		//
		// ForceReap: when the watchdog force-Unregisters a wedged run's leaked
		// slot, emit a terminal run_failed and drive the owning queue item
		// terminal so the group advances (the wedged goroutine never runs the
		// completion path itself).
		staleWatcher.SetForceReap(func(runID core.RunID, handle *RunHandle) {
			emitRunCompleted(ctx, bus, runID, string(handle.BeadID), handle.OwningEpicID, handle.OwningEpicAssignee, false,
				"force-reaped: run wedged past cancel grace; concurrency slot reclaimed (hk-mdus1)",
				handle.QueueID, handle.QueueGroupIndex, nil)
			if handle.QueueName != "" && handle.QueueID != nil && handle.QueueGroupIndex != nil && handle.QueueItemIndex >= 0 {
				evaluateGroupAdvanceWithOutcome(ctx, deps, handle.QueueName, *handle.QueueID, *handle.QueueGroupIndex, handle.QueueItemIndex, false)
			}
		})
		// RunProcessDead: fast dead-process reap probe. Resolve the run's tmux
		// session (from the .harmonik/runs/ record written for independent-session
		// runs) and report whether its pane PID is gone via the substrate's
		// #{pane_pid} liveness. Best-effort: any lookup error → "not dead" so a
		// probe failure never triggers a spurious reap.
		if sa, ok := cfg.Substrate.(substrateWithAdapter); ok {
			if reapAdapter := sa.tmuxAdapter(); reapAdapter != nil && cfg.ProjectDir != "" {
				staleWatcher.SetRunProcessDead(func(runID core.RunID, _ *RunHandle) bool {
					recs, listErr := runpkg.List(cfg.ProjectDir)
					if listErr != nil {
						return false
					}
					for _, r := range recs {
						if r.RunID != runID.String() {
							continue
						}
						if r.SessionName == "" {
							return false
						}
						pid, pidErr := reapAdapter.WindowPanePID(ctx, ltmux.WindowHandle(r.SessionName+":"))
						if pidErr != nil {
							return false
						}
						if pid == 0 {
							return true
						}
						return processDead(pid)
					}
					return false
				})
			}
		}

		loopDone := make(chan error, 1)
		go func() {
			loopDone <- runWorkLoop(ctx, deps)
		}()

		// Block until the work loop exits (either ctx cancelled or fatal error).
		<-loopDone
	}

	return nil
}
