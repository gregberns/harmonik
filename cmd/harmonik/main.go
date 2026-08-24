// Command harmonik is the production daemon binary for harmonik.
//
// # Composition root
//
// This file is the composition root: it constructs every production dependency
// and wires them together before handing control to the daemon's run loop.
// Production bindings declared here:
//
//   - PolicyEngine: [core.NoOpPolicyEngine] — permits every evaluation with no
//     constraints. This is the first-class production binding; it is
//     NOT a nil sentinel and NOT a test double. The orchestrator dispatcher
//     calls PolicyEngine.Evaluate on every gate and guard without branching on
//     the concrete type, satisfying [specs/scenario-harness.md §4.3.SH-018].
//
//   - BusFlusher: nil for now. The [lifecycle.BusFlusher] interface is declared;
//     its real implementation ([lifecycle.BusFlusher] on the EventBus type) lands
//     when the EventBus bead (hk-hqwn.57) merges. Until then the bus-flush step
//     in [lifecycle.RecoverWithLogFlush] is skipped (nil-safe per EV-019a).
//     Wiring site (hk-hqwn.70): substitute nil with the real EventBus once
//     hk-hqwn.57 lands.
//
// When the control-points subsystem (hk-a8bg) lands later, the composition
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
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/branching"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/hookrelay"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
	"github.com/gregberns/harmonik/internal/release"
	"github.com/gregberns/harmonik/internal/supervise"
	"github.com/gregberns/harmonik/internal/workers"
)

func main() {
	os.Exit(run())
}

const queueTopUsage = `harmonik queue — submit or inspect the bead queue

USAGE
  harmonik queue <verb> [flags]

VERBS
  submit    Submit a new bead to the queue (daemon must be running)
  append    Append a bead to an existing queue run (daemon must be running)
  status    Show current queue state and bead statuses (daemon must be running)
  list      List all active queues with status and worker counts (daemon must be running)
  pause     Pause a named queue (daemon must be running)
  resume    Release a DRAIN pause on a named queue (daemon must be running)
  recover   Re-arm the failed items of a queue paused by FAILURE (daemon must be running)
  dry-run   Validate a queue submission without executing (daemon must be running)
  cancel    Archive a stale queue.json without a live daemon (no daemon required)
  set-concurrency <n>  Set the daemon's concurrent-dispatch ceiling live (daemon must be running)
  readiness Capture or judge the evidence that a dogfood run is safe to start (no daemon required)

NOTES
  Most verbs require the daemon to be running.
  'cancel' works without a live daemon — use it to clear a queue left by a
  killed daemon (e.g. after SIGTERM of a wedged harmonik process).
  'readiness' also works without a live daemon, and is meant to: the readiness
  gate runs before anyone starts one. Run 'harmonik queue readiness --help'.
  Exit code 17 means the daemon is not running (socket absent or ECONNREFUSED).
  Queues are created automatically on first submit to a new name (--queue flag).
  Absent --queue defaults to the 'main' queue.
  'resume' and 'recover' are not interchangeable. A queue stops for two
  different reasons. A drain pause holds dispatch and 'resume' releases it. A
  failure pause also marks the failed items, so it needs 'recover', which
  re-arms them. 'resume' against a failure-paused queue is refused and names
  'recover'.

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
  harmonik queue recover investigate
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

//nolint:gocognit,cyclop,funlen // The command router keeps the complete CLI dispatch table in one place.
func run() int {
	if len(os.Args) >= 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		harmonikUsage()
		return 0
	}

	if len(os.Args) >= 2 && os.Args[1] == "session-bootstrap" {
		return runSessionBootstrap(os.Args[2:], os.Getenv, os.Environ, resolveSessionBootstrapExecutable, sessionBootstrapDial, sessionBootstrapExec, os.Stderr)
	}

	if len(os.Args) >= 2 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-version") {
		if versionArgsRouteToInspect(os.Args) {
			return runVersionInspect(os.Args[2:], os.Stdout, os.Stderr)
		}
		fmt.Printf("harmonik %s (commit: %s)\n", version, resolvedCommitHash())
		return 0
	}

	if len(os.Args) >= 2 && os.Args[1] == "tmux-start" {
		subArgs := os.Args[2:]
		for _, arg := range subArgs {
			if arg == "--help" || arg == "-h" {
				fmt.Print(`harmonik tmux-start — create a detached tmux session and attach to it

This verb starts no daemon. It creates the session with one window running your
login shell in the project directory, then replaces itself with
'tmux attach-session'. To start a daemon, run 'harmonik start daemon' inside
the session.

USAGE
  harmonik tmux-start [--session-name NAME] [--project DIR]

FLAGS
  --session-name NAME  tmux session name. The default is
                       harmonik-<project-hash>-default. A name you supply must
                       start with 'harmonik-<project-hash>-', or the command
                       exits 24. Run 'harmonik project-hash' for the current
                       directory, or 'harmonik project-hash --project DIR' for
                       another one. That verb ignores a bare positional path.

  --project DIR        Project directory (default: current working directory)

EXIT
  0   the session is ready, or you were already inside tmux and nothing was done
  22  tmux is missing, or the tmux probe failed
  24  any other unrecoverable failure. The known causes are a session name
      without the required prefix, a project path that cannot be resolved, a
      working directory that cannot be read, tmux refusing to create the
      session, and a failed exec of 'tmux attach-session'.

  On the success path this process is replaced by 'tmux attach-session', so the
  status you finally see is tmux's, not harmonik's.

EXAMPLES
  harmonik tmux-start
  harmonik tmux-start --project /path/to/project
  harmonik tmux-start --session-name "harmonik-$(harmonik project-hash)-scratch"
`)
				return 0
			}
		}
		sessionNameFlag := ""
		projectDirFlag := ""
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

	if len(os.Args) >= 2 && os.Args[1] == "hook-relay" {
		if len(os.Args) >= 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			fmt.Print(`harmonik hook-relay — forward a Claude hook event to the daemon (internal use)

USAGE
  harmonik hook-relay <event-kind>

ARGUMENTS
  event-kind  The Claude hook event type. The relay sends four kinds to the
              daemon: SessionStart, Stop, StopFailure and Notification. It
              knows SessionEnd and does nothing with it. It accepts every
              other kind and does nothing with it.

NOTES
  Claude Code hook configurations call this subcommand. An operator rarely
  calls it directly.
  The relay reads the hook JSON on stdin. It sends one message to the daemon
  socket that HARMONIK_DAEMON_SOCKET names.
  A shell where every HARMONIK_* variable is UNSET is not a harmonik-managed
  session. In that shell the relay sends nothing and exits 0. A call you type
  yourself takes this path. Exporting even one of those names, with a value or
  empty, makes the shell harmonik-managed. A required variable that is
  exported but empty is then broken wiring, and the relay exits 1.
  The daemon must run to receive an event. The relay looks at the daemon only
  on a path that has an event to send. A socket that is not listening yet is a
  daemon still starting, so the relay retries it for up to 25 seconds and then
  exits 1. A dial that can never succeed, such as a path that is not a socket,
  exits 1 at once with no wait. On a path that sends nothing the relay does not
  look at the daemon at all.

EXAMPLES
  harmonik hook-relay Stop
  harmonik hook-relay SessionStart

EXIT CODES
  0   The relay sent the event. Also 0 when it had nothing to send: SessionEnd,
      an event kind it does not know, or a shell where every HARMONIK_*
      variable is unset.
  1   Missing event-kind argument, broken harmonik wiring, unreadable or
      invalid hook JSON on stdin, a session id that disagrees with
      HARMONIK_CLAUDE_SESSION_ID, an event kind that disagrees with the one on
      the command line, or a daemon socket that did not answer.
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

	if len(os.Args) >= 2 && os.Args[1] == "init" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runInitSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "sync-assets" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runSyncAssetsSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "reconcile" {
		return runReconcileSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "confirm-verdict" {
		return runConfirmVerdictSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "veto-verdict" {
		return runVetoVerdictSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "write-review-verdict" {
		return runWriteReviewVerdictSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "commit-msg" {
		return runCommitMsgSubcommand(context.Background(), os.Args[2:], os.Stdout, os.Stderr)
	}

	if len(os.Args) >= 2 && os.Args[1] == "beads-merge" {
		return runBeadsMergeSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "beads-dedup" {
		return runBeadsDedupSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "sleep" {
		subArgs := os.Args[2:]
		return runSleepSubcommand(context.Background(), subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "sleep-gate" {
		return runSleepGateSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "wake" {
		subArgs := os.Args[2:]
		return runWakeSubcommand(context.Background(), subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "queue" {
		verb := ""
		if len(os.Args) >= 3 {
			verb = os.Args[2]
		}
		if verb == "--help" || verb == "-h" {
			fmt.Print(queueTopUsage)
			return 0
		}
		subArgs := []string{}
		if len(os.Args) >= 4 {
			subArgs = os.Args[3:]
		}
		if len(subArgs) >= 1 && verb != "readiness" && (subArgs[0] == "--help" || subArgs[0] == "-h") {
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
		case "recover":
			return queuecli.RunQueueRecover(ctx, subArgs, os.Stdout, os.Stderr)
		case "dry-run":
			return queuecli.RunQueueDryRun(ctx, subArgs, os.Stdout, os.Stderr)
		case "cancel":
			return queuecli.RunQueueCancel(ctx, subArgs, os.Stdout, os.Stderr)
		case "set-concurrency":
			return queuecli.RunQueueSetConcurrency(ctx, subArgs, os.Stdout, os.Stderr)
		case "readiness":
			return runQueueReadiness(ctx, subArgs, os.Stdout, os.Stderr)
		default:
			fmt.Fprintf(os.Stderr, "harmonik queue: unrecognised verb %q; verbs are: submit, append, status, list, pause, resume, recover, readiness, dry-run, cancel, set-concurrency\n", verb)
			return 2
		}
	}

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

	if len(os.Args) >= 2 && os.Args[1] == "handler" {
		return runHandlerSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "run" {
		return runBeadSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "keeper" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
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
			case "restart-driver":
				return runKeeperRestartDriver(subArgs[1:])
			case "ping":
				return runKeeperPing(subArgs[1:])
			case "await-ack":
				return runKeeperAwaitAck(subArgs[1:])
			default:
				if !strings.HasPrefix(subArgs[0], "-") {
					fmt.Fprintf(os.Stderr, "harmonik keeper: unknown keeper subcommand %q\n\n", subArgs[0])
					fmt.Fprint(os.Stderr, keeperTopUsage)
					return 2
				}
			}
		}
		for _, arg := range subArgs {
			if arg == "--help" || arg == "-h" {
				fmt.Print(keeperTopUsage)
				return 0
			}
		}
		return runKeeperSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "supervise" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runSuperviseSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "subscribe" {
		return runSubscribeSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "smoke" {
		return runSmokeSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "comms" {
		return runCommsSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "decisions" {
		return runDecisionsSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "mailbox" {
		return runMailboxSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "captain" {
		return runCaptainSubcommand(os.Args[2:])
	}

	startDaemonRequested := false
	if len(os.Args) >= 3 && os.Args[1] == "start" && os.Args[2] == "daemon" {
		daemonArgs := os.Args[3:]
		if len(daemonArgs) >= 1 && (daemonArgs[0] == "--help" || daemonArgs[0] == "-h") {
			harmonikUsage()
			return 0
		}
		startDaemonRequested = true
		os.Args = append([]string{os.Args[0]}, daemonArgs...)
	}

	if len(os.Args) >= 2 && os.Args[1] == "start" {
		return runStart(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "crew" {
		return runCrewSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "agent" {
		return runAgentSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "ops-monitor" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runOpsMonitorSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "schedule" {
		return runScheduleSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "sentinel" {
		return runSentinelSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "greenlight" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runGreenlightSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "goal-keeper" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runGoalkeeperSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "graph" {
		return runGraphSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "promote" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runPromoteSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "gc" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runGCSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "release" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runReleaseSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "state" {
		return runStateSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "dashboard" {
		return runDashboardSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "digest" {
		return runDigestSubcommand(os.Args[2:])
	}

	if len(os.Args) >= 2 && os.Args[1] == "project-hash" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runProjectHashSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "remote-control-prefix" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runRemoteControlPrefixSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "migrate-rc-prefix" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runMigrateRCPrefixSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "usage" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runUsageSubcommand(subArgs)
	}

	if len(os.Args) >= 2 && os.Args[1] == "eval" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runEvalCmd(subArgs, os.Stdout, os.Stderr)
	}

	if len(os.Args) >= 2 && os.Args[1] == "harness" {
		subArgs := []string{}
		if len(os.Args) >= 3 {
			subArgs = os.Args[2:]
		}
		return runHarnessSubcommand(subArgs)
	}

	if verb, ok := unknownSubcommand(os.Args); ok {
		fmt.Fprintf(os.Stderr, "harmonik: unknown subcommand %q\n", verb)
		harmonikUsage()
		return exitUnknownSubcommand
	}

	if !startDaemonRequested {
		fmt.Fprint(os.Stderr, daemonStartRefusal(os.Args))
		harmonikUsage()
		return exitUnknownSubcommand
	}

	defer lifecycle.RecoverWithLogFlush(nil, nil, nil)

	var policyEngine core.PolicyEngine = core.NoOpPolicyEngine{}
	_ = policyEngine // consumed by dispatcher once cluster-A EM beads land

	// TODO(hk-b3f): pass policyEngine to the EM dispatcher once the
	// dispatcher wiring beads (hk-b3f cluster-A) land. The binding site is
	// here; the consumer site is internal/orchestrator (not yet shipped).

	var projectFlag string
	flag.StringVar(&projectFlag, "project", "", "project directory (default: current working directory)")

	var maxConcurrentFlag int
	flag.IntVar(&maxConcurrentFlag, "max-concurrent", 1, "maximum number of beads dispatched concurrently")

	var subscriptionTokenCeilingFlag int64
	flag.Int64Var(&subscriptionTokenCeilingFlag, "subscription-token-ceiling", 0,
		"per-5h token ceiling for the Claude subscription; enables auto-tuning of --max-concurrent (0 = disabled)")

	var workflowModeFlag string
	flag.StringVar(&workflowModeFlag, "workflow-mode", string(core.WorkflowModeDot),
		"daemon-level default workflow mode: single, dot (default: dot)")

	var autoPullFlag bool
	flag.BoolVar(&autoPullFlag, "auto-pull", false, "enable br-ready fallback poll (historical single-daemon topology; opt-in)")
	flag.BoolVar(new(bool), "no-auto-pull", false, "no-op alias; queue-only is now the default (back-compat)")

	var targetBranchFlag string
	flag.StringVar(&targetBranchFlag, "target-branch", "", "branch to merge completed bead branches into (default: main)")

	var protectBranchesFlag stringSliceFlag
	flag.Var(&protectBranchesFlag, "protect-branch", "branch name to protect from daemon merges (repeatable)")

	var forbidUnprotectedDefaultFlag bool
	flag.BoolVar(&forbidUnprotectedDefaultFlag, "forbid-default-main", false,
		"refuse to start if the default branch (main/master) is not in --protect-branch")

	var defaultHarnessFlag string
	flag.StringVar(&defaultHarnessFlag, "default-harness", "",
		"global default harness (tier-4): claude-code, codex (default: claude-code built-in fallback)")

	var codexBinaryFlag string
	flag.StringVar(&codexBinaryFlag, "codex-binary", "",
		"path to the codex executable (default: 'codex' resolved by PATH)")

	var workerHostFlag string
	flag.StringVar(&workerHostFlag, "worker-host", "",
		"override the remote worker host (B4 remote-substrate; empty = use workers.yaml value)")

	var workerEnabledFlag bool
	flag.BoolVar(&workerEnabledFlag, "worker-enabled", false,
		"override the remote worker enabled state (B4 remote-substrate)")

	var agentReadyTimeoutFlag time.Duration
	flag.DurationVar(&agentReadyTimeoutFlag, "agent-ready-timeout", 0,
		"per-dispatch timeout for agent_ready event; 0 uses the built-in default (150s) (hk-hzj)")

	var remoteAgentReadyTimeoutFlag time.Duration
	flag.DurationVar(&remoteAgentReadyTimeoutFlag, "remote-agent-ready-timeout", 0,
		"per-dispatch timeout for agent_ready event on a REMOTE worker; 0 uses the built-in default (210s) (hk-96d7w)")

	var spawnStaggerFlag time.Duration
	flag.DurationVar(&spawnStaggerFlag, "spawn-stagger", 0,
		"minimum interval between consecutive agent window creations; 0 disables (hk-hzj)")

	flag.Usage = harmonikUsage
	flag.Parse()

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

	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		return 1
	}

	explicitFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })

	projCfg, projCfgErr := projectconfig.LoadProjectConfig(projectDir)
	if projCfgErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", projCfgErr)
		return 1
	}

	piBlock := projCfg.Harnesses.Pi
	if piBlock.Provider != "" || piBlock.Model != "" || piBlock.APIKeyEnv != "" {
		if _, piCfgErr := ResolvePiConfig(piBlock, projectDir); piCfgErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik: %v\n", piCfgErr)
			return 1
		}
	}

	branchDflt, branchErr := branching.Load(projectDir)
	if branchErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", branchErr)
		return 1
	}

	workersCfg, workersErr := workers.Load(projectDir)
	if workersErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", workersErr)
		return 1
	}
	workersCfg = applyWorkerOverrides(workersCfg, explicitFlags, workerHostFlag, workerEnabledFlag)

	if !explicitFlags["max-concurrent"] && projCfg.Daemon.MaxConcurrent > 0 {
		maxConcurrentFlag = projCfg.Daemon.MaxConcurrent
	}

	if !explicitFlags["workflow-mode"] && projCfg.Daemon.WorkflowMode != "" {
		workflowModeFlag = string(projCfg.Daemon.WorkflowMode)
	}

	if !explicitFlags["target-branch"] && branchDflt.LandsOn != "" {
		targetBranchFlag = branchDflt.LandsOn
	}

	brPath := optionalExecutablePath("br")

	kerfPath := optionalExecutablePath("kerf")
	if v := os.Getenv("HARMONIK_DISABLE_EAGER_REFILL"); v == "1" || v == "true" {
		kerfPath = ""
	}

	jsonlLogPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), core.HarmonikDirMode); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot create .harmonik/events/: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), core.HarmonikDirMode); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot create .harmonik/beads-intents/: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "harmonik daemon starting in", projectDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go inFlightDrainGoroutine(ctx, runCtx, cancelRun)

	daemonBinaryPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: os.Executable() failed — cannot resolve daemon binary path for hook commands: %v\n", err)
		return 1
	}

	tmuxAdapter := tmux.OSAdapter{}
	hosting := resolveTmuxHosting(ctx, projectDir, tmuxAdapter, os.Stderr)
	if !hosting.Available {
		if code := reportNoTmuxHosting(hosting, tmuxSubstrateSelected(), os.Stderr); code != 0 {
			return code
		}
	}

	maxSessions := spawnCapFromEnv(maxConcurrentFlag)

	substrateOpts := []daemon.TmuxSubstrateOption{
		daemon.WithSpawnCap(maxSessions),
		daemon.WithSpawnStagger(spawnStaggerFlag),                            // hk-hzj: spread concurrent cold-starts; 0 = disabled
		daemon.WithCrewProjectHash(lifecycle.ComputeProjectHash(projectDir)), // fleet-portability T2
	}
	if hosting.NeedKeepalive {
		substrateOpts = append(substrateOpts, daemon.WithSessionKeepalive(0)) // 0 = default 30 s interval
	}

	resolvedHash := resolvedCommitHash()

	var tmuxSub handler.Substrate
	if hosting.Available {
		tmuxSub = daemon.NewTmuxSubstrate(tmuxAdapter, hosting.SessionName, substrateOpts...)
	}
	codexSubstrate, codexRegObserver, reviewerSubstrate := selectSubstrate(tmuxSub, codexBinaryFlag)

	cfg := daemon.Config{
		ProjectDir:               projectDir,
		BrPath:                   brPath,
		KerfPath:                 kerfPath, // hk-9321v: kerf next for EM-062/EM-063 eager-refill
		JSONLLogPath:             jsonlLogPath,
		MaxConcurrent:            maxConcurrentFlag,
		NoAutoPull:               !autoPullFlag,  // hk-8vy18: queue-only by default; --auto-pull opts in to br-ready drain
		Substrate:                codexSubstrate, // AIS-015 selection axis; default tmux
		ReviewerSubstrate:        reviewerSubstrate,
		WorkerRegistryObserver:   codexRegObserver,                    // M4-C3: late-bind live registry into Codex runner
		DaemonBinaryPath:         daemonBinaryPath,                    // absolute path for hook commands (hk-kqdpf.6)
		BinaryCommitHash:         resolvedHash,                        // ldflags stamp or runtime/debug fallback (hk-mz0x4, hk-v3nv)
		AgentReadyTimeout:        agentReadyTimeoutFlag,               // hk-hzj: per-dispatch ready timeout; 0 = built-in default (150s)
		RemoteAgentReadyTimeout:  remoteAgentReadyTimeoutFlag,         // hk-96d7w: remote-worker ready timeout; 0 = built-in default (210s)
		SubscriptionTokenCeiling: subscriptionTokenCeilingFlag,        // hk-ymav1: bandwidth auto-tuner
		WorkflowModeDefault:      core.WorkflowMode(workflowModeFlag), // hk-30vlb: default to dot (embedded standard-bead.dot)
		TargetBranch:             targetBranchFlag,                    // hk-mkxw1: merge target branch
		ProtectBranches:          []string(protectBranchesFlag),       // hk-mkxw1: branches protected from daemon merges
		ForbidUnprotectedDefault: forbidUnprotectedDefaultFlag,        // hk-mkxw1: guard against unprotected default branch
		DefaultHarness:           core.AgentType(defaultHarnessFlag),  // hk-y01k6: tier-4 harness default
		CodexBinary:              codexBinaryFlag,                     // hk-y01k6: codex executable path
		Workers:                  workersCfg,                          // hk-rs-b4-bootwire-b44z: remote-substrate worker registry
	}

	if resolvedHash != "unknown" && resolvedHash != "" {
		for _, e := range release.Ledger {
			if e.CommitHash == resolvedHash && e.Yanked {
				fmt.Fprintf(os.Stderr, "FATAL: this binary (%s, %s) has been yanked: %s\n",
					e.Semver, resolvedHash, e.YankedReason)
				return 9
			}
		}
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

	startSupervisorWatchdogIfEnabled(
		ctx,
		projCfg.Subsystems,
		buildSupervisorWatchdogSpec(projectDir, daemonBinaryPath),
		os.Stderr,
	)

	cfg.StopDispatchCtx = ctx

	if err := daemon.Start(runCtx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: %v\n", err)
		if errors.Is(err, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	return 0
}

type stringSliceFlag []string

func (f *stringSliceFlag) String() string { return strings.Join(*f, ",") }
func (f *stringSliceFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

const inFlightDrainGrace = 5 * time.Minute

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

func startSupervisorWatchdogIfEnabled(
	ctx context.Context,
	subsystems projectconfig.SubsystemsConfig,
	spec supervise.SupervisorWatchdogSpec,
	logOut io.Writer,
) *supervise.SupervisorWatchdog {
	if !subsystems.Enabled(projectconfig.SubsystemSupervisorWatchdog) {
		// Say so at boot. A silent partition looks the same as a config that
		// did not take effect.
		//nolint:errcheck // best-effort boot diagnostic; a failed log write must not stop the daemon
		_, _ = fmt.Fprintf(logOut, "supervisor-watchdog: subsystem %s disabled — not started; "+
			"a dead supervisor will not be detected or revived\n",
			projectconfig.SubsystemSupervisorWatchdog)
		return nil
	}
	swLog := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sw := supervise.NewSupervisorWatchdog(spec, swLog)
	go func() {
		if err := sw.Run(ctx); err != nil && ctx.Err() == nil {
			//nolint:errcheck // best-effort exit diagnostic; the daemon is already past boot
			_, _ = fmt.Fprintf(logOut, "supervisor-watchdog: exited: %v\n", err)
		}
	}()
	return sw
}

func buildSupervisorWatchdogSpec(projectDir, binaryPath string) supervise.SupervisorWatchdogSpec {
	return supervise.SupervisorWatchdogSpec{
		PidfilePath: filepath.Join(projectDir, ".harmonik", "cognition", "supervisor.pid"),
		ReviveCmd:   []string{binaryPath, "supervise", "restart", "--watch-restart", "--project", projectDir},
		WorkDir:     projectDir,
		MaxRevives:  3,
	}
}

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
