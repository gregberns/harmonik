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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/hookrelay"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
)

func main() {
	os.Exit(run())
}

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

	// hk tmux-start — operator-facing tmux session bootstrap.
	// Parses its own --session-name flag and must run before flag.Parse so
	// that the global flag set does not reject the subcommand-specific flag.
	//
	// Spec: process-lifecycle.md §4.10 PL-028 refinement.
	if len(os.Args) >= 2 && os.Args[1] == "tmux-start" {
		subArgs := os.Args[2:]
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

	if len(os.Args) >= 2 && os.Args[1] == "queue" {
		verb := ""
		if len(os.Args) >= 3 {
			verb = os.Args[2]
		}
		subArgs := []string{}
		if len(os.Args) >= 4 {
			subArgs = os.Args[3:]
		}
		ctx := context.Background()
		switch verb {
		case "submit":
			return queuecli.RunQueueSubmit(ctx, subArgs, os.Stdout, os.Stderr)
		case "append":
			return queuecli.RunQueueAppend(ctx, subArgs, os.Stdout, os.Stderr)
		case "status":
			return queuecli.RunQueueStatus(ctx, subArgs, os.Stdout, os.Stderr)
		case "dry-run":
			return queuecli.RunQueueDryRun(ctx, subArgs, os.Stdout, os.Stderr)
		default:
			fmt.Fprintf(os.Stderr, "harmonik queue: unrecognised verb %q; v0.1 verbs are: submit, append, status, dry-run\n", verb)
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

	// Resolve the current session name by asking tmux directly.
	// We use exec.Command here (not OSAdapter.display-message) because this path
	// runs before the substrate is constructed and there is no window handle to
	// target; the unqualified display-message returns the current session.
	var sessionNameBytes []byte
	sessionNameBytes, err = exec.Command("tmux", "display-message", "-p", "#{session_name}").Output() //nolint:gosec // G204: arguments are hard-coded constants
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: cannot resolve tmux session name: %v\n", err)
		return 1
	}
	sessionName := strings.TrimSpace(string(sessionNameBytes))
	if sessionName == "" {
		fmt.Fprintln(os.Stderr, "harmonik: tmux returned an empty session name — cannot attach substrate")
		return 1
	}

	// Probe tmux version (≥ 3.0 required for -e env-injection per PL-021b).
	tmuxAdapter := tmux.OSAdapter{}
	if probeErr := tmuxAdapter.ProbeTmux(ctx); probeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik: tmux probe failed: %v\n", probeErr)
		return 1
	}

	cfg := daemon.Config{
		ProjectDir:       projectDir,
		BrPath:           brPath,
		JSONLLogPath:     jsonlLogPath,
		MaxConcurrent:    maxConcurrentFlag,
		Substrate:        daemon.NewTmuxSubstrate(tmuxAdapter, sessionName),
		DaemonBinaryPath: daemonBinaryPath, // absolute path for hook commands (hk-kqdpf.6)
		BinaryCommitHash: commitHash,       // stamped via -ldflags at build time (hk-mz0x4)
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
