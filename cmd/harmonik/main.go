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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/hookrelay"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
	"github.com/gregberns/harmonik/internal/release"
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
		fmt.Printf("harmonik %s (commit: %s)\n", version, commitHash)
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
			case "set-dispatching":
				return runKeeperSetDispatching(subArgs[1:])
			case "clear-dispatching":
				return runKeeperClearDispatching(subArgs[1:])
			case "enable":
				return runKeeperEnableSubcommand(subArgs[1:])
			case "doctor":
				return runKeeperDoctorSubcommand(subArgs[1:])
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

	// harmonik crew <verb> — captain & crew session management (C2).
	// crew start/stop are daemon RPCs (exit 17 when daemon down).
	// crew list is a local read that works daemon-down.
	// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1.
	// Bead ref: hk-yj2j6 (C2 CLI).
	if len(os.Args) >= 2 && os.Args[1] == "crew" {
		return runCrewSubcommand(os.Args[2:])
	}

	// harmonik schedule <verb> — generic recurring-job primitive (codename:schedule,
	// hk-0es). All verbs mutate/read .harmonik/schedules.json directly and work
	// whether or not the daemon is running; a running daemon picks up changes on
	// its next poll tick. No daemon connection required (no exit-17 path).
	if len(os.Args) >= 2 && os.Args[1] == "schedule" {
		return runScheduleSubcommand(os.Args[2:])
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

	// Build a context that is cancelled on SIGINT or SIGTERM so the work loop
	// shuts down cleanly. Signal handling lives at the composition root
	// (hk-7oz2f) so daemon.Start is testable without process-level signals.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	// hk-9vp51 (fix-forward, option (a)): resolve the implementer spawn-target
	// session at DISPATCH/boot time from the LIVE session the daemon runs inside
	// — which always exists — then EXCLUDE only the supervisor's own session.
	//
	// We ask tmux for the current session via `display-message -p
	// '#{session_name}'` (exec.Command, not OSAdapter, because no window handle
	// exists yet). That returns whatever session the daemon was launched inside:
	//   - operator's `hk tmux-start` session, or an ambient `harmonik` session →
	//     use it verbatim; it provably exists right now so SpawnWindow can never
	//     hit "session does not exist".
	//   - the supervisor's `hk-daemon-supervise` session (the daemon inherited
	//     $TMUX from /tmp/hk-daemon-supervise.sh) → fall back to the deterministic
	//     per-project DefaultSessionName and EnsureSession it, so implementer
	//     windows land in the daemon's own session, NOT the supervisor's.
	//
	// This deliberately does NOT switch the whole mechanism to a boot-time
	// deterministic name (the original sub-fix #3 did that and the created
	// session did not persist to dispatch time → every spawn failed in 0.6s,
	// reverted fe94e0b1). We keep the always-exists live session and only depart
	// from it for the one unusable case.
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

	// hk-9vp51: when we fell back to the deterministic daemon-owned session
	// (live session was the supervisor's, or display-message failed), ensure it
	// exists BEFORE constructing the substrate. A detached session with a live
	// shell persists for the daemon's whole lifetime, and the #4 coordinator
	// reaper only targets "-flywheel" sessions (never this "-default" one), so it
	// is guaranteed present at dispatch time. When we kept the live session
	// (needEnsureSession=false) it already exists — we are running inside it — so
	// we must NOT re-create it.
	if needEnsureSession {
		if ensErr := tmuxAdapter.EnsureSession(ctx, sessionName, projectDir); ensErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik: cannot ensure daemon tmux session %q: %v\n", sessionName, ensErr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "harmonik: spawning implementer windows into daemon-owned session %q (ambient session was supervisor/empty)\n", sessionName)
	}

	// hk-xb5yi: resolve spawn cap. HARMONIK_MAX_CONCURRENT_SESSIONS env var
	// overrides; default is maxConcurrentFlag*2 to cover both implementer and
	// reviewer sessions per in-flight bead.
	maxSessions := spawnCapFromEnv(maxConcurrentFlag)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		BrPath:        brPath,
		KerfPath:      kerfPath, // hk-9321v: kerf next for EM-062/EM-063 eager-refill
		JSONLLogPath:  jsonlLogPath,
		MaxConcurrent: maxConcurrentFlag,
		NoAutoPull:    !autoPullFlag, // hk-8vy18: queue-only by default; --auto-pull opts in to br-ready drain
		Substrate: daemon.NewTmuxSubstrate(tmuxAdapter, sessionName,
			daemon.WithSpawnCap(maxSessions),
			daemon.WithSpawnStagger(spawnStaggerFlag), // hk-hzj: spread concurrent cold-starts; 0 = disabled
		),
		DaemonBinaryPath:         daemonBinaryPath,                    // absolute path for hook commands (hk-kqdpf.6)
		BinaryCommitHash:         commitHash,                          // stamped via -ldflags at build time (hk-mz0x4)
		AgentReadyTimeout:        agentReadyTimeoutFlag,               // hk-hzj: per-dispatch ready timeout; 0 = built-in default (90s)
		SubscriptionTokenCeiling: subscriptionTokenCeilingFlag,        // hk-ymav1: bandwidth auto-tuner
		WorkflowModeDefault:      core.WorkflowMode(workflowModeFlag), // hk-30vlb: default to dot (embedded standard-bead.dot)
		TargetBranch:             targetBranchFlag,                    // hk-mkxw1: merge target branch
		ProtectBranches:          []string(protectBranchesFlag),       // hk-mkxw1: branches protected from daemon merges
		ForbidUnprotectedDefault: forbidUnprotectedDefaultFlag,        // hk-mkxw1: guard against unprotected default branch
		DefaultHarness:           core.AgentType(defaultHarnessFlag),  // hk-y01k6: tier-4 harness default
		CodexBinary:              codexBinaryFlag,                     // hk-y01k6: codex executable path
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
	if commitHash != "unknown" && commitHash != "" {
		// Check compiled-in ledger.
		for _, e := range release.Ledger {
			if e.CommitHash == commitHash && e.Yanked {
				fmt.Fprintf(os.Stderr, "FATAL: this binary (%s, %s) has been yanked: %s\n",
					e.Semver, commitHash, e.YankedReason)
				return 9
			}
		}
		// Check on-disk (mutable) ledger.
		if onDiskEntries, ldErr := release.LoadLedgerFile(release.LedgerPath(projectDir)); ldErr == nil {
			for _, e := range onDiskEntries {
				if e.CommitHash == commitHash && e.Yanked {
					fmt.Fprintf(os.Stderr, "FATAL: this binary (%s, %s) has been yanked: %s\n",
						e.Semver, commitHash, e.YankedReason)
					return 9
				}
			}
		}
	}

	// hk-b6m3h: map lifecycle.ErrPidfileLocked → exit code 5 per PL-008a.
	// All other errors map to exit code 1.
	if err := daemon.Start(ctx, cfg); err != nil {
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
