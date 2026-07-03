// Command harmonik is the production daemon binary for harmonik.
//
// # Composition root
//
// This file is the composition root: it constructs every production dependency
// and wires them together before handing control to the daemon's run loop.
// Production bindings declared here:
//
//   - PolicyEngine: [core.NoOpPolicyEngine] — permits every evaluation with no
//     constraints. This is the first-class production binding for MVH; it is
//     NOT a nil sentinel and NOT a test double. The orchestrator dispatcher
//     calls PolicyEngine.Evaluate on every gate and guard without branching on
//     the concrete type, satisfying [specs/scenario-harness.md §4.3.SH-018].
//
//   - BusFlusher: nil for MVH. The [lifecycle.BusFlusher] interface is declared;
//     its real implementation ([lifecycle.BusFlusher] on the EventBus type) lands
//     when the EventBus bead (hk-hqwn.57) merges. Until then the bus-flush step
//     in [lifecycle.RecoverWithLogFlush] is skipped (nil-safe per EV-019a).
//     Wiring site (hk-hqwn.70): substitute nil with the real EventBus once
//     hk-hqwn.57 lands.
//
// When the control-points subsystem (hk-a8bg) lands post-MVH, the composition
// root substitutes the real PolicyEngine evaluator. No dispatcher changes are
// required.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5 (policy-engine
// bypass-ability must be explicit); specs/scenario-harness.md §4.3.SH-018
// (no test-mode branches in production); bootstrap-subset.md §1 (CP fully
// deferred).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/branching"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/hookrelay"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
	"github.com/gregberns/harmonik/internal/release"
	"github.com/gregberns/harmonik/internal/supervise"
	"github.com/gregberns/harmonik/internal/workers"
)

func main() {
	os.Exit(run())
}

// queueTopUsage is the help block for `harmonik queue`. It is printed both at
// the verb position (`harmonik queue --help`, hk-y4e96) and when --help is the
// first sub-arg of a file-or-args-taking verb (`harmonik queue submit --help`,
// hk-l7b) so that --help is never swallowed as a queue-file path.
const queueTopUsage = `harmonik queue — submit or inspect the bead queue

USAGE
  harmonik queue <verb> [flags]

VERBS
  submit    Submit a new bead to the queue (daemon must be running)
  append    Append a bead to an existing queue run (daemon must be running)
  status    Show current queue state and bead statuses (daemon must be running)
  list      List all active queues with status and worker counts (daemon must be running)
  pause     Pause a named queue (daemon must be running)
  resume    Resume a paused named queue (daemon must be running)
  dry-run   Validate a queue submission without executing (daemon must be running)
  cancel    Archive a stale queue.json without a live daemon (no daemon required)
  set-concurrency <n>  Set the daemon's concurrent-dispatch ceiling live (daemon must be running)

NOTES
  Most verbs require the daemon to be running.
  'cancel' works without a live daemon — use it to clear a queue left by a
  killed daemon (e.g. after SIGTERM of a wedged harmonik process).
  Exit code 17 means the daemon is not running (socket absent or ECONNREFUSED).
  Queues are created automatically on first submit to a new name (--queue flag).
  Absent --queue defaults to the 'main' queue.

EXIT CODES
  0   Success (JSON response to stdout)
  1   Validation error (JSON error body to stdout)
  2   Transport/protocol error or unrecognised verb
  17  Daemon not running

EXAMPLES
  harmonik queue submit --beads hk-abc123
  harmonik queue submit --queue investigate --beads hk-abc,hk-def
  harmonik queue submit --beads hk-abc,hk-def,hk-ghi
  harmonik queue submit /tmp/batch.json
  harmonik queue dry-run --beads hk-abc123
  harmonik queue dry-run /tmp/batch.json
  harmonik queue append --queue-id <uuid> 0 hk-abc123
  harmonik queue append --queue investigate 0 hk-abc123
  harmonik queue status
  harmonik queue list
  harmonik queue pause investigate
  harmonik queue resume investigate
  harmonik queue cancel
  harmonik queue cancel --force
  harmonik queue set-concurrency 4
`

const workerTopUsage = `harmonik worker — toggle a remote worker live (no restart)

USAGE
  harmonik worker <verb> <name> [flags]

VERBS
  enable <name>    Enable a configured remote worker in the live daemon (remote dispatch on)
  disable <name>   Disable a configured remote worker in the live daemon (remote dispatch off)

NOTES
  Both verbs require the daemon to be running and a worker configured in
  .harmonik/workers.yaml. The toggle flips the worker's enabled flag in the
  LIVE registry over the daemon socket — no restart, no workers.yaml edit.
  An enabled worker becomes selectable for remote dispatch on the next tick;
  a disabled worker stops taking new remote runs (in-flight runs complete).
  An unknown worker name is rejected.

EXIT CODES
  0   Success (worker state echoed to stdout)
  2   Transport/protocol error, unknown worker name, or unrecognised verb
  17  Daemon not running

EXAMPLES
  harmonik worker enable gb-mbp
  harmonik worker disable gb-mbp
  harmonik worker enable gb-mbp --json
`

// run is the testable entry-point. It constructs the composition root and
// starts the daemon. It returns an exit code.
//
// The composition root pattern keeps dependency construction separate from
// daemon logic so that the wiring can be inspected and replaced at this single
// site.
func run() int {
	// Subcommand dispatch: tmux-start and hook-relay must be checked before
	// flag.Parse so that flag does not consume their positional arguments.

	// --help / -h: print top-level usage and exit 0 before any flag.Parse so
	// that "harmonik --help" always exits 0 with the subcommand listing.
	if len(os.Args) >= 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		harmonikUsage()
		return 0
	}

	// harmonik version  (or --version / -version): print semver + commit and exit 0.
	//
	// Output format (normative, specs/release-pipeline.md §2.3):
	//   harmonik v0.y.z (commit: <sha>)
	//
	// Dispatched before flag.Parse so the global flag set does not reject
	// the positional "version" argument.
	//
	// Spec ref: specs/release-pipeline.md §2.3; bead hk-ww7ee.
	if len(os.Args) >= 2 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("harmonik %s (commit: %s)\n", version, resolvedCommitHash())
		return 0
	}

	// hk tmux-start — operator-facing tmux session bootstrap.
	// Parses its own --session-name flag and must run before flag.Parse so
	// that the global flag set does not reject the subcommand-specific flag.
	//
	// Spec: process-lifecycle.md §4.10 PL-028 refinement.
	if len(os.Args) >= 2 && os.Args[1] == "tmux-start" {
		subArgs := os.Args[2:]
		// --help/-h intercept (hk-y4e96).
		for _, arg := range subArgs {
			if arg == "--help" || arg == "-h" {
				fmt.Print(`harmonik tmux-start — bootstrap a tmux session and start the daemon inside it

USAGE
  harmonik tmux-start [--session-name NAME] [--project DIR]

FLAGS
  --session-name NAME  tmux session name (default: harmonik)
  --project DIR        Project directory (default: current working directory)

EXAMPLES
  harmonik tmux-start
  harmonik tmux-start --session-name my-project
  harmonik tmux-start --project /path/to/project --session-name my-project
`)
				return 0
			}
		}
		sessionNameFlag := ""
		projectDirFlag := ""
		// Minimal flag parsing for tmux-start arguments only.
		for i := 0; i < len(subArgs); i++ {
			switch {
			case subArgs[i] == "--session-name" && i+1 < len(subArgs):
				i++
				sessionNameFlag = subArgs[i]
			case strings.HasPrefix(subArgs[i], "--session-name="):
				sessionNameFlag = strings.TrimPrefix(subArgs[i], "--session-name=")
			case subArgs[i] == "--project" && i+1 < len(subArgs):
				i++
				projectDirFlag = subArgs[i]
			case strings.HasPrefix(subArgs[i], "--project="):
				projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")
			}
		}
		if projectDirFlag == "" {
			wd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik tmux-start: cannot determine working directory: %v\n", err)
				return 24
			}
			projectDirFlag = wd
		}
		absProjectDir, err := filepath.Abs(projectDirFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik tmux-start: cannot resolve project path %q: %v\n", projectDirFlag, err)
			return 24
		}
		return tmux.RunTmuxStart(absProjectDir, sessionNameFlag, os.Stdout, os.Stderr, tmux.SyscallExec, nil)
	}

	// Spec: specs/claude-hook-bridge.md §4.4 CHB-010..017.
	if len(os.Args) >= 2 && os.Args[1] == "hook-relay" {
		// --help/-h intercept (hk-y4e96).
		if len(os.Args) >= 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			fmt.Print(`harmonik hook-relay — forward a Claude hook event to the daemon (internal use)

USAGE
  harmonik hook-relay <event-kind>

ARGUMENTS
  event-kind  The Claude hook event type (e.g. PreToolUse, PostToolUse, Stop)

NOTES
  This subcommand is intended for use by Claude Code hook configurations, not
  direct operator invocation. The daemon must be running to receive events.

EXAMPLES
  harmonik hook-relay PreToolUse
  harmonik hook-relay Stop
`)
			return 0
		}
		eventKind := ""
		if len(os.Args) >= 3 {
			eventKind = os.Args[2]
		}
		if eventKind == "" {
			fmt.Fprintln(os.Stderr, "harmonik hook-relay: missing event-kind argument")
			return 1
		}
		return hookrelay.Run(eventKind, os.Stdin, os.Stderr, nil)
	}

	// hk queue {submit,append,status,dry-run} — external orchestrator queue
	// control surface. Dispatched before flag.Parse per PL-028c so that the
	// global flag set does not reject subcommand-specific flags.
	//
	// Exit-code contract (all four verbs):
	//   0  — success (JSON response to stdout)
	//   1  — validation error (JSON error body to stdout, not stderr)
	//   2  — transport/protocol error or unrecognised verb
	//  17  — daemon not running (socket absent or ECONNREFUSED)
	//
	// Spec ref: specs/process-lifecycle.md §4.4 PL-028 + PL-028c.
	// Bead ref: hk-eblue.

	// harmonik init [--project DIR] [--target-branch BRANCH] [--prefix PREFIX]
	// [--doctor] [--force] [--smoke] [--no-supervise]
	//
	// Bootstrap a new project for harmonik: create .harmonik/ structure, init
	// beads database, write config files, render AGENTS.md, symlink CLAUDE.md.
	//
	// Bead ref: hk-y171w.
	if len(os.Args) >= 2 && os.Args[1] == "init" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runInitSubcommand(subArgs)
	}

	// harmonik sync-assets [--project DIR] [--dry-run|--apply|--commit] [--force]
	//
	// Ongoing UPDATE path (sibling of init): reconcile a project's on-disk
	// instruction files against the binary's embedded asset bundle via a
	// class-aware 3-way reconcile. --dry-run is the default; --apply refuses
	// while the daemon is dispatching unless --force.
	//
	// Exit codes: 0 success, 1 arg/IO error, 3 daemon-lull gate refused.
	// Bead ref: hk-i7i3. Design: plans/2026-06-20-doc-instruction-audit/10-asset-sync.md.
	if len(os.Args) >= 2 && os.Args[1] == "sync-assets" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runSyncAssetsSubcommand(subArgs)
	}

	// harmonik reconcile [--project DIR] [--target-branch BRANCH]
	// Cat 3c auto-reconciler: detect and close IN_PROGRESS beads whose
	// implementation has already merged to the target branch.
	//
	// Exit-code contract:
	//   0  — success (0 or more beads closed)
	//   1  — argument or adapter error
	//   2  — at least one bead close failed
	//
	// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
	if len(os.Args) >= 2 && os.Args[1] == "reconcile" {
		return runReconcileSubcommand(os.Args[2:])
	}

	// harmonik confirm-verdict <run_id> [--project DIR]
	// Operator verdict-confirmation surface: confirm a pending reconciliation
	// verdict so the daemon proceeds with verdict execution.
	//
	// Exit-code contract:
	//   0  — success
	//   1  — argument or flag error
	//  16  — no pending verdict for run_id (operator-control-invalid-state)
	//  17  — daemon not running
	//
	// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
	//           specs/operator-nfr.md §4.3 ON-014.
	// Bead ref: hk-63oh.39.
	if len(os.Args) >= 2 && os.Args[1] == "confirm-verdict" {
		return runConfirmVerdictSubcommand(os.Args[2:])
	}

	// harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]
	// Operator verdict-veto surface: veto a pending reconciliation verdict.
	// With --promote-to escalate-to-human, the daemon substitutes the vetoed
	// verdict with escalate-to-human and executes that instead.
	//
	// Exit-code contract:
	//   0  — success
	//   1  — argument or flag error
	//  16  — no pending verdict for run_id (operator-control-invalid-state)
	//  17  — daemon not running
	//
	// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
	//           specs/operator-nfr.md §4.3 ON-014.
	// Bead ref: hk-63oh.39.
	if len(os.Args) >= 2 && os.Args[1] == "veto-verdict" {
		return runVetoVerdictSubcommand(os.Args[2:])
	}

	// harmonik beads-merge %O %A %B %P — custom git merge-driver for .beads/issues.jsonl.
	//
	// Union-by-bead-ID merge with last-writer-wins collision resolution on updated_at.
	// Registered via .gitattributes + .git/config per bead hk-jon6r.
	//
	// Bead ref: hk-jon6r.
	if len(os.Args) >= 2 && os.Args[1] == "beads-merge" {
		return runBeadsMergeSubcommand(os.Args[2:])
	}

	// harmonik beads-dedup [--path FILE] [--dry-run]
	// One-time dedup of .beads/issues.jsonl: keeps the newest record per bead ID.
	// Fixes ghost "open" beads left by older-open + newer-closed duplicate JSONL
	// records that caused br show / br list to over-report open work.
	//
	// Bead ref: hk-0f35x.
	if len(os.Args) >= 2 && os.Args[1] == "beads-dedup" {
		return runBeadsDedupSubcommand(os.Args[2:])
	}

	// harmonik sleep [--force] [--project DIR]
	// Manual operator override to quiesce (park) all LLM sessions now.
	// Without --force, the daemon's GenuineDrain oracle is consulted first.
	// --force bypasses the drain gate (operator/captain maintenance escape hatch).
	//
	// Exit-code contract:
	//   0  — fleet parked
	//   1  — argument error
	//   2  — daemon rejected the request or protocol error
	//  17  — daemon not running
	//
	// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
	if len(os.Args) >= 2 && os.Args[1] == "sleep" {
		subArgs := os.Args[2:]
		return runSleepSubcommand(context.Background(), subArgs)
	}

	// harmonik wake (--agent <name> | --all) [--project DIR]
	// Manual operator override to wake sleeping LLM sessions.
	// --agent <name> wakes one specific session; --all wakes every sleeping session.
	// This is the fleet-stall human escape hatch when automatic wake triggers miss.
	//
	// Exit-code contract:
	//   0  — sessions nudged
	//   1  — argument error
	//   2  — daemon rejected the request or protocol error
	//  17  — daemon not running
	//
	// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
	if len(os.Args) >= 2 && os.Args[1] == "wake" {
		subArgs := os.Args[2:]
		return runWakeSubcommand(context.Background(), subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "queue" {
		verb := ""
		if len(os.Args) >= 3 {
			verb = os.Args[2]
		}
		// --help/-h intercept (hk-y4e96): catch on the verb position; the
		// first-subArg case (e.g. `queue submit --help`) is handled below (hk-l7b).
		if verb == "--help" || verb == "-h" {
			fmt.Print(queueTopUsage)
			return 0
		}
		subArgs := []string{}
		if len(os.Args) >= 4 {
			subArgs = os.Args[3:]
		}
		// --help/-h intercept (hk-l7b): catch --help as the FIRST sub-arg of a
		// file-or-args-taking verb (submit/dry-run/append/set-concurrency).
		// Without this, `harmonik queue submit --help` reaches the submit handler
		// and treats "--help" as the queue-file path ("open --help: no such file").
		// Reuse the verb-position queue help block above; exit 0 like that path.
		if len(subArgs) >= 1 && (subArgs[0] == "--help" || subArgs[0] == "-h") {
			fmt.Print(queueTopUsage)
			return 0
		}
		ctx := context.Background()
		switch verb {
		case "submit":
			return queuecli.RunQueueSubmit(ctx, subArgs, os.Stdout, os.Stderr)
		case "append":
			return queuecli.RunQueueAppend(ctx, subArgs, os.Stdout, os.Stderr)
		case "status":
			return queuecli.RunQueueStatus(ctx, subArgs, os.Stdout, os.Stderr)
		case "list":
			return queuecli.RunQueueList(ctx, subArgs, os.Stdout, os.Stderr)
		case "pause":
			return queuecli.RunQueuePause(ctx, subArgs, os.Stdout, os.Stderr)
		case "resume":
			return queuecli.RunQueueResume(ctx, subArgs, os.Stdout, os.Stderr)
		case "dry-run":
			return queuecli.RunQueueDryRun(ctx, subArgs, os.Stdout, os.Stderr)
		case "cancel":
			return queuecli.RunQueueCancel(ctx, subArgs, os.Stdout, os.Stderr)
		case "set-concurrency":
			return queuecli.RunQueueSetConcurrency(ctx, subArgs, os.Stdout, os.Stderr)
		default:
			fmt.Fprintf(os.Stderr, "harmonik queue: unrecognised verb %q; verbs are: submit, append, status, list, pause, resume, dry-run, cancel, set-concurrency\n", verb)
			return 2
		}
	}

	// harmonik worker enable|disable <name> [--project DIR] [--json]
	// Live operator toggle for the remote worker registry (hk-xjbvi): flips a
	// worker's enabled flag in the running daemon via socket RPC so remote
	// dispatch can be turned on/off WITHOUT a restart. Mirrors `queue
	// set-concurrency`.
	if len(os.Args) >= 2 && os.Args[1] == "worker" {
		verb := ""
		if len(os.Args) >= 3 {
			verb = os.Args[2]
		}
		if verb == "--help" || verb == "-h" {
			fmt.Print(workerTopUsage)
			return 0
		}
		subArgs := []string{}
		if len(os.Args) >= 4 {
			subArgs = os.Args[3:]
		}
		if len(subArgs) >= 1 && (subArgs[0] == "--help" || subArgs[0] == "-h") {
			fmt.Print(workerTopUsage)
			return 0
		}
		ctx := context.Background()
		switch verb {
		case "enable":
			return queuecli.RunWorkerEnable(ctx, subArgs, os.Stdout, os.Stderr)
		case "disable":
			return queuecli.RunWorkerDisable(ctx, subArgs, os.Stdout, os.Stderr)
		default:
			fmt.Fprintf(os.Stderr, "harmonik worker: unrecognised verb %q; verbs are: enable, disable\n", verb)
			return 2
		}
	}

	// harmonik handler status [--type T] [--format json|text] [--project DIR]
	// Read-only status surface for handler-pause state.
	// Reads .harmonik/handler-state.json directly (no daemon required).
	//
	// Exit-code contract:
	//   0  — success (output written)
	//   1  — argument or file-parse error
	//   2  — forward-incompatible schema version
	//
	// Bead ref: hk-39ryh.
	if len(os.Args) >= 2 && os.Args[1] == "handler" {
		return runHandlerSubcommand(os.Args[2:])
	}

	// harmonik run <bead-id> [--project DIR] — single-bead invocation.
	//
	// Submits a single-item queue to the daemon and blocks until the bead reaches
	// a terminal state. The daemon context is cancelled after the queue drains so
	// the process exits with code 0 on success or non-zero on failure.
	//
	// Exit-code contract:
	//   0  — bead reached SUCCESS terminal (bead closed)
	//   1  — bead validation or daemon error
	//  17  — daemon not running (socket absent) — not used here (in-process)
	//
	// Bead ref: hk-icecw.
	if len(os.Args) >= 2 && os.Args[1] == "run" {
		return runBeadSubcommand(os.Args[2:])
	}

	// harmonik keeper <verb|flags> — session-keeper context watcher and
	// dispatching-marker control surface (codename:session-keeper, hk-ekap1).
	//
	// Verbs (hk-rc51s):
	//   set-dispatching   <agent> [--project DIR] — write .dispatching marker
	//   clear-dispatching <agent> [--project DIR] — remove .dispatching marker
	//
	// Watcher mode (flags only):
	//   --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N]
	//
	// Dispatched before flag.Parse so that the global flag set does not reject
	// subcommand-specific flags.
	//
	// Exit-code contract: 0 success/no-op, 1 arg/IO error, 2 lock held.
	//
	// Spec ref: codename:session-keeper (hk-ekap1); beads hk-fzzc6, hk-rc51s.
	if len(os.Args) >= 2 && os.Args[1] == "keeper" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		// Route to verb handlers before the help intercept. Note: verbs use
		// flag.ContinueOnError, so "harmonik keeper set-dispatching --help"
		// prints the verb's own flag usage and exits 1 (not keeperTopUsage).
		// The keeperTopUsage intercept below only fires for bare
		// "harmonik keeper --help" or unknown subcommands.
		if len(subArgs) > 0 {
			switch subArgs[0] {
			case "config":
				return runKeeperConfig(subArgs[1:])
			case "set-dispatching":
				return runKeeperSetDispatching(subArgs[1:])
			case "clear-dispatching":
				return runKeeperClearDispatching(subArgs[1:])
			case "hold":
				return runKeeperHold(subArgs[1:])
			case "release":
				return runKeeperRelease(subArgs[1:])
			case "enable":
				return runKeeperEnableSubcommand(subArgs[1:])
			case "doctor":
				return runKeeperDoctorSubcommand(subArgs[1:])
			case "restart-now":
				return runKeeperRestartNow(subArgs[1:])
			case "ping":
				return runKeeperPing(subArgs[1:])
			case "await-ack":
				return runKeeperAwaitAck(subArgs[1:])
			default:
				// W7 (hk-x7s): a NON-flag first token that matches no known verb is a
				// typo'd subcommand (e.g. "restrt-now"). Previously it fell through to
				// runKeeperSubcommand and was rejected as a stray positional with the
				// misleading "this command is flag-only" message — so the operator
				// thought the FLAG form was wrong, not that they fat-fingered the verb
				// (a real recovery footgun for a destructive verb like restart-now).
				// Catch it loudly here with the verb list and a non-zero exit. Tokens
				// that START with '-' are watcher-mode flags, not verbs, so they fall
				// through to the help intercept + runKeeperSubcommand below.
				if !strings.HasPrefix(subArgs[0], "-") {
					fmt.Fprintf(os.Stderr, "harmonik keeper: unknown keeper subcommand %q\n\n", subArgs[0])
					fmt.Fprint(os.Stderr, keeperTopUsage)
					return 2
				}
			}
		}
		for _, arg := range subArgs {
			if arg == "--help" || arg == "-h" {
				fmt.Print(keeperTopUsage) //nolint:forbidigo // help output to stdout is intentional (hk-fzzc6)
				return 0
			}
		}
		return runKeeperSubcommand(subArgs)
	}

	// harmonik supervise {start,stop,status,attach,restart,logs} — manage the
	// supervisor/cognition process per §PL-019. Dispatched before flag.Parse so
	// that the global flag set does not reject subcommand-specific flags.
	//
	// Exit-code contract: 0 success, 1 op-error, 2 unknown-verb,
	// 17 daemon-not-running, 25 supervisor-already-running.
	//
	// Spec ref: specs/process-lifecycle.md §4.10 PL-028d.
	// Bead ref: hk-qx702.
	if len(os.Args) >= 2 && os.Args[1] == "supervise" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runSuperviseSubcommand(subArgs)
	}

	// harmonik subscribe — stream daemon events on the Unix socket (hk-6ynv4).
	// Long-running observation-only command; routed through ON-055 (subscribe
	// is read-only observation, no control-plane authority).
	if len(os.Args) >= 2 && os.Args[1] == "subscribe" {
		return runSubscribeSubcommand(os.Args[2:])
	}

	// harmonik smoke — 5-signal end-to-end verification of a live daemon (hk-4rkrg).
	// Creates a smoke bead, submits it to the queue, and asserts
	// run_started → run_completed + commit-on-branch + reviewer_verdict + bead_closed.
	// Exit codes: 0 pass, 1 failure, 2 timeout, 17 daemon not running.
	if len(os.Args) >= 2 && os.Args[1] == "smoke" {
		return runSmokeSubcommand(os.Args[2:])
	}

	// harmonik comms <verb> — agent-to-agent messaging surface (agent-comms spec §2.1 C2/C3).
	// Currently supports: comms send (T3), comms log (T5).
	// Exit code 17 = daemon not running (send only; log reads directly from events.jsonl).
	// Bead ref: hk-cnjhx (T3), hk-onn1x (T5).
	if len(os.Args) >= 2 && os.Args[1] == "comms" {
		return runCommsSubcommand(os.Args[2:])
	}

	// harmonik decisions <verb> — agent→human decision surface (hitl-decisions
	// SPEC §2; agent-side K2). raise/withdraw are daemon emit ops (exit 17 when
	// daemon down); wait is a pure client-side subscribe stream (§4 N8). The
	// operator side (list/show/answer) is component K4, a later bead.
	// Bead ref: hk-xz9 (K2).
	if len(os.Args) >= 2 && os.Args[1] == "decisions" {
		return runDecisionsSubcommand(os.Args[2:])
	}

	// harmonik captain — bare launcher for the Captain LLM session: mint/validate
	// a stable UUIDv4 --session-id and bring up `claude --remote-control` in a
	// tmux session. It is a launcher, not a daemon — it never touches the pidfile
	// lock. Bead ref: hk-ly0n.
	if len(os.Args) >= 2 && os.Args[1] == "captain" {
		return runCaptainSubcommand(os.Args[2:])
	}

	// harmonik start <role> … — the umbrella easy-start verb (codename:easy-start,
	// ES1/hk-kbjl). Owns ALL start-routing: enforces the positional-XOR-flags rule
	// (operator decision D2) then delegates to the captain launcher (above) or the
	// crew subcommand (below). The former `start captain` alias is folded in here.
	if len(os.Args) >= 2 && os.Args[1] == "start" {
		return runStart(os.Args[2:])
	}

	// harmonik crew <verb> — captain & crew session management (C2).
	// crew start/stop are daemon RPCs (exit 17 when daemon down).
	// crew list is a local read that works daemon-down.
	// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1.
	// Bead ref: hk-yj2j6 (C2 CLI).
	if len(os.Args) >= 2 && os.Args[1] == "crew" {
		return runCrewSubcommand(os.Args[2:])
	}

	// harmonik ops-monitor <verb> — launchd LaunchAgent management for the
	// ops-monitor-check.sh fleet health probe (hk-qpzsv). Verbs: install,
	// uninstall, status. Installs a per-project LaunchAgent so the probe runs
	// every 5 min independent of any Claude or captain session. No daemon required.
	if len(os.Args) >= 2 && os.Args[1] == "ops-monitor" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runOpsMonitorSubcommand(subArgs)
	}

	// harmonik schedule <verb> — generic recurring-job primitive (codename:schedule,
	// hk-0es). All verbs mutate/read .harmonik/schedules.json directly and work
	// whether or not the daemon is running; a running daemon picks up changes on
	// its next poll tick. No daemon connection required (no exit-17 path).
	if len(os.Args) >= 2 && os.Args[1] == "schedule" {
		return runScheduleSubcommand(os.Args[2:])
	}

	// harmonik sentinel <verb> — flywheel sentinel surface (flywheel V4, hk-9mr2).
	// Exposes governor-trip exception writes to the adversary crew and operators.
	// No daemon required; file-only operations.
	if len(os.Args) >= 2 && os.Args[1] == "sentinel" {
		return runSentinelSubcommand(os.Args[2:])
	}

	// harmonik greenlight <bead-id> [--project DIR] — captain approval for staged
	// deploy+verify beads (AC2, hk-lacr). Removes the "needs-greenlight" label so
	// the daemon's dispatch loop can claim the bead. No daemon required; calls br.
	// Spec ref: flywheel-motion.md §5.3/§6.2.
	if len(os.Args) >= 2 && os.Args[1] == "greenlight" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runGreenlightSubcommand(subArgs)
	}

	// harmonik goal-keeper [--project DIR] — ephemeral goal-state updater
	// (flywheel V6, hk-owz1). Reads operator comms since the last_event_id
	// cursor in .harmonik/intent/goal-state.json, appends new messages as
	// verbatim operator_directives, and exits. Spawned by the daemon's
	// schedule primitive on idle-triggered realign; also callable manually.
	// No daemon required.
	if len(os.Args) >= 2 && os.Args[1] == "goal-keeper" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runGoalkeeperSubcommand(subArgs)
	}

	// harmonik graph <verb> — workflow graph utilities (hk-voyf4).
	// Currently supports: graph validate <path>
	// No daemon required; reads files directly.
	if len(os.Args) >= 2 && os.Args[1] == "graph" {
		return runGraphSubcommand(os.Args[2:])
	}

	// harmonik promote <sha>... | --pr — banked-commit cherry-pick to target with
	// build gate + non-ff-safe push (push-mode), or PR opener (PR-mode).
	// No daemon required; operates directly on git and gh.
	// Spec ref: specs/promote.md. Bead ref: hk-pk3p1 (reconciles hk-gax8v).
	if len(os.Args) >= 2 && os.Args[1] == "promote" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runPromoteSubcommand(subArgs)
	}

	// harmonik release <verb> — release ledger management (hk-n7ofb).
	// ledger: list entries; certify: flip prerelease:false + stamp certified_at;
	// yank: mark yanked. No daemon required; operates on the ledger JSON file.
	// Spec ref: specs/release-pipeline.md §4, §6, §7.1.
	if len(os.Args) >= 2 && os.Args[1] == "release" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runReleaseSubcommand(subArgs)
	}

	// harmonik digest [--project DIR] [--json] [--since EVENT_ID] [--full]
	// Pure-Go status sheet builder; snapshot mode requires no daemon.
	// harmonik state [--json] — typed StateSnapshot aggregator (hk-gv04 P2-a).
	// Spec: specs/system-state.md §4.  Emits JSON or compact human table.
	// Daemon-up: live socket RPC ("state" op); daemon-down: disk fallback.
	if len(os.Args) >= 2 && os.Args[1] == "state" {
		return runStateSubcommand(os.Args[2:])
	}

	// Missing .harmonik/ → exit 7. Spec: specs/digest-command.md; CL-030..033.
	// Bead ref: hk-1qrty.
	if len(os.Args) >= 2 && os.Args[1] == "digest" {
		return runDigestSubcommand(os.Args[2:])
	}

	// harmonik project-hash [--project DIR] — read-only PL-006a hash printer.
	// Prints the first 12 hex chars of SHA-256(realpath(project_root)) and exits 0.
	// No daemon required; side-effect-free. Shell scripts use this to obtain the
	// project hash without reimplementing SHA-256 in bash.
	//
	// Exit codes: 0 success, 1 argument or path-resolution error.
	// Spec ref: specs/process-lifecycle.md §4.2 PL-031.
	// Bead ref: hk-dmw.
	if len(os.Args) >= 2 && os.Args[1] == "project-hash" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runProjectHashSubcommand(subArgs)
	}

	// harmonik remote-control-prefix [--project DIR] — read-only printer for the
	// per-project Claude RC label prefix (daemon.remote_control_prefix). No daemon
	// required; side-effect-free. Mirrors project-hash; fetches the prefix without
	// parsing YAML. Bead ref: hk-igpg.
	if len(os.Args) >= 2 && os.Args[1] == "remote-control-prefix" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runRemoteControlPrefixSubcommand(subArgs)
	}

	// harmonik migrate-rc-prefix [--project DIR] — interactive migration for
	// existing projects that pre-date daemon.remote_control_prefix. Prompts the
	// user for a slug (defaulting to the project's beads issue_prefix) and
	// writes it in-place to .harmonik/config.yaml. Satisfies §8.3 of the
	// rc-prefix plan (hk-f4w7): do NOT silently backfill; ask the user.
	if len(os.Args) >= 2 && os.Args[1] == "migrate-rc-prefix" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runMigrateRCPrefixSubcommand(subArgs)
	}

	// harmonik usage [--since DURATION|ISO] [--until ISO] [--format json|summary] [--project DIR]
	// Token cost analysis: join Claude transcripts × events.jsonl on run/<run_id>.
	// No daemon required; reads files directly.
	// Bead ref: hk-b89kk (Phase-0 token-usage join).
	if len(os.Args) >= 2 && os.Args[1] == "usage" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runUsageSubcommand(subArgs)
	}

	// harmonik eval <verb> — eval-harness tooling (EH1).
	// harmonik eval collect: post-run collector, reads events.jsonl,
	// writes per-run records to eval-results.jsonl.
	if len(os.Args) >= 2 && os.Args[1] == "eval" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runEvalCmd(subArgs, os.Stdout, os.Stderr)
	}

	// harmonik harness [flags] — scenario harness runner (hk-nwqa0).
	//
	// Implements the MVH CLI surface: 8 flags (--cadence, --scenario,
	// --fixture-root, --twin-search-path, --list, --dry-run, --output,
	// --verbose) and 5 exit codes (0/1/2/3/130).
	//
	// Spec ref: specs/scenario-harness.md §4.12 SH-032.
	if len(os.Args) >= 2 && os.Args[1] == "harness" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runHarnessSubcommand(subArgs)
	}

	// EV-019 / EV-019a: top-level panic recovery wired at the composition root.
	//
	// logFlusher and busFlusher are both nil for MVH:
	//   - logFlusher:  the structured-log channel does not exist yet; the flush
	//     step is nil-safe and skipped (lifecycle.RecoverWithLogFlush nil-safety).
	//   - busFlusher:  the EventBus (hk-hqwn.57) is not yet implemented; the
	//     bus-flush step is nil-safe per EV-019a. Substitute with the real
	//     EventBus once hk-hqwn.57 lands (wiring site: hk-hqwn.70).
	//
	// Spec refs:
	//   - event-model.md §4.4 EV-019  — log flush MUST precede exit on panic.
	//   - event-model.md §4.4 EV-019a — bus flush SHOULD follow log flush (nil-safe).
	defer lifecycle.RecoverWithLogFlush(nil, nil, nil)

	// PolicyEngine binding for MVH.
	//
	// NoOpPolicyEngine is the production interface — not a nil check, not a
	// test double. The dispatcher always calls policyEngine.Evaluate; the
	// no-op always returns {Permitted: true, Constraints: nil}.
	//
	// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5;
	// specs/scenario-harness.md §4.3.SH-018; bootstrap-subset.md §1.
	var policyEngine core.PolicyEngine = core.NoOpPolicyEngine{} //nolint:ineffassign // composition-root binding; dispatcher wiring is pending (hk-b3f.*)
	_ = policyEngine                                             // consumed by dispatcher once cluster-A EM beads land

	// TODO(hk-b3f): pass policyEngine to the EM dispatcher once the
	// dispatcher wiring beads (hk-b3f cluster-A) land. The binding site is
	// here; the consumer site is internal/orchestrator (not yet shipped).

	// --project flag (MVH_ROADMAP row #1, hk-56ajv).
	//
	// Default: current working directory. Resolved to an absolute path via
	// filepath.Abs before the directory-existence check, so relative paths
	// work intuitively from any shell context.
	//
	// MVH stays foreground — no additional flags, no env-var fallbacks, no
	// config-file loading (MVH_ROADMAP §"What we are NOT building for MVH").
	var projectFlag string
	flag.StringVar(&projectFlag, "project", "", "project directory (default: current working directory)")

	// --max-concurrent: maximum beads dispatched concurrently.
	// Default 1 preserves MVH single-threaded semantics (POST_MVH_PARALLELISM_ROADMAP row 6, hk-e61c3.1).
	// Values >1 are inert until the work-loop goroutine scheduler (hk-e61c3.2) lands.
	var maxConcurrentFlag int
	flag.IntVar(&maxConcurrentFlag, "max-concurrent", 1, "maximum number of beads dispatched concurrently")

	// --subscription-token-ceiling: per-5h token budget for the shared Claude
	// subscription.  When non-zero the bandwidth tuner (hk-ymav1) reads rolling
	// token usage from ~/.claude/projects/*/*.jsonl and auto-scales --max-concurrent
	// to stay within this ceiling.  Zero (the default) disables the tuner.
	// Start conservative and raise empirically until a 429 is observed.
	var subscriptionTokenCeilingFlag int64
	flag.Int64Var(&subscriptionTokenCeilingFlag, "subscription-token-ceiling", 0,
		"per-5h token ceiling for the Claude subscription; enables auto-tuning of --max-concurrent (0 = disabled)")

	// --workflow-mode: daemon-level default workflow mode (hk-rssrg).
	// Tier-3 of the four-tier resolution chain (execution-model.md §4.3 EM-012a):
	// per-bead label → per-project → daemon-default (this flag) → built-in fallback.
	// Defaults to "dot" so every bead with no explicit ref runs the embedded
	// standard-bead.dot workflow (implement → commit_gate → review → merge).
	// Pass --workflow-mode review-loop or --workflow-mode single to override.
	// Valid values: single, review-loop, dot.
	var workflowModeFlag string
	flag.StringVar(&workflowModeFlag, "workflow-mode", string(core.WorkflowModeDot),
		"daemon-level default workflow mode: single, review-loop, dot (default: dot)")

	// Queue-only is now the default (hk-8vy18): a bare boot with no submitted
	// queue dispatches zero runs. --auto-pull opts in to the historical br-ready
	// drain for non-queue-driven deployments. --no-auto-pull is kept as an
	// accepted no-op alias for back-compat (it was the opt-in; now queue-only is
	// the default so passing it is redundant but harmless).
	var autoPullFlag bool
	flag.BoolVar(&autoPullFlag, "auto-pull", false, "enable br-ready fallback poll (historical single-daemon topology; opt-in)")
	flag.BoolVar(new(bool), "no-auto-pull", false, "no-op alias; queue-only is now the default (back-compat)")

	// --target-branch: branch the daemon merges completed bead branches into
	// (default "main").  Threaded into mergeRunBranchToMain by codename:productization
	// beads (hk-mkxw1).
	var targetBranchFlag string
	flag.StringVar(&targetBranchFlag, "target-branch", "", "branch to merge completed bead branches into (default: main)")

	// --protect-branch: repeatable; names a branch the daemon must never merge
	// into or overwrite (hk-mkxw1).
	var protectBranchesFlag stringSliceFlag
	flag.Var(&protectBranchesFlag, "protect-branch", "branch name to protect from daemon merges (repeatable)")

	// --forbid-default-main: refuse to start when the repository default branch
	// is not in the protected set (hk-mkxw1).
	var forbidUnprotectedDefaultFlag bool
	flag.BoolVar(&forbidUnprotectedDefaultFlag, "forbid-default-main", false,
		"refuse to start if the default branch (main/master) is not in --protect-branch")

	// --default-harness: tier-4 (global) default for the harness-selection chain
	// (bead > queue > node > global, codex-harness C4/T4, hk-y01k6).
	// The empty default causes the daemon to fall back to the built-in default
	// (claude-code) per the tier-4 fallback in resolveHarness.
	// Valid values: "claude-code", "codex", or any registered AgentType (AR-025).
	var defaultHarnessFlag string
	flag.StringVar(&defaultHarnessFlag, "default-harness", "",
		"global default harness (tier-4): claude-code, codex (default: claude-code built-in fallback)")

	// --codex-binary: path to the codex executable used when the resolved harness
	// is codex.  Empty falls back to bare "codex" resolved by PATH (hk-y01k6).
	var codexBinaryFlag string
	flag.StringVar(&codexBinaryFlag, "codex-binary", "",
		"path to the codex executable (default: 'codex' resolved by PATH)")

	// --worker-host: CLI override for the single remote worker's host field.
	// When explicitly set, takes precedence over the value in .harmonik/workers.yaml
	// following flag > file > default precedence (B4 remote-substrate).
	var workerHostFlag string
	flag.StringVar(&workerHostFlag, "worker-host", "",
		"override the remote worker host (B4 remote-substrate; empty = use workers.yaml value)")

	// --worker-enabled: CLI override for the single remote worker's enabled field.
	// When explicitly set via --worker-enabled or --no-worker-enabled, takes
	// precedence over the value in .harmonik/workers.yaml (B4 remote-substrate).
	var workerEnabledFlag bool
	flag.BoolVar(&workerEnabledFlag, "worker-enabled", false,
		"override the remote worker enabled state (B4 remote-substrate)")

	// --agent-ready-timeout: HC-056 per-dispatch timeout for the agent_ready event.
	// The daemon kills and reopens the bead when the agent does not signal ready
	// within this window. Zero (the default) falls back to the compiled-in default
	// (90s as of hk-hzj). Increase for slow-disk / high-concurrency environments
	// where claude cold-start exceeds the default; decrease for fast NVMe boxes to
	// surface hung spawns sooner.
	//
	// Bead ref: hk-hzj.
	var agentReadyTimeoutFlag time.Duration
	flag.DurationVar(&agentReadyTimeoutFlag, "agent-ready-timeout", 0,
		"per-dispatch timeout for agent_ready event; 0 uses the built-in default (90s) (hk-hzj)")

	// --spawn-stagger: minimum interval between consecutive tmux window creations.
	// Under a concurrent dispatch burst all claude agents cold-start simultaneously,
	// competing for disk I/O and CPU. Spreading window creation by this interval
	// reduces the peak cold-start contention and lowers the probability of
	// agent_ready_timeout under disk pressure. Zero (the default) disables
	// staggering. A value of 2–5s is a reasonable starting point for
	// --max-concurrent ≥ 4 on a disk-heavy box.
	//
	// Bead ref: hk-hzj.
	var spawnStaggerFlag time.Duration
	flag.DurationVar(&spawnStaggerFlag, "spawn-stagger", 0,
		"minimum interval between consecutive agent window creations; 0 disables (hk-hzj)")

	flag.Usage = harmonikUsage
	flag.Parse()

	// Resolve project directory.
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}

	projectDir, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot resolve project path %q: %v\n", projectFlag, err)
		return 1
	}

	// Validate the directory exists. Fail fast with a clear message so the
	// operator knows immediately when they've pointed at a non-existent path.
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		return 1
	}

	// PL-004b (hk-rcp7): flag > config > default precedence for max_concurrent,
	// workflow_mode, and target_branch.
	//
	// flag.Visit iterates only the flags that were explicitly set on the command
	// line (not defaulted-and-not-passed).  An explicitly-passed flag always wins
	// over any config-file value; a defaulted-but-not-passed flag defers to the
	// config file, which in turn defers to the built-in default.
	explicitFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })

	// Load the daemon: block from .harmonik/config.yaml.  The loader validates
	// the workflow_mode value and returns *ErrWorkflowModeFloorViolation when
	// single is found (PL-004a review floor — daemon-level config must never
	// lower the mode below review-loop).
	projCfg, projCfgErr := daemon.LoadProjectConfig(projectDir)
	if projCfgErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", projCfgErr)
		return 1
	}

	// hk-f8u5j: Pi harness config validation at daemon boot (PI-051).
	// When harnesses.pi is present (any required field non-empty), validate it
	// fully via ResolvePiConfig so the operator sees a clear PiConfigMissingError
	// before the daemon accepts beads rather than at first Pi dispatch.
	// When harnesses.pi is entirely absent, no validation — Pi is simply
	// unavailable and buildPiLaunchSpec surfaces the error at dispatch time.
	piBlock := projCfg.Harnesses.Pi
	if piBlock.Provider != "" || piBlock.Model != "" || piBlock.APIKeyEnv != "" {
		if _, piCfgErr := ResolvePiConfig(piBlock, projectDir); piCfgErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik: %v\n", piCfgErr)
			return 1
		}
	}

	// Load branching.yaml for the authoritative target_branch precedence source
	// (config.yaml daemon.target_branch is observability/symmetry only per PL-004b).
	branchDflt, branchErr := branching.Load(projectDir)
	if branchErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", branchErr)
		return 1
	}

	// Load workers.yaml for the remote-substrate worker registry (B4).
	// Missing file → zero-value Config (local execution only); malformed → fatal.
	workersCfg, workersErr := workers.Load(projectDir)
	if workersErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", workersErr)
		return 1
	}
	// Apply CLI overrides: flag > file > default (B4).
	workersCfg = applyWorkerOverrides(workersCfg, explicitFlags, workerHostFlag, workerEnabledFlag)

	// Resolve max_concurrent: explicit flag > config (> 0) > flag default (1).
	if !explicitFlags["max-concurrent"] && projCfg.Daemon.MaxConcurrent > 0 {
		maxConcurrentFlag = projCfg.Daemon.MaxConcurrent
	}

	// Resolve workflow_mode: explicit flag > config (non-empty, already validated) > flag default (dot).
	// The loader already enforces the PL-004a review floor on the config value.
	if !explicitFlags["workflow-mode"] && projCfg.Daemon.WorkflowMode != "" {
		workflowModeFlag = string(projCfg.Daemon.WorkflowMode)
	}

	// Resolve target_branch: explicit flag > branching.yaml lands_on > flag default ("").
	// The daemon normalises empty → "main" via resolveTargetBranch.
	if !explicitFlags["target-branch"] && branchDflt.LandsOn != "" {
		targetBranchFlag = branchDflt.LandsOn
	}

	// hk-sm6j7: resolve br binary via PATH so the work loop is reachable.
	// If br is not on PATH, BrPath remains empty and daemon.Start skips the
	// work loop (existing nil-path guard at daemon.go:251 is preserved).
	brPath, _ := exec.LookPath("br")

	// hk-9321v: resolve kerf binary via PATH for EM-062/EM-063 eager-refill.
	// If kerf is not on PATH, KerfPath remains empty and eager-refill is disabled.
	kerfPath, _ := exec.LookPath("kerf")

	// hk-keul6: default JSONL log path to <ProjectDir>/.harmonik/events/events.jsonl
	// per event-model.md §6.2 EV-020.
	jsonlLogPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	// hk-woebv: create required subdirectories before daemon.Start so that
	// eventbus.OpenJSONLWriter never fails with "no such file or directory".
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot create .harmonik/events/: %v\n", err)
		return 1
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot create .harmonik/beads-intents/: %v\n", err)
		return 1
	}

	// hk-002zx: startup banner so the operator knows the daemon is active.
	fmt.Fprintln(os.Stderr, "harmonik daemon starting in", projectDir)

	// F56 (hk-86eh): two-phase shutdown — separate dispatch-halt from in-flight cancel.
	//
	// ctx (from signal.NotifyContext) is cancelled immediately on SIGINT/SIGTERM.
	// It is used ONLY as StopDispatchCtx: it halts new dispatch without touching
	// in-flight DOT/implement goroutines, which run on runCtx below.
	//
	// runCtx is passed to daemon.Start. It is independent of signals; the grace
	// goroutine cancels it after inFlightDrainGrace once ctx fires, bounding
	// the window in which a hung implement node can delay process exit.
	// In practice the supervisor sends SIGKILL within its StopTimeout (10 s)
	// before the grace fires; either way, goroutines never see a cancelled
	// context and never emit 'context cancelled during node implement'.
	// QM-002a on the next daemon start resets in-progress beads to open.
	//
	// Signal handling lives at the composition root (hk-7oz2f) so daemon.Start
	// is testable without process-level signals.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go inFlightDrainGoroutine(ctx, runCtx, cancelRun)

	// hk-kqdpf.6: resolve the absolute path to this binary so that settings.json
	// hook commands reference an absolute path rather than a bare "harmonik" name.
	// Fail fast here so the daemon never starts with an unresolvable hook command.
	daemonBinaryPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: os.Executable() failed — cannot resolve daemon binary path for hook commands: %v\n", err)
		return 1
	}

	// hk-kqdpf.4: wire tmuxSubstrate into the daemon composition root.
	//
	// Fail fast when $TMUX is not set: the daemon requires an active tmux session
	// so that handler subprocesses appear as new windows inside that session.
	// The user may run from any existing session — the prefix-enforcement done by
	// hk tmux-start (PL-006a) applies only to that subcommand, not to daemon start.
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "harmonik: $TMUX is not set — run hk inside a tmux session or via hk tmux-start")
		return 1
	}

	// hk-9vp51 + hk-u9ji: resolve the implementer spawn-target session at boot
	// from the LIVE session the daemon runs inside, then EXCLUDE system sessions
	// that must not receive implementer windows.
	//
	// We ask tmux for the current session via `display-message -p
	// '#{session_name}'` (exec.Command, not OSAdapter, because no window handle
	// exists yet). That returns whatever session the daemon was launched inside:
	//   - operator's `hk tmux-start` session, or an ambient `harmonik` session →
	//     use it verbatim; it provably exists right now so SpawnWindow can never
	//     hit "session does not exist".
	//   - the old per-project supervisor `hk-daemon-supervise` session, OR the
	//     new flywheel shim session `harmonik-<hash>-flywheel` (daemon inherited
	//     $TMUX on supervisor-revive via DaemonWatchdog — see hk-u9ji) →
	//     fall back to the deterministic per-project DefaultSessionName and
	//     EnsureSession it, so implementer windows land in the daemon's own
	//     session, NOT the system session.
	//
	// This deliberately does NOT switch the whole mechanism to a boot-time
	// deterministic name (the original sub-fix #3 did that and the created
	// session did not persist to dispatch time → every spawn failed in 0.6s,
	// reverted fe94e0b1). We keep the always-exists live session and only depart
	// from it for the unusable system-session cases.
	liveSession := ""
	if out, dmErr := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output(); dmErr != nil { //nolint:gosec // G204: arguments are hard-coded constants
		// display-message failure is non-fatal: ResolveDaemonSpawnSession treats
		// an empty live session as "force fallback to the ensured daemon session".
		fmt.Fprintf(os.Stderr, "harmonik: tmux display-message failed (%v); falling back to deterministic daemon session\n", dmErr)
	} else {
		liveSession = strings.TrimSpace(string(out))
	}
	sessionName, needEnsureSession := tmux.ResolveDaemonSpawnSession(projectDir, liveSession)

	// Probe tmux version (≥ 3.0 required for -e env-injection per PL-021b).
	tmuxAdapter := tmux.OSAdapter{}
	if probeErr := tmuxAdapter.ProbeTmux(ctx); probeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: tmux probe failed: %v\n", probeErr)
		return 1
	}

	// hk-9vp51 + hk-u9ji: when we fell back to the deterministic daemon-owned
	// session (live session was the supervisor's, the flywheel's, or
	// display-message failed), ensure it exists BEFORE constructing the substrate.
	// A detached session with a live shell persists for the daemon's whole
	// lifetime, and the #4 coordinator reaper only targets "-flywheel" sessions
	// (never this "-default" one), so it is guaranteed present at dispatch time.
	// When we kept the live session (needEnsureSession=false) it already exists —
	// we are running inside it — so we must NOT re-create it.
	if needEnsureSession {
		if ensErr := tmuxAdapter.EnsureSession(ctx, sessionName, projectDir); ensErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik: cannot ensure daemon tmux session %q: %v\n", sessionName, ensErr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "harmonik: spawning implementer windows into daemon-owned session %q (ambient session was supervisor/flywheel/empty)\n", sessionName)
	}

	// hk-xb5yi: resolve spawn cap. HARMONIK_MAX_CONCURRENT_SESSIONS env var
	// overrides; default is maxConcurrentFlag*2 to cover both implementer and
	// reviewer sessions per in-flight bead.
	maxSessions := spawnCapFromEnv(maxConcurrentFlag)

	// hk-9ptu: build substrate options; add session keepalive when the daemon owns
	// the session (needEnsureSession=true → supervisor-revive or display-message
	// failure boot path). On this path the daemon is responsible for keeping the
	// "-default" session alive for its entire lifetime. The keepalive goroutine
	// complements the reactive hk-yaj ErrNoSession self-heal in SpawnWindow by
	// proactively recreating the session between dispatches so a killed session
	// does not cause a fleet-wide launch_initiated outage.
	substrateOpts := []daemon.TmuxSubstrateOption{
		daemon.WithSpawnCap(maxSessions),
		daemon.WithSpawnStagger(spawnStaggerFlag),                            // hk-hzj: spread concurrent cold-starts; 0 = disabled
		daemon.WithCrewProjectHash(lifecycle.ComputeProjectHash(projectDir)), // fleet-portability T2
	}
	if needEnsureSession {
		substrateOpts = append(substrateOpts, daemon.WithSessionKeepalive(0)) // 0 = default 30 s interval
	}

	// Resolve the binary's commit hash once: ldflags stamp takes precedence;
	// runtime/debug VCS embedding is the fallback for plain go install builds.
	// Cite: bead hk-v3nv (TA4 tokenaudit — unblocks version<->cost correlation).
	resolvedHash := resolvedCommitHash()

	cfg := daemon.Config{
		ProjectDir:               projectDir,
		BrPath:                   brPath,
		KerfPath:                 kerfPath, // hk-9321v: kerf next for EM-062/EM-063 eager-refill
		JSONLLogPath:             jsonlLogPath,
		MaxConcurrent:            maxConcurrentFlag,
		NoAutoPull:               !autoPullFlag, // hk-8vy18: queue-only by default; --auto-pull opts in to br-ready drain
		Substrate:                daemon.NewTmuxSubstrate(tmuxAdapter, sessionName, substrateOpts...),
		DaemonBinaryPath:         daemonBinaryPath,                    // absolute path for hook commands (hk-kqdpf.6)
		BinaryCommitHash:         resolvedHash,                        // ldflags stamp or runtime/debug fallback (hk-mz0x4, hk-v3nv)
		AgentReadyTimeout:        agentReadyTimeoutFlag,               // hk-hzj: per-dispatch ready timeout; 0 = built-in default (90s)
		SubscriptionTokenCeiling: subscriptionTokenCeilingFlag,        // hk-ymav1: bandwidth auto-tuner
		WorkflowModeDefault:      core.WorkflowMode(workflowModeFlag), // hk-30vlb: default to dot (embedded standard-bead.dot)
		TargetBranch:             targetBranchFlag,                    // hk-mkxw1: merge target branch
		ProtectBranches:          []string(protectBranchesFlag),       // hk-mkxw1: branches protected from daemon merges
		ForbidUnprotectedDefault: forbidUnprotectedDefaultFlag,        // hk-mkxw1: guard against unprotected default branch
		DefaultHarness:           core.AgentType(defaultHarnessFlag),  // hk-y01k6: tier-4 harness default
		CodexBinary:              codexBinaryFlag,                     // hk-y01k6: codex executable path
		Workers:                  workersCfg,                          // hk-rs-b4-bootwire-b44z: remote-substrate worker registry
	}

	// Yanked-binary check (specs/release-pipeline.md §7.2 point 4).
	//
	// Belt-and-suspenders over the supervisor guard: if the compiled-in ledger or
	// the on-disk ledger marks this binary's commit hash as yanked, exit 9 with a
	// clear FATAL message so operators know immediately why the daemon refused to start.
	//
	// Only applies to the daemon path (after subcommand dispatch), so operator
	// tools like `harmonik release rollback` remain usable even on a yanked binary.
	//
	// Exit code 9 = yanked-binary per spec §7.2.4.
	if resolvedHash != "unknown" && resolvedHash != "" {
		// Check compiled-in ledger.
		for _, e := range release.Ledger {
			if e.CommitHash == resolvedHash && e.Yanked {
				fmt.Fprintf(os.Stderr, "FATAL: this binary (%s, %s) has been yanked: %s\n",
					e.Semver, resolvedHash, e.YankedReason)
				return 9
			}
		}
		// Check on-disk (mutable) ledger.
		if onDiskEntries, ldErr := release.LoadLedgerFile(release.LedgerPath(projectDir)); ldErr == nil {
			for _, e := range onDiskEntries {
				if e.CommitHash == resolvedHash && e.Yanked {
					fmt.Fprintf(os.Stderr, "FATAL: this binary (%s, %s) has been yanked: %s\n",
						e.Semver, resolvedHash, e.YankedReason)
					return 9
				}
			}
		}
	}

	// Supervisor watchdog: daemon-side liveness monitor for the flywheel supervisor
	// (hk-dqlkz). When the supervisor is found dead the daemon revives it via
	// 'harmonik supervise restart --watch-restart', closing the gap where the
	// supervisor's own DaemonWatchdog dies with it and leaves no auto-revive path
	// for the daemon itself (hk-pen9: 7h11m undetected outage).
	{
		swLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		sw := supervise.NewSupervisorWatchdog(buildSupervisorWatchdogSpec(projectDir, daemonBinaryPath), swLog)
		go func() {
			if err := sw.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "supervisor-watchdog: exited: %v\n", err)
			}
		}()
	}

	// F56 (hk-86eh): wire signal ctx as StopDispatchCtx so SIGTERM halts new
	// dispatch immediately; in-flight goroutines continue on runCtx.
	cfg.StopDispatchCtx = ctx

	// hk-b6m3h: map lifecycle.ErrPidfileLocked → exit code 5 per PL-008a.
	// All other errors map to exit code 1.
	if err := daemon.Start(runCtx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", err)
		if errors.Is(err, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	return 0
}

// stringSliceFlag is a flag.Value implementation for repeatable string flags
// such as --protect-branch.  Each invocation of Set appends one value; the
// zero value (nil slice) is safe and means "no values provided".
//
// Bead ref: hk-mkxw1.
type stringSliceFlag []string

func (f *stringSliceFlag) String() string { return strings.Join(*f, ",") }
func (f *stringSliceFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// inFlightDrainGrace is the bounded window the persistent daemon gives
// in-flight DOT/implement goroutines to complete after SIGTERM fires (F56,
// hk-86eh). After this duration runCtx is cancelled, which surfaces as an
// error in still-running goroutines. In practice the supervisor (or the
// operator's shell) sends SIGKILL within its own StopTimeout (10 s) before
// this timer fires; the constant exists to ensure the process exits eventually
// even without an external kill.
const inFlightDrainGrace = 5 * time.Minute

// inFlightDrainGoroutine implements the F56 (hk-86eh) two-phase shutdown:
// wait for sigCtx to fire, then cancel runCtx after inFlightDrainGrace. If
// runCtx is cancelled first (normal exit), the goroutine exits immediately.
func inFlightDrainGoroutine(sigCtx, runCtx context.Context, cancelRun context.CancelFunc) {
	select {
	case <-sigCtx.Done():
		t := time.NewTimer(inFlightDrainGrace)
		defer t.Stop()
		select {
		case <-t.C:
			cancelRun()
		case <-runCtx.Done():
		}
	case <-runCtx.Done():
	}
}

// buildSupervisorWatchdogSpec returns the SupervisorWatchdogSpec used by the
// daemon-side supervisor liveness watchdog (hk-dqlkz). Factored out for
// testability.
func buildSupervisorWatchdogSpec(projectDir, binaryPath string) supervise.SupervisorWatchdogSpec {
	return supervise.SupervisorWatchdogSpec{
		PidfilePath: filepath.Join(projectDir, ".harmonik", "cognition", "supervisor.pid"),
		ReviveCmd:   []string{binaryPath, "supervise", "restart", "--watch-restart", "--project", projectDir},
		WorkDir:     projectDir,
		MaxRevives:  3,
	}
}

// spawnCapFromEnv resolves the concurrent-session spawn ceiling (hk-xb5yi).
//
// Precedence:
//  1. HARMONIK_MAX_CONCURRENT_SESSIONS env var (operator override).
//  2. maxConcurrent*2 — default covering both implementer and reviewer per bead.
//
// Returns 0 when HARMONIK_MAX_CONCURRENT_SESSIONS is set to "0" (disables cap).
func spawnCapFromEnv(maxConcurrent int) int {
	if v := os.Getenv("HARMONIK_MAX_CONCURRENT_SESSIONS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			return n
		}
	}
	if maxConcurrent <= 0 {
		return 0
	}
	return maxConcurrent * 2
}
